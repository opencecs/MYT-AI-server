package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/moyunteng/myt/internal/v4host"
)

// --- V4 创建云机 / 镜像列表（Wails 暴露）---
//
// 与 V3 的 CreateAndroid 语义差异（故不复用其签名）：
//   - V3 有坑位号 / 机型 / 镜像 URL / 网络等十多个参数；V4 只有资源配置 + 后端 +
//     镜像（variant 或三件套路径），其余由镜像决定。
//   - V4 创建后实例是 stopped，需要再 POST /android/start（是否自动开机由调用方
//     用 start 参数决定，与 V3 创建弹窗的默认行为一致：默认开机）。
//
// 刷新：创建/开机后走一次 REST 全量补拉写缓存并推 androidlist:updated——
// WS 的 instance 事件可能滞后（尤其刚创建的实例），REST 是权威事实。

// V4CreateInstanceParams 创建云机参数（前端 CreateInstanceV4Dialog 提交）
type V4CreateInstanceParams struct {
	// Host 主机 IP（可为 ip:8088；空则用设备列表里唯一的主机）
	Host string `json:"host"`
	// Name 实例显示名（空 = 服务端自动生成）
	Name string `json:"name"`
	// CPUCores / MemoryMB / DiskMB 资源配置；0 = 服务端默认（4 核 / 4096 / 8192）
	CPUCores int32 `json:"cpuCores"`
	MemoryMB int32 `json:"memoryMB"`
	DiskMB   int32 `json:"diskMB"`
	// Backend 目标 tebox 后端（空 = 服务端默认后端）
	Backend string `json:"backend"`
	// Variant 镜像变体名（来自 ListImages；与三件套路径二选一）
	Variant string `json:"variant"`
	// Start 创建后是否立即开机（默认 true，与 V3 创建弹窗默认行为一致）
	Start bool `json:"start"`
}

// V4CreatedInstance 创建结果（回传前端展示用）
type V4CreatedInstance struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Backend string `json:"backend"`
	State   string `json:"state"`
	// Started 是否已成功发起开机（start=false 或开机失败时 false）
	Started bool `json:"started"`
	// StartError 开机失败原因（实例已创建成功，不阻断整体流程，与 V3 同语义）
	StartError string `json:"startError"`
}

// V4ImageOption 镜像下拉选项（只暴露前端需要的字段）
type V4ImageOption struct {
	Variant string `json:"variant"`
	// Ready 三件套齐全可用；false 的变体在前端置灰
	Ready bool `json:"ready"`
}

// V4BackendOption 后端下拉选项（多套 tebox 栈时前端要选打到哪一套）
type V4BackendOption struct {
	Name    string `json:"name"`
	Address string `json:"address"`
	Status  string `json:"status"`
	Default bool   `json:"default"`
}

// requireV4Mode V4 专属接口的模式校验（非 v4 模式明确报错，避免静默无操作）
func requireV4Mode(op string) error {
	return fmt.Errorf("「%s」仅在 tebox 模式下可用", op)
}

// defaultV4Host 未指定主机时取设备列表里唯一的主机（多主机必须显式指定）。
func (s *V4ServiceWrapper) defaultV4Host() (string, error) {
	hosts := v4HostsOf(s.configManager.GetDeviceList())
	switch len(hosts) {
	case 0:
		return "", fmt.Errorf("还没有添加 tebox 主机，请先到「主机」页添加设备")
	case 1:
		return hosts[0], nil
	default:
		return "", fmt.Errorf("当前有 %d 台 tebox 主机，请指定目标主机", len(hosts))
	}
}

// ListImages 拉取指定主机的镜像变体列表（GET /image/list），供创建弹窗下拉。
func (s *V4ServiceWrapper) ListImages(host string) ([]V4ImageOption, error) {
	if s.configManager.GetDeployMode() != "v4" {
		return nil, requireV4Mode("镜像列表")
	}
	ip := v4HostIP(host)
	if ip == "" {
		var err error
		if ip, err = s.defaultV4Host(); err != nil {
			return nil, err
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	res, err := newV4InstanceClient(ip).ListImages(ctx)
	if err != nil {
		return nil, err
	}
	options := make([]V4ImageOption, 0, len(res.Variants))
	for _, v := range res.Variants {
		options = append(options, V4ImageOption{Variant: v.Variant, Ready: v.Ready})
	}
	return options, nil
}

// ListBackends 拉取指定主机的 tebox 后端列表（GET /backend/list），供创建弹窗选后端。
func (s *V4ServiceWrapper) ListBackends(host string) ([]V4BackendOption, error) {
	if s.configManager.GetDeployMode() != "v4" {
		return nil, requireV4Mode("后端列表")
	}
	ip := v4HostIP(host)
	if ip == "" {
		var err error
		if ip, err = s.defaultV4Host(); err != nil {
			return nil, err
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	res, err := newV4InstanceClient(ip).ListBackends(ctx)
	if err != nil {
		return nil, err
	}
	options := make([]V4BackendOption, 0, len(res.Backends))
	for _, b := range res.Backends {
		options = append(options, V4BackendOption{
			Name: b.Name, Address: b.Address, Status: b.Status, Default: b.Default,
		})
	}
	return options, nil
}

// CreateInstance 创建云机（POST /android/create，可选随后 POST /android/start）。
//
// 与 V3 一致的行为约定：创建成功即返回（不等待开机完成），开机失败只回传
// StartError 而不报错——实例已建出，用户可以手动点「启动」。
func (s *V4ServiceWrapper) CreateInstance(p V4CreateInstanceParams) (*V4CreatedInstance, error) {
	if s.configManager.GetDeployMode() != "v4" {
		return nil, requireV4Mode("创建云机")
	}
	ip := v4HostIP(p.Host)
	if ip == "" {
		var err error
		if ip, err = s.defaultV4Host(); err != nil {
			return nil, err
		}
	}
	client := newV4InstanceClient(ip)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	created, err := client.Create(ctx, v4host.CreateRequest{
		Name:     p.Name,
		CPUCores: p.CPUCores,
		MemoryMB: p.MemoryMB,
		DiskMB:   p.DiskMB,
		Backend:  p.Backend,
		Variant:  p.Variant,
	})
	if err != nil {
		return nil, err
	}
	inst := created.Instance
	ref := v4host.InstanceRef{ID: inst.ID, Backend: inst.Backend}
	if ref.Backend == "" {
		ref.Backend = p.Backend
	}
	log.Printf("[v4] 实例已创建: host=%s backend=%s id=%s name=%s variant=%s",
		ip, ref.Backend, inst.ID, inst.Name, p.Variant)

	result := &V4CreatedInstance{
		ID:      inst.ID,
		Name:    inst.Name,
		Backend: ref.Backend,
		State:   inst.State,
	}
	if p.Start {
		// 开机是异步的（实测约 70s 才 running，接口在 starting 即返回），不等结果
		if err := client.Start(ctx, ref); err != nil {
			log.Printf("[v4] 实例已创建但开机失败: %s/%s err=%v", ref.Backend, ref.ID, err)
			result.StartError = err.Error()
		} else {
			result.Started = true
		}
	}
	s.refreshAfterCreate(ip)
	return result, nil
}

// refreshAfterCreate 创建后立即 REST 补拉一次该主机实例列表并推前端。
// WS 的 instance 事件可能滞后（刚创建的实例尤其如此），REST 是权威事实；
// 走与 v4ws 适配层同一个 indexer，保证截图/投屏 key 一致。
func (s *V4ServiceWrapper) refreshAfterCreate(ip string) {
	if s.refreshInstances != nil {
		s.refreshInstances(ip)
		return
	}
	// 未注入回调（理论不会发生）：至少记日志，便于排查「创建后列表没刷新」
	log.Printf("[v4] 创建后刷新回调未注入，主机 %s 的实例列表需手动刷新", ip)
}

// SetRefreshInstances 注入「创建后 REST 补拉并推前端」的回调（setup.go 挂接
// sdkApiSvc 的列表查询 + 事件推送，避免本文件直接依赖 sdkapi 服务）。
func (s *V4ServiceWrapper) SetRefreshInstances(fn func(ip string)) {
	s.refreshInstances = fn
}
