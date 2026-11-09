package main

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/moyunteng/myt/internal/config"
	"github.com/moyunteng/myt/internal/device/discover"
	"github.com/moyunteng/myt/internal/v4host"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// errV4ScanBusy V4 扫描防重入错误。文案与 v2 的 errV2ScanBusy 同口径（含「稍后再试」），
// 前端 AddDeviceScanTab 据此把「忙」区别于「没扫到设备」并 toast。
var errV4ScanBusy = errors.New("正在扫描 tebox 设备，请稍后再试")

// v4ScanPreemptWait 用户扫描等待被打断的后台批次退出的时间上限（与 v2ScanPreemptWait
// 同值）：后台批次收到取消后 UDP 发现阶段立即退出，在途单台探测（最长 1.5s）被 ctx
// 级联中断，通常远快于此上限；超时则用户扫描本次报忙，下轮再试。
const v4ScanPreemptWait = 3 * time.Second

// V4ServiceWrapper V4 模式服务：设备发现 = UDP 7678 广播（复用 discover.Discoverer）
// + 8088 /health 验证（方案 A）。扫描结果按 DeviceID 分派：已在正式列表 → 刷新信息并
// 置在线；不在 → 进「待添加」。不复制 v2 的型号白名单（V4 无白名单信息）。
//
// 扫描互斥：复用 v2 的 scanGuard 抢占语义（不与 v2 共享实例）——
//   - 用户扫描（ScanV4Hosts）撞后台静默扫描 → 取消对方 + 短暂等待接管（用户优先）
//   - 用户扫描撞另一轮用户扫描 → 报 errV4ScanBusy（防连点）
//   - 静默扫描（ScanForDiscover）撞任何持有者 → 静默让位，失败只落日志
//     （下一轮 discoverAndUpdate 会再来，自动发现不丢功能）
type V4ServiceWrapper struct {
	scanner       *v4host.Scanner
	configManager *config.Manager
	app           *application.App
	scan          scanGuard
	// refreshInstances 创建云机后的 REST 补拉回调（setup.go 注入，见 v4_create.go）
	refreshInstances func(ip string)
}

// NewV4ServiceWrapper 创建 V4 服务（发现器复用 local/v2 的同一个 UDP Discoverer）
func NewV4ServiceWrapper(cm *config.Manager, d *discover.Discoverer) *V4ServiceWrapper {
	s := &V4ServiceWrapper{
		scanner:       v4host.NewScanner(d, cm),
		configManager: cm,
	}
	// 事件推送：pendinglist:updated / devicelist:updated（前端雷达与待添加列表依赖）
	s.scanner.SetEmitter(func(event string, data any) {
		if s.app == nil || s.app.Event == nil {
			return
		}
		s.app.Event.Emit(event, data)
	})
	return s
}

func (s *V4ServiceWrapper) SetApp(app *application.App) { s.app = app }

// ScanV4Hosts 用户显式扫描（前端「开始扫描」）：UDP 广播发现 + 8088 /health 验证。
// 抢占语义（与 v2 的 ScanV2Hosts 同构）：
//   - 后台静默扫描在跑 → 取消对方、短暂等待退出后接管（用户优先，静默扫描
//     下一轮 discoverAndUpdate 会再来）
//   - 另一轮用户扫描在跑 → 报 errV4ScanBusy（不能静默——否则「忙」会被理解成「没设备」）
// 返回本轮验证通过的设备（前端雷达/日志展示用）。
func (s *V4ServiceWrapper) ScanV4Hosts() ([]v4host.Result, error) {
	if s.configManager.GetDeployMode() != "v4" {
		return nil, nil
	}
	ok, cur := s.scan.tryLock("user")
	if !ok {
		if cur != "background" {
			log.Printf("[v4] 扫描防重入：另一轮用户扫描进行中，拒绝本次扫描")
			return nil, errV4ScanBusy
		}
		// 用户优先：打断后台批次，短暂等待其退出后接管
		log.Printf("[v4] 用户扫描打断后台扫描/刷新，等待其退出")
		s.scan.cancelOwner()
		s.scan.waitDone(v4ScanPreemptWait)
		if ok, cur = s.scan.tryLock("user"); !ok {
			log.Printf("[v4] 后台批次未在 %s 内退出，本次扫描报忙", v4ScanPreemptWait)
			return nil, errV4ScanBusy
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.scan.attachCancel(cancel)
	defer s.scan.unlock()
	return s.scanner.Scan(ctx, nil)
}

// ScanForDiscover 发现主流程（discoverAndUpdate 的 v4 分支）调用的静默扫描入口：
// 抢不到锁（用户扫描/刷新/另一轮后台在跑）静默跳过（不与用户操作争抢），失败只落日志。
// commitValid 为提交前代际校验（模式代际未变才写库），由 DiscoverServiceWrapper 传入。
func (s *V4ServiceWrapper) ScanForDiscover(ctx context.Context, commitValid func() bool) error {
	if s.configManager.GetDeployMode() != "v4" {
		return nil
	}
	if ok, _ := s.scan.tryLock("background"); !ok {
		log.Printf("[v4] 扫描/刷新进行中，跳过本轮发现")
		return nil
	}
	// 从入参 ctx 派生（保留 discoverAndUpdate 上游取消链），cancel 挂到 guard 上：
	// 用户扫描抢占时 guard.cancelOwner() 触发它级联中断在途探测；unlock 时兜底回收。
	scanCtx, cancel := context.WithCancel(ctx)
	s.scan.attachCancel(cancel)
	defer s.scan.unlock()
	if _, err := s.scanner.Scan(scanCtx, commitValid); err != nil {
		// 被用户扫描抢占（ctx 取消）= 正常让位，不算失败——按失败上抛会让
		// RefreshDevices 等同步路径把「让位」toast 成「扫描失败」
		if errors.Is(err, context.Canceled) {
			log.Printf("[v4] 后台扫描被用户操作抢占，静默让位")
			return nil
		}
		log.Printf("[v4] 发现扫描失败: %v", err)
		return err
	}
	return nil
}

// ProbeV4IPs 定向探测一批 IP（手动添加用）：逐台 GET /health。
// 返回成功设备信息与无响应 IP（顺序与入参一致）。不走扫描锁——与 v2 的
// AddV2Hosts 探测同语义，手动添加是明确的用户意图，不需要和广播扫描互斥。
func (s *V4ServiceWrapper) ProbeV4IPs(ctx context.Context, ips []string) ([]v4host.Info, []string) {
	if s.configManager.GetDeployMode() != "v4" || len(ips) == 0 {
		return nil, ips
	}
	found, failed := s.scanner.ProbeIPs(ctx, ips)
	infos := make([]v4host.Info, 0, len(found))
	for _, info := range found {
		if info == nil {
			continue
		}
		infos = append(infos, *info)
	}
	return infos, failed
}
