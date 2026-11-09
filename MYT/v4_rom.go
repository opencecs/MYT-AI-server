package main

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/moyunteng/myt/internal/v4host"
)

// --- V4 ROM（镜像）管理（Wails 暴露，前端 pages/rom/RomPage）---
//
// 数据源是 tobox_player 8088 的「镜像管理」分组（见 internal/v4host/image.go）：
//   - GET    /image/list              变体列表 + 三件套就绪状态
//   - POST   /image/pull              URL 后台下载（按单文件），返回 task_id
//   - GET    /image/status/{task_id}  下载进度
//   - POST   /image/upload/{variant}  multipart 上传（zip 包或单个 img）
//   - DELETE /image/{variant}         删除整个变体目录
//
// 一个变体 = system.img + vendor.img + initramfs.img 三件套；ready=true 才能用于
// 创建该变体的实例。pull 按单文件触发，所以「下载一个变体」= 三次 pull。

// V4RomFile 变体内单个镜像文件（前端展示大小/状态）
type V4RomFile struct {
	Name  string `json:"name"`
	Size  int64  `json:"size"`
	Ready bool   `json:"ready"`
}

// V4RomVariant 一个镜像变体（前端列表行）
type V4RomVariant struct {
	Variant string      `json:"variant"`
	Files   []V4RomFile `json:"files"`
	// Ready 三件套齐全且均大于 0 字节（可用于创建实例）
	Ready bool `json:"ready"`
	// TotalSize 三件套字节总和（前端显示"文件大小"）
	TotalSize int64 `json:"totalSize"`
	// Missing 缺失/未就绪的文件名（前端显示"缺失"提示）
	Missing []string `json:"missing"`
}

// V4RomList 镜像列表结果
type V4RomList struct {
	// Host 实际查询的主机 IP（多主机时前端据此确认目标）
	Host string `json:"host"`
	// Root 镜像根目录（设备端路径，仅展示）
	Root     string         `json:"root"`
	Variants []V4RomVariant `json:"variants"`
	Total    int            `json:"total"`
}

// V4RomPullResult 触发下载的结果
type V4RomPullResult struct {
	TaskID   string `json:"taskId"`
	Variant  string `json:"variant"`
	Filename string `json:"filename"`
}

// V4RomTaskStatus 下载任务进度
type V4RomTaskStatus struct {
	TaskID     string `json:"taskId"`
	Variant    string `json:"variant"`
	Filename   string `json:"filename"`
	Status     string `json:"status"` // pending / running / completed / failed
	Downloaded int64  `json:"downloaded"`
	Total      int64  `json:"total"`
	Error      string `json:"error"`
}

// V4RomUploadResult 上传结果
type V4RomUploadResult struct {
	Variant  string `json:"variant"`
	Filename string `json:"filename"`
	Size     int64  `json:"size"`
	Ready    bool   `json:"ready"`
}

// v4RomUploadCancel 一个进行中的 ROM 上传的取消句柄
type v4RomUploadCancel struct {
	cancel  context.CancelFunc
	started time.Time
}

// v4RomUploadMu 保护 v4RomUploadCancels（key = host|variant，同一变体同时只允许
// 一个上传：前端弹窗内已互斥，这里兜底防绕过 UI 的并发调用互相覆盖取消句柄）。
var (
	v4RomUploadMu      sync.Mutex
	v4RomUploadCancels = make(map[string]*v4RomUploadCancel)
)

// romUploadKey 取消表的 key（host 解析后的 IP 才是唯一标识）
func romUploadKey(ip, variant string) string { return ip + "|" + variant }

// emitRomUploadProgress 推送上传进度事件（参考 agent:install-progress 的模式）。
func (s *V4ServiceWrapper) emitRomUploadProgress(ev V4RomUploadProgress) {
	if s.app == nil || s.app.Event == nil {
		return
	}
	s.app.Event.Emit("rom:upload-progress", ev)
}

// UploadRomImage 上传镜像到指定变体（POST /image/upload/{variant}，multipart 字段 file）。
//
// filePath 是本地绝对路径（前端经系统文件对话框选取）：zip 包一次上传三件套，
// 单个 img 需显式 filename（system.img / vendor.img / initramfs.img）。
// 大文件走流式上传，超时 1 小时兜底。
//
// 进度经 rom:upload-progress 事件推送（payload 见 V4RomUploadProgress，约每 5MB
// 一次 + 收尾 100% 一次）；前端可调 CancelRomUpload 取消（取消后 UploadRomImage
// 返回「上传已取消」错误）。
func (s *V4ServiceWrapper) UploadRomImage(host, variant, filePath, filename string) (*V4RomUploadResult, error) {
	if s.configManager.GetDeployMode() != "v4" {
		return nil, requireV4Mode("ROM 上传")
	}
	ip, err := s.resolveRomHost(host)
	if err != nil {
		return nil, err
	}
	key := romUploadKey(ip, variant)

	v4RomUploadMu.Lock()
	if _, busy := v4RomUploadCancels[key]; busy {
		v4RomUploadMu.Unlock()
		return nil, fmt.Errorf("该变体已有上传在进行，请先等待完成或取消")
	}
	ctx, cancel := context.WithCancel(context.Background())
	v4RomUploadCancels[key] = &v4RomUploadCancel{cancel: cancel, started: time.Now()}
	v4RomUploadMu.Unlock()
	defer func() {
		v4RomUploadMu.Lock()
		delete(v4RomUploadCancels, key)
		v4RomUploadMu.Unlock()
		cancel()
	}()

	start := time.Now()
	lastLogged := start
	res, err := newV4InstanceClient(ip).UploadImage(ctx, variant, filePath, filename, func(sent, total int64) {
		percent := 100.0
		if total > 0 && sent < total {
			percent = float64(sent) / float64(total) * 100
		}
		s.emitRomUploadProgress(V4RomUploadProgress{
			Host: ip, Variant: variant,
			Sent: sent, Total: total, Percent: percent,
		})
		// 日志按 10% 节流（进度事件按 5MB 节流，日志再粗一档）
		if time.Since(lastLogged) >= 5*time.Second {
			lastLogged = time.Now()
			log.Printf("[v4:rom] 主机 %s 上传中: variant=%s %d/%d (%.1f%%) 已耗时 %v",
				ip, variant, sent, total, percent, time.Since(start).Round(time.Second))
		}
	})
	if err != nil {
		return nil, err
	}
	log.Printf("[v4:rom] 主机 %s 上传完成: variant=%s file=%s size=%d ready=%v 耗时=%v",
		ip, variant, res.Filename, res.Size, res.Ready, time.Since(start))
	return &V4RomUploadResult{
		Variant: res.Variant, Filename: res.Filename, Size: res.Size, Ready: res.Ready,
	}, nil
}

// CancelRomUpload 取消一个进行中的 ROM 上传（host+variant 定位）。
// 上传不存在（已完成/已取消/从未开始）时静默返回，不报错——前端取消按钮的
// 竞态窗口（进度事件滞后于上传结束）不需要向用户报错。
func (s *V4ServiceWrapper) CancelRomUpload(host, variant string) error {
	if s.configManager.GetDeployMode() != "v4" {
		return requireV4Mode("ROM 上传")
	}
	ip, err := s.resolveRomHost(host)
	if err != nil {
		return err
	}
	v4RomUploadMu.Lock()
	c, ok := v4RomUploadCancels[romUploadKey(ip, variant)]
	v4RomUploadMu.Unlock()
	if !ok {
		return nil
	}
	log.Printf("[v4:rom] 用户取消上传: host=%s variant=%s（已进行 %v）", ip, variant, time.Since(c.started).Round(time.Second))
	c.cancel()
	return nil
}

// V4RomUploadProgress rom:upload-progress 事件 payload
type V4RomUploadProgress struct {
	Host    string  `json:"host"`
	Variant string  `json:"variant"`
	Sent    int64   `json:"sent"`
	Total   int64   `json:"total"`
	Percent float64 `json:"percent"`
}

// resolveRomHost 解析 ROM 操作的目标主机：显式传入优先，否则用列表里唯一主机。
func (s *V4ServiceWrapper) resolveRomHost(host string) (string, error) {
	if ip := v4HostIP(host); ip != "" {
		return ip, nil
	}
	return s.defaultV4Host()
}

// ListRomImages 拉取镜像变体列表（GET /image/list）。
// host 为空时用设备列表里唯一的主机；多主机必须显式指定（前端有主机下拉）。
func (s *V4ServiceWrapper) ListRomImages(host string) (*V4RomList, error) {
	if s.configManager.GetDeployMode() != "v4" {
		return nil, requireV4Mode("ROM 管理")
	}
	ip, err := s.resolveRomHost(host)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	res, err := newV4InstanceClient(ip).ListImages(ctx)
	if err != nil {
		return nil, err
	}
	out := &V4RomList{
		Host:     ip,
		Root:     res.Root,
		Variants: make([]V4RomVariant, 0, len(res.Variants)),
		Total:    res.Total,
	}
	for _, v := range res.Variants {
		item := V4RomVariant{Variant: v.Variant, Ready: v.Ready}
		for _, f := range v.Files {
			item.Files = append(item.Files, V4RomFile{Name: f.Name, Size: f.Size, Ready: f.Ready})
			item.TotalSize += f.Size
			if !f.Ready {
				item.Missing = append(item.Missing, f.Name)
			}
		}
		out.Variants = append(out.Variants, item)
	}
	// Total 以实际数组长度为准（服务端 total 与 variants 理论上一致，防脏数据）
	out.Total = len(out.Variants)
	return out, nil
}

// PullRomImage 触发一次镜像文件下载（POST /image/pull），返回任务 ID 供前端轮询进度。
// 一个完整变体需要三次调用（system.img / vendor.img / initramfs.img）。
func (s *V4ServiceWrapper) PullRomImage(host, variant, url, filename string) (*V4RomPullResult, error) {
	if s.configManager.GetDeployMode() != "v4" {
		return nil, requireV4Mode("ROM 下载")
	}
	ip, err := s.resolveRomHost(host)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	res, err := newV4InstanceClient(ip).PullImage(ctx, v4host.PullRequest{
		Variant: variant, URL: url, Filename: filename,
	})
	if err != nil {
		return nil, err
	}
	log.Printf("[v4:rom] 主机 %s 已触发下载: variant=%s file=%s task=%s", ip, variant, filename, res.TaskID)
	return &V4RomPullResult{TaskID: res.TaskID, Variant: variant, Filename: filename}, nil
}

// RomImageStatus 查询下载任务进度（GET /image/status/{task_id}）。
func (s *V4ServiceWrapper) RomImageStatus(host, taskID string) (*V4RomTaskStatus, error) {
	if s.configManager.GetDeployMode() != "v4" {
		return nil, requireV4Mode("ROM 下载进度")
	}
	ip, err := s.resolveRomHost(host)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	st, err := newV4InstanceClient(ip).ImageStatus(ctx, taskID)
	if err != nil {
		return nil, err
	}
	return &V4RomTaskStatus{
		TaskID: st.TaskID, Variant: st.Variant, Filename: st.Filename,
		Status: st.Status, Downloaded: st.Downloaded, Total: st.Total, Error: st.Error,
	}, nil
}

// DeleteRomImage 删除一个镜像变体（DELETE /image/{variant}，不可恢复）。
func (s *V4ServiceWrapper) DeleteRomImage(host, variant string) error {
	if s.configManager.GetDeployMode() != "v4" {
		return requireV4Mode("ROM 删除")
	}
	if variant == "" {
		return fmt.Errorf("变体名不能为空")
	}
	ip, err := s.resolveRomHost(host)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	if err := newV4InstanceClient(ip).DeleteImage(ctx, variant); err != nil {
		return err
	}
	log.Printf("[v4:rom] 主机 %s 已删除镜像变体: %s", ip, variant)
	return nil
}

// ListRomHosts 返回可用于 ROM 管理的主机列表（前端多主机时选择目标）。
func (s *V4ServiceWrapper) ListRomHosts() []string {
	if s.configManager.GetDeployMode() != "v4" {
		return nil
	}
	return v4HostsOf(s.configManager.GetDeviceList())
}
