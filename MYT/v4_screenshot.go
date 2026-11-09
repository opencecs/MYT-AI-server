// v4_screenshot.go — V4（tobox_player）云机截图接入。
//
// 背景：V4 模式复用 HostManagement 页面，卡片结构与 V3 完全一致，但此前 tobox 没有
// 截图接口，卡片只能显示「暂无截图」占位。tobox_player v0.8.x 新增
// GET /android/:id/screenshot?backend=，返回 PNG 原始字节，故此处把 V4 接进既有的
// 截图服务（internal/device/screenshot）：缓存、版本号、screenshot:updated /
// screenshot:invalidated 事件全部复用，前端只去掉占位分支即可显示真实画面。
//
// key 对齐：截图 key 仍是 "IP_indexNum"，其中 indexNum 由 v4Indexer 派生（WS 快照与
// REST 补拉共用同一份，见 v4_indexer.go），与前端 ScreenshotView 的
// `${a.ip}_${a.indexNum}` 天然一致——因此这里不再自行编号，直接沿用缓存条目的 IndexNum。
package main

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/moyunteng/myt/internal/config"
	"github.com/moyunteng/myt/internal/device/screenshot"
	"github.com/moyunteng/myt/internal/v4host"
	"github.com/moyunteng/myt/internal/v4ws"
)

const (
	// v4ShotPerHostConcurrency 单台 V4 主机同时进行的截图请求上限。
	// 服务端每张图要抽帧 + 容器内 ffmpeg 解码（实测单张 ~300ms，上限 5s），且**同一实例
	// 并发只允许 1 个进行中**（超出 429）。V4 实例数通常不多，但也不能一次全抓：
	// 4 路并发既能把首屏 4 台以内的画面秒级铺满，又不会把宿主机 CPU 打满。
	v4ShotPerHostConcurrency = 4
	// v4ShotFetchTimeout 单个目标的「排队 + 请求」总时限。
	// 截图服务传进来的 ctx 是服务级 ctx（无超时，见 fetchSingle 的 9082 路径自行加
	// fetchTimeout），若排队无限等，实例多的主机上 worker 会全卡在信号量上。
	v4ShotFetchTimeout = 15 * time.Second
	// v4ShotWidth 请求服务端把截图输出缩放到该宽度（px），高度由源图比例决定。
	//
	// 服务端 width/height 参数语义 = 输出 PNG 像素尺寸（ffmpeg 缩放，仅 cast 路径），
	// 实测 10.10.2.142:8088：源图 720x1600 → width=480 得 480x1066，width=360 得
	// 360x800，体积 266KB → 162KB / 105KB（PNG 无损，与像素数近线性）。
	//
	// 取 320 的理由：竖屏卡片基准列宽 150 CSS px，滑条最大 150% → 225 CSS px；
	// 客户端 DPR 实际 1~1.5 → 225~337 物理px，320 已覆盖全部场景（此前按 2x DPR
	// 取 480，但 WebView2 下 2x 缩放并不出现，白带了 1/3 像素）。再小（如 240）
	// 在 337px 显示宽下会肉眼可见发糊。不传 height，让各 ROM 保持自己的宽高比
	// （前端 object-contain 完整显示，不裁切）。
	//
	// 配合 storeImage 的 PNG→JPEG 重压缩（screenshot_recompress.go），单张 IPC
	// 载荷从 ~200KB PNG（base64 ~300KB）降到 ~15~30KB（base64 ~20~40KB）。
	v4ShotWidth = 320
)

// v4ShotLimiter 按主机限制截图并发（懒建信号量；主机数很少，不做过期回收）。
type v4ShotLimiter struct {
	mu   sync.Mutex
	sems map[string]chan struct{}
}

// acquire 取一个并发位；ctx 结束则放弃等待（截图轮询不能因限流永久阻塞）。
func (l *v4ShotLimiter) acquire(ctx context.Context, host string) (func(), error) {
	if ctx == nil {
		ctx = context.Background()
	}
	l.mu.Lock()
	if l.sems == nil {
		l.sems = make(map[string]chan struct{})
	}
	sem, ok := l.sems[host]
	if !ok {
		sem = make(chan struct{}, v4ShotPerHostConcurrency)
		l.sems[host] = sem
	}
	l.mu.Unlock()

	select {
	case sem <- struct{}{}:
		return func() { <-sem }, nil
	case <-ctx.Done():
		return nil, fmt.Errorf("等待截图并发位超时: host=%s err=%w", host, ctx.Err())
	}
}

// v4ScreenshotTargets 构建 V4 截图目标集：该主机下所有 running 实例。
//
// 只取 running：tobox 对未 running 实例返回 503（容器不存在 / guest 未就绪），
// 抓也抓不到，白占并发位还会刷失败日志。注意这里用精确 running 而非
// GetRunningAndroids 的宽口径（v2 稳定性修复后后者含 starting/restarting 等）：
// V4 实例状态来自 WS snapshot，无 v2 的 state 抖动问题，维持原口径。
func v4ScreenshotTargets(cm *config.Manager) []screenshot.ContainerTarget {
	androids := cm.GetRunningAndroids()
	targets := make([]screenshot.ContainerTarget, 0, len(androids))
	for _, a := range androids {
		if a.Status != "running" {
			continue
		}
		// 缓存条目 ID = "backend/id"（v4CacheItem），拆回 tobox 的实例主键
		backend, id := v4ws.SplitInstanceKey(a.ID)
		if id == "" {
			continue
		}
		targets = append(targets, screenshot.ContainerTarget{
			DeviceID:   a.DeviceID,
			DeviceIP:   a.IP,
			IndexNum:   a.IndexNum,
			InstanceID: id,
			Backend:    backend,
		})
	}
	screenshot.SortTargets(targets, "v4")
	return targets
}

// newV4ScreenshotFetcher 构造 V4 抓图回调（注入 screenshot.Service.SetTargetFetcher）。
//
// 每次抓图新建 InstanceClient（轻量、复用全局 Transport），并发由 limiter 按主机限制。
func newV4ScreenshotFetcher(cm *config.Manager) screenshot.TargetFetcher {
	lim := &v4ShotLimiter{}
	return func(ctx context.Context, t screenshot.ContainerTarget) ([]byte, string, error) {
		// 模式守卫：切走 v4 后残余目标不得再打 8088（切模式时缓存已清，这里只是兜底）
		if cm != nil && cm.GetDeployMode() != "v4" {
			return nil, "", fmt.Errorf("当前非 v4 模式，跳过截图")
		}
		if t.DeviceIP == "" || t.InstanceID == "" {
			return nil, "", fmt.Errorf("tebox 截图目标不完整: host=%q instance=%q", t.DeviceIP, t.InstanceID)
		}
		// 单目标总时限：排队 + 请求都不超过 v4ShotFetchTimeout（服务端单张上限 5s）
		reqCtx, cancel := context.WithTimeout(ctx, v4ShotFetchTimeout)
		defer cancel()

		release, err := lim.acquire(reqCtx, t.DeviceIP)
		if err != nil {
			return nil, "", err
		}
		defer release()

		data, contentType, err := newV4InstanceClient(t.DeviceIP).Screenshot(reqCtx,
			v4host.InstanceRef{ID: t.InstanceID, Backend: t.Backend}, v4ShotWidth)
		if err != nil {
			return nil, "", err
		}
		if len(data) == 0 {
			return nil, "", fmt.Errorf("截图响应为空: %s/%s", t.Backend, t.InstanceID)
		}
		return data, contentType, nil
	}
}

// v4ScreenshotInvalidator 实例状态变化 → 清该实例截图缓存 + 立即重抓一轮。
//
// 与 V3 的 androidlist.SetScreenshotEventCallback 同款语义（V3 也是直接调 service 的
// InvalidateSlot，不发 screenshot:invalidated）：状态转换时画面必然变化，不清缓存的话
// 卡片会继续显示旧画面直到下一轮内容比较发现差异。cm 用于把实例主键反查成 indexNum。
func v4ScreenshotInvalidator(cm *config.Manager, ss *screenshot.Service) func(hostIP, containerID string) {
	return func(hostIP, containerID string) {
		if cm == nil || ss == nil {
			return
		}
		indexNum := 0
		for _, a := range cm.GetAndroidsByDevice(hostIP) {
			if a.ID == containerID {
				indexNum = a.IndexNum
				break
			}
		}
		if indexNum <= 0 {
			// 缓存里查不到（刚删除/快照未到）：整机失效兜底，避免残留旧图
			ss.InvalidateDevice(hostIP)
		} else {
			ss.InvalidateSlot(hostIP, indexNum)
		}
		ss.TriggerNow()
	}
}
