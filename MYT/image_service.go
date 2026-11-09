package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"sync"

	"github.com/moyunteng/myt/internal/imagemgr"
	"github.com/moyunteng/myt/internal/util"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// ImageServiceWrapper 封装本地镜像管理服务，供Wails前端调用
type ImageServiceWrapper struct {
	mgr               *imagemgr.Service
	dlMgr             *imagemgr.DownloadManager
	ulMgr             *imagemgr.UploadManager
	app               *application.App
	urlMap            sync.Map // mirrorID → imageURL，用于取消时定位 .downloading 文件
	getDevicePassword func(deviceIP string) string
}

// NewImageServiceWrapper 创建镜像服务封装
func NewImageServiceWrapper(mgr *imagemgr.Service) *ImageServiceWrapper {
	return &ImageServiceWrapper{
		mgr:   mgr,
		dlMgr: imagemgr.NewDownloadManager(),
		ulMgr: imagemgr.NewUploadManager(),
	}
}

// SetApp 设置应用实例（用于事件推送）
func (s *ImageServiceWrapper) SetApp(app *application.App) {
	s.app = app
}

// emitEvent 向前端发送事件
func (s *ImageServiceWrapper) emitEvent(name string, data any) {
	if s.app != nil && s.app.Event != nil {
		s.app.Event.Emit(name, data)
	}
}

// DownloadMirror 下载镜像到本地（异步，通过事件推送进度）
func (s *ImageServiceWrapper) DownloadMirror(id, name, url, ttype2, osVer, sysVer, sysVerDes, udesc string) error {
	log.Printf("DownloadMirror 被调用: id=%s name=%s", id, name)

	meta := imagemgr.NewSavedImage(id, name, url, ttype2, osVer, sysVer, sysVerDes, udesc)
	imagesDir := s.mgr.ImagesDir()
	s.urlMap.Store(id, url)

	ctx, cancel := context.WithCancel(context.Background())
	s.dlMgr.SetCancel(id, cancel)

	util.GoSafe(func() {
		onProgress := func(stage string, progress float64) {
			s.emitEvent("download-progress", map[string]any{
				"mirrorId": id,
				"stage":    stage,
				"progress": progress,
			})
		}

		err := imagemgr.DownloadImage(url, imagesDir, meta, onProgress, ctx.Done())

		// 清理 cancel 函数（避免内存泄漏）
		s.dlMgr.Cancel(id)

		if err != nil {
			// 取消导致失败时，由 CancelDownload 统一发送事件，此处跳过
			if ctx.Err() != nil {
				return
			}
			log.Printf("下载镜像失败 %s: %v", name, err)
			os.Remove(s.mgr.DownloadingPath(url))
			os.Remove(s.mgr.MetaPath(url))
			s.urlMap.Delete(id)
			s.emitEvent("download-complete", map[string]any{
				"mirrorId": id,
				"success":  false,
				"error":    err.Error(),
			})
			return
		}

		// 下载完成，重命名 .downloading → .tar
		if err := s.mgr.FinalizeDownload(url); err != nil {
			log.Printf("重命名镜像文件失败 %s: %v", name, err)
			os.Remove(s.mgr.DownloadingPath(url))
			os.Remove(s.mgr.MetaPath(url))
			s.urlMap.Delete(id)
			s.emitEvent("download-complete", map[string]any{
				"mirrorId": id,
				"success":  false,
				"error":    err.Error(),
			})
			return
		}

		s.urlMap.Delete(id)
		s.emitEvent("download-complete", map[string]any{
			"mirrorId": id,
			"success":  true,
		})
	})

	return nil
}

// CancelDownload 取消下载并清理残留文件
func (s *ImageServiceWrapper) CancelDownload(mirrorID string) {
	log.Printf("CancelDownload 被调用: %s", mirrorID)

	// 取消下载 goroutine
	s.dlMgr.Cancel(mirrorID)

	// 通过 urlMap 找到 imageURL 来定位 .downloading 和 .meta.json 文件
	if urlVal, ok := s.urlMap.LoadAndDelete(mirrorID); ok {
		imageURL, _ := urlVal.(string)
		os.Remove(s.mgr.DownloadingPath(imageURL))
		os.Remove(s.mgr.MetaPath(imageURL))
	}

	// 通知前端下载已取消（统一由此处发送，goroutine 检测到取消后不再发送）
	s.emitEvent("download-complete", map[string]any{
		"mirrorId": mirrorID,
		"success":  false,
		"error":    "下载已取消",
	})

	log.Printf("CancelDownload 完成: %s", mirrorID)
}

// CancelUpload 取消单个设备的上传
func (s *ImageServiceWrapper) CancelUpload(mirrorID, deviceIP string) {
	s.ulMgr.CancelDevice(mirrorID, deviceIP)
}

// CancelAllUploads 取消某镜像的所有上传
func (s *ImageServiceWrapper) CancelAllUploads(mirrorID string) {
	s.ulMgr.CancelAll(mirrorID)
}

// GetSavedMirrors 获取所有已保存的镜像记录
func (s *ImageServiceWrapper) GetSavedMirrors() []imagemgr.SavedImage {
	return s.mgr.GetSavedImages()
}

// DeleteSavedMirror 删除已保存的镜像文件
func (s *ImageServiceWrapper) DeleteSavedMirror(id string) error {
	log.Printf("DeleteSavedMirror: id=%s", id)
	err := s.mgr.DeleteSavedImage(id)
	if err != nil {
		log.Printf("DeleteSavedMirror 失败: id=%s, err=%v", id, err)
	}
	return err
}

func (s *ImageServiceWrapper) SetGetDevicePassword(fn func(deviceIP string) string) {
	s.getDevicePassword = fn
}

// PushToDevices 将本地镜像推送到多台设备（异步，通过事件推送进度和结果）
func (s *ImageServiceWrapper) PushToDevices(mirrorID string, deviceIPs []string) error {
	localPath := s.mgr.FindTarPath(mirrorID)
	if localPath == "" {
		return fmt.Errorf("本地镜像文件不存在")
	}

	onProgress := func(deviceIP string, progress float64) {
		s.emitEvent("upload-progress", map[string]any{
			"mirrorId": mirrorID,
			"deviceIP": deviceIP,
			"progress": progress,
		})
	}

	onComplete := func(deviceIP string, success bool, errMsg string) {
		s.emitEvent("upload-complete", map[string]any{
			"mirrorId": mirrorID,
			"deviceIP": deviceIP,
			"success":  success,
			"error":    errMsg,
		})
	}

	getPwd := s.getDevicePassword
	if getPwd == nil {
		getPwd = func(string) string { return "" }
	}

	util.GoSafe(func() {
		imagemgr.UploadToDevices(mirrorID, localPath, deviceIPs, onProgress, onComplete, s.ulMgr, getPwd)
	})
	return nil
}

// GetImageStoragePath 获取镜像存储路径
func (s *ImageServiceWrapper) GetImageStoragePath() string {
	return s.mgr.ImagesDir()
}

// SetImageStoragePath 设置镜像存储路径（暂返回当前路径，实际迁移后续实现）
func (s *ImageServiceWrapper) SetImageStoragePath(newPath string) (string, error) {
	return s.mgr.ImagesDir(), nil
}

// OpenImageDirectory 打开镜像存储目录
func (s *ImageServiceWrapper) OpenImageDirectory() error {
	dir := s.mgr.ImagesDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("创建镜像目录失败: %w", err)
	}
	return util.OpenDirectory(dir)
}
