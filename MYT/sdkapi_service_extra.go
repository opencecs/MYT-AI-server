package main

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/moyunteng/myt/internal/maintenance"
	sdkapi "github.com/moyunteng/myt/internal/sdkapi"
	"github.com/moyunteng/myt/internal/util"
)

// --- 安卓管理补充 ---

// CopyProgress 复制进度事件数据
type CopyProgress struct {
	DeviceIP string `json:"deviceIP"`
	Current  int    `json:"current"`
	Total    int    `json:"total"`
	Status   string `json:"status"`
	Message  string `json:"message"`
}

// ImportProgress 导入进度事件数据（设备端 SSE 事件透传）
type ImportProgress struct {
	DeviceIP    string `json:"deviceIP"`
	MachineName string `json:"machineName"`
	Uploaded    int64  `json:"uploaded"`
	Total       int64  `json:"total"`
	Progress    int    `json:"progress"`
	Stage       string `json:"stage,omitempty"` // upload/verify/extract/restore/start/complete
	Msg         string `json:"msg,omitempty"`   // 设备端阶段描述
}

func (s *SdkApiServiceWrapper) CopyAndroid(deviceIP, name string, indexNum, count int, taskID string) error {
	ctx := s.registerCancel(taskID)
	defer s.unregisterCancel(taskID)
	client := s.newClientWithTimeout(deviceIP, 10*time.Minute)
	client.SetContext(ctx)
	err := client.CopyAndroidStream(name, indexNum, count, func(current, total int, status, message string) {
		s.emitEvent("android:copy-progress", CopyProgress{
			DeviceIP: deviceIP,
			Current:  current,
			Total:    total,
			Status:   status,
			Message:  message,
		})
	})
	if err == nil {
		s.notifyAndroidsChanged([]string{deviceIP})
	}
	return err
}

func (s *SdkApiServiceWrapper) ResetAndroid(deviceIP, name string, start bool) error {
	// V4 模式：8088 /android/reset（wipe_data=false，与 V3「重置=重建环境不清数据」对齐）
	if s.isV4Mode() {
		return s.resetAndroidV4(deviceIP, name, false, start)
	}
	// 重置=恢复出厂（设备端重建容器数据），同步等待可能超过默认超时——与容器操作同档
	err := s.newClientWithTimeout(deviceIP, androidOpTimeout).ResetAndroid(name, start)
	if err == nil {
		s.notifyAndroidsChanged([]string{deviceIP})
	}
	return err
}

// CrossDeviceMove 跨设备移动云机（Go侧流式管道，不经过前端内存）
func (s *SdkApiServiceWrapper) CrossDeviceMove(srcIP, dstIP, androidName, imageURL string, targetSlot int, taskID string) error {
	ctx := s.registerCancel(taskID)
	defer s.unregisterCancel(taskID)
	onStage := func(stage, message string) {
		s.emitEvent("android:move-progress", map[string]any{
			"srcIP":   srcIP,
			"dstIP":   dstIP,
			"stage":   stage,
			"message": message,
		})
	}
	srcClient := s.newClientWithTimeout(srcIP, 30*time.Minute)
	srcClient.SetContext(ctx)
	dstClient := s.newClientWithTimeout(dstIP, 30*time.Minute)
	dstClient.SetContext(ctx)
	err := sdkapi.CrossDeviceMove(ctx, srcClient, dstClient, androidName, imageURL, targetSlot, onStage)
	if err == nil {
		s.notifyAndroidsChanged([]string{srcIP, dstIP})
	}
	return err
}

func (s *SdkApiServiceWrapper) ExportAndroid(deviceIP, name, taskID, exportName string) (string, error) {
	log.Printf("[ExportAndroid] 开始: deviceIP=%s name=%s exportName=%q", deviceIP, name, exportName)
	ctx := s.registerCancel(taskID)
	defer s.unregisterCancel(taskID)
	client := s.newClient(deviceIP)
	client.SetContext(ctx)
	// 设备端 SSE 进度事件（config/snapshot/.../pack）逐条透传给前端任务队列
	result, err := client.ExportAndroidWithProgress(name, exportName, func(ev sdkapi.SSEResult) {
		s.emitEvent("android:export-progress", map[string]any{
			"deviceIP": deviceIP,
			"stage":    ev.Stage,
			"progress": ev.Progress,
			"msg":      ev.Msg,
		})
	})
	log.Printf("[ExportAndroid] 结果: deviceIP=%s name=%s exportName=%q err=%v", deviceIP, name, result, err)
	if err != nil {
		s.emitEvent("android:export-progress", map[string]any{
			"deviceIP": deviceIP,
			"stage":    "error",
			"error":    err.Error(),
		})
	}
	return result, err
}

func (s *SdkApiServiceWrapper) ImportAndroid(deviceIP, name string, indexNum int, newName string, start bool, keepMac bool, taskID string) (string, error) {
	ctx := s.registerCancel(taskID)
	defer s.unregisterCancel(taskID)
	client := s.newClient(deviceIP)
	client.SetContext(ctx)
	// 设备端 SSE 进度事件（verify/extract/restore/start）透传给前端任务队列
	result, err := client.ImportAndroid(name, indexNum, newName, start, keepMac, func(ev sdkapi.SSEResult) {
		s.emitEvent("android:import-progress", ImportProgress{
			DeviceIP:    deviceIP,
			MachineName: name,
			Uploaded:    ev.Current,
			Total:       ev.Total,
			Progress:    ev.Progress,
			Stage:       ev.Stage,
			Msg:         ev.Msg,
		})
	})
	if err == nil {
		s.notifyAndroidsChanged([]string{deviceIP})
	}
	return result, err
}

// registerCancel 为长任务注册可取消的 context，返回 ctx。CancelTaskOp 调 cancelFn 可中断执行。
func (s *SdkApiServiceWrapper) registerCancel(taskID string) context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	s.cancelOpsMu.Lock()
	s.cancelOps[taskID] = cancel
	s.cancelOpsMu.Unlock()
	return ctx
}

// unregisterCancel 任务结束后清理 cancelFn
func (s *SdkApiServiceWrapper) unregisterCancel(taskID string) {
	s.cancelOpsMu.Lock()
	delete(s.cancelOps, taskID)
	s.cancelOpsMu.Unlock()
}

// CancelTaskOp 取消指定任务的执行（前端长任务取消按钮调用）
func (s *SdkApiServiceWrapper) CancelTaskOp(taskID string) {
	s.cancelOpsMu.Lock()
	cancel, ok := s.cancelOps[taskID]
	s.cancelOpsMu.Unlock()
	if ok {
		cancel()
		log.Printf("[sdkapi] 取消任务执行: %s", taskID)
	}
}

// ImportAndroidFromFile 从本地备份文件导入安卓容器到设备
func (s *SdkApiServiceWrapper) ImportAndroidFromFile(deviceIP, machineName string, indexNum int, newName string, keepMac bool, taskID string) (string, error) {
	if s.cm == nil {
		return "", os.ErrNotExist
	}
	backupDir := s.cm.CloudMachineBackupDir()
	entries, err := os.ReadDir(backupDir)
	if err != nil {
		return "", err
	}
	// 优先匹配 .tar.gz 文件，避免匹配到旧的无后缀文件
	var filePath string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.Contains(name, machineName) && strings.HasSuffix(name, ".tar.gz") {
			filePath = filepath.Join(backupDir, name)
			break
		}
	}
	// fallback: 匹配任意包含 machineName 的文件
	if filePath == "" {
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			if strings.Contains(entry.Name(), machineName) {
				filePath = filepath.Join(backupDir, entry.Name())
				break
			}
		}
	}
	if filePath == "" {
		return "", os.ErrNotExist
	}
	ctx := s.registerCancel(taskID)
	defer s.unregisterCancel(taskID)
	client := s.newClient(deviceIP)
	client.SetContext(ctx)
	// 设备端 SSE 进度事件（upload/verify/extract/restore/start）逐条透传给前端任务队列。
	// start 固定 true（本地导入弹窗无开机选项，与原行为一致）；keepPorts 默认 false（新分配端口）。
	result, err := client.ImportAndroidFromFile(filePath, machineName, newName, indexNum, true, keepMac, false, func(ev sdkapi.SSEResult) {
		s.emitEvent("android:import-progress", ImportProgress{
			DeviceIP:    deviceIP,
			MachineName: machineName,
			Uploaded:    ev.Current,
			Total:       ev.Total,
			Progress:    ev.Progress,
			Stage:       ev.Stage,
			Msg:         ev.Msg,
		})
	})
	if err == nil {
		s.notifyAndroidsChanged([]string{deviceIP})
	}
	return result, err
}

// LocalBackupMachine 本地备份云机信息
type LocalBackupMachine struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
}

// ListLocalBackupMachines 列出本地备份云机文件
func (s *SdkApiServiceWrapper) ListLocalBackupMachines() []LocalBackupMachine {
	if s.cm == nil {
		return nil
	}
	backupDir := s.cm.CloudMachineBackupDir()
	entries, err := os.ReadDir(backupDir)
	if err != nil {
		return nil
	}
	var result []LocalBackupMachine
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		fi, err := entry.Info()
		if err != nil {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
		result = append(result, LocalBackupMachine{
			Name: name,
			Size: fi.Size(),
		})
	}
	return result
}

func (s *SdkApiServiceWrapper) SwitchModel(deviceIP, name, modelID, localModel, modelStatic string, latitude, longitude float64, countryCode string) error {
	err := s.newClient(deviceIP).SwitchModel(name, modelID, localModel, modelStatic, latitude, longitude, countryCode)
	if err == nil {
		s.notifyAndroidsChanged([]string{deviceIP})
	}
	return err
}

func (s *SdkApiServiceWrapper) ChangeImage(deviceIP string, containerNames []string, image string) (string, string, error) {
	result, message, err := s.newClient(deviceIP).ChangeImage(containerNames, image)
	if err == nil {
		s.notifyAndroidsChanged([]string{deviceIP})
	}
	return result, message, err
}

func (s *SdkApiServiceWrapper) PruneImages(deviceIP string) (int, string, error) {
	return s.newClient(deviceIP).PruneImages()
}

// CleanLogs 清理设备/云机日志（委托 maintenance 包，复用带密码的 SDK 客户端）。
// 返回 (success, message, error)，供任务队列与维护 modal 复用。
func (s *SdkApiServiceWrapper) CleanLogs(deviceIP, androidName string, categories []string) (bool, string, error) {
	r := maintenance.CleanLogs(deviceIP, androidName, categories, s.newClient(deviceIP))
	success, _ := r["success"].(bool)
	message, _ := r["message"].(string)
	return success, message, nil
}

// CleanLogsWithProgress 带进度回调的日志清理（任务队列用，按 category 推进度）。
func (s *SdkApiServiceWrapper) CleanLogsWithProgress(deviceIP, androidName string, categories []string, cb func(done, total int, stage string)) (bool, string, error) {
	r := maintenance.CleanLogsWithProgress(deviceIP, androidName, categories, s.newClient(deviceIP), cb)
	success, _ := r["success"].(bool)
	message, _ := r["message"].(string)
	return success, message, nil
}

// CleanAndroidCache 清理云机缓存（委托 maintenance 包，复用带密码的 SDK 客户端）。
func (s *SdkApiServiceWrapper) CleanAndroidCache(deviceIP, androidName string, categories []string) (bool, string, error) {
	r := maintenance.CleanAndroidCache(deviceIP, androidName, categories, s.newClient(deviceIP))
	success, _ := r["success"].(bool)
	message, _ := r["message"].(string)
	return success, message, nil
}

// CleanAndroidCacheWithProgress 带进度回调的缓存清理（任务队列用，按 category 推进度）。
func (s *SdkApiServiceWrapper) CleanAndroidCacheWithProgress(deviceIP, androidName string, categories []string, cb func(done, total int, stage string)) (bool, string, error) {
	r := maintenance.CleanAndroidCacheWithProgress(deviceIP, androidName, categories, s.newClient(deviceIP), cb)
	success, _ := r["success"].(bool)
	message, _ := r["message"].(string)
	return success, message, nil
}

// ResetDevice 格式化设备磁盘（设备级清盘，委托 maintenance 包调 /server/device/reset）。
// 返回 (success, message, error)，供任务队列 formatDisk handler 使用。
func (s *SdkApiServiceWrapper) ResetDevice(deviceIP string) (bool, string, error) {
	r := maintenance.ResetDevice(deviceIP)
	success, _ := r["success"].(bool)
	message, _ := r["message"].(string)
	return success, message, nil
}

func (s *SdkApiServiceWrapper) GetTaskStatus(deviceIP, taskID string) (string, error) {
	return s.newClient(deviceIP).GetTaskStatus(taskID)
}

// --- 本地机型管理 ---

func (s *SdkApiServiceWrapper) ListLocalPhoneModels(deviceIP string) ([]sdkapi.PhoneModelEntry, error) {
	return s.newClient(deviceIP).ListLocalPhoneModels()
}

func (s *SdkApiServiceWrapper) DeleteLocalPhoneModel(deviceIP, name string) error {
	return s.newClient(deviceIP).DeleteLocalPhoneModel(name)
}

func (s *SdkApiServiceWrapper) ExportLocalPhoneModel(deviceIP, name string) error {
	return s.newClient(deviceIP).ExportLocalPhoneModel(name)
}

func (s *SdkApiServiceWrapper) ImportLocalPhoneModel(deviceIP, fileURL string) (string, error) {
	return s.newClient(deviceIP).ImportLocalPhoneModel(fileURL)
}

// --- 机型备份管理 ---

func (s *SdkApiServiceWrapper) ListPhoneModelBackups(deviceIP string) ([]sdkapi.PhoneModelBackup, error) {
	return s.newClient(deviceIP).ListPhoneModelBackups()
}

func (s *SdkApiServiceWrapper) SavePhoneModelBackup(deviceIP, name, suffix string) error {
	return s.newClient(deviceIP).SavePhoneModelBackup(name, suffix)
}

func (s *SdkApiServiceWrapper) DeletePhoneModelBackup(deviceIP, name string) error {
	return s.newClient(deviceIP).DeletePhoneModelBackup(name)
}

// ExportPhoneModelBackup 导出机型备份文件（返回文件字节）
func (s *SdkApiServiceWrapper) ExportPhoneModelBackup(deviceIP, name string) ([]byte, error) {
	return s.newClient(deviceIP).ExportPhoneModelBackup(name)
}

// ImportPhoneModelBackup 导入机型备份文件（fileURL 为设备侧可访问的远程地址）
func (s *SdkApiServiceWrapper) ImportPhoneModelBackup(deviceIP, fileURL string) error {
	return s.newClient(deviceIP).ImportPhoneModelBackup(fileURL)
}

// --- SDK 升级 ---

func (s *SdkApiServiceWrapper) CheckUpgrade(deviceIP string) (string, error) {
	client := s.newClient(deviceIP)
	onEvent := func(body []byte) {
		var p sdkapi.SSEProgress
		if err := json.Unmarshal(body, &p); err == nil {
			s.emitEvent("upgrade:progress", map[string]any{
				"deviceIP": deviceIP,
				"progress": p.Progress,
				"current":  p.Current,
				"total":    p.Total,
			})
		}
	}
	return client.CheckUpgradeWithProgress(onEvent)
}

// --- VPC 方法已全部迁移至 VpcServiceWrapper (vpc_service.go)，避免 Wails methodID 冲突 ---
// AddVpcRuleBatch, DeleteVpcRuleBatch, SetVpcSocks, EnableWhiteListDNS, DeleteVpc 等均在 VpcServiceWrapper 上

// OpenBackupDirectory 打开本地备份目录
func (s *SdkApiServiceWrapper) OpenBackupDirectory() error {
	if s.cm == nil {
		return os.ErrNotExist
	}
	return util.OpenDirectory(s.cm.CloudMachineBackupDir())
}

// --- 备份下载 ---

func (s *SdkApiServiceWrapper) DownloadBackup(deviceIP, filename string, deleteAfterDownload bool, taskID string) (string, error) {
	localDir := "backups"
	if s.cm != nil {
		localDir = s.cm.CloudMachineBackupDir()
	}
	log.Printf("[DownloadBackup] 开始: deviceIP=%s filename=%s localDir=%s", deviceIP, filename, localDir)
	s.emitEvent("android:export-progress", map[string]any{
		"deviceIP": deviceIP,
		"stage":    "downloading",
		"progress": 0,
	})
	client := s.newClient(deviceIP)
	// 对接任务取消机制：taskID 非空时注册可取消 ctx，CancelTaskOp 可中断下载
	if taskID != "" {
		ctx := s.registerCancel(taskID)
		defer s.unregisterCancel(taskID)
		client.SetContext(ctx)
	}
	onProgress := func(downloaded, total int64) {
		pct := 0
		if total > 0 {
			pct = int(downloaded * 100 / total)
		}
		s.emitEvent("android:export-progress", map[string]any{
			"deviceIP":   deviceIP,
			"stage":      "downloading",
			"progress":   pct,
			"downloaded": downloaded,
			"total":      total,
		})
	}
	localPath, err := client.DownloadBackupWithProgress(filename, localDir, deleteAfterDownload, onProgress)
	log.Printf("[DownloadBackup] 结果: localPath=%q err=%v", localPath, err)
	if err != nil {
		s.emitEvent("android:export-progress", map[string]any{
			"deviceIP": deviceIP,
			"stage":    "error",
			"error":    err.Error(),
		})
		return "", err
	}
	return localPath, nil
}

// DownloadExportedImage 下载导出后的安卓镜像包到本地
func (s *SdkApiServiceWrapper) DownloadExportedImage(deviceIP, filename string) error {
	localDir := "backups"
	if s.cm != nil {
		localDir = s.cm.CloudMachineBackupDir()
	}
	client := s.newClient(deviceIP)
	_, err := client.DownloadExportedImage(filename, localDir)
	return err
}
