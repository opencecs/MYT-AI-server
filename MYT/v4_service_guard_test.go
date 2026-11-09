package main

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/moyunteng/myt/internal/config"
	"github.com/moyunteng/myt/internal/device/discover"
	"github.com/moyunteng/myt/internal/v4host"
)

// newV4SvcForTest 构造 v4 模式的 V4ServiceWrapper（只注入 config 管理器）。
// scanner 未注入——本测试只验证 guard 互斥/抢占语义，不触达 scanner.Scan
// （ScanV4Hosts 抢到锁后才会调 Scan；抢占/防重入路径都在拿锁阶段返回）。
func newV4SvcForTest(t *testing.T) *V4ServiceWrapper {
	t.Helper()
	dir := t.TempDir()
	// config.NewManager 不自建目录，SetDeployMode 持久化写入需要 config/ 子目录
	if err := os.MkdirAll(filepath.Join(dir, "config"), 0o755); err != nil {
		t.Fatalf("创建 config 目录失败: %v", err)
	}
	cm := config.NewManager(dir)
	if err := cm.SetDeployMode("v4"); err != nil {
		t.Fatalf("SetDeployMode(v4) 失败: %v", err)
	}
	return &V4ServiceWrapper{configManager: cm}
}

// TestV4ScanBusyOnConcurrentUserScans 两个用户扫描并发：后到的报 errV4ScanBusy。
func TestV4ScanBusyOnConcurrentUserScans(t *testing.T) {
	svc := newV4SvcForTest(t)

	// 占住用户锁（模拟一轮用户扫描进行中）
	if ok, cur := svc.scan.tryLock("user"); !ok {
		t.Fatalf("首次 tryLock(user) 应成功，cur=%q", cur)
	}
	_, err := svc.ScanV4Hosts()
	if err == nil {
		t.Fatal("用户扫描撞用户扫描应报 errV4ScanBusy，实际无错误")
	}
	if err != errV4ScanBusy {
		t.Errorf("错误应为 errV4ScanBusy，实际: %v", err)
	}
	// 锁仍在原持有者手里（ScanV4Hosts 未越权释放）
	if ok, cur := svc.scan.tryLock("user"); ok || cur != "user" {
		t.Errorf("原持有者应仍是 user，ok=%v cur=%q", ok, cur)
	}
	svc.scan.unlock()
}

// blockingDiscoverer 假发现器：第 1 次 Discover 阻塞到 ctx 取消（模拟后台扫描
// 的 UDP 等待窗口，可被抢占取消），第 2 次起立即返回空结果（用户扫描接管后
// 快速走完 Scan，不引入真实网络等待）。
type blockingDiscoverer struct {
	calls int32
}

func (d *blockingDiscoverer) Discover(ctx context.Context) ([]discover.Device, error) {
	if atomic.AddInt32(&d.calls, 1) == 1 {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return nil, nil
}

// TestV4UserScanPreemptsBackgroundScan 用户扫描撞后台静默扫描：取消对方、接管成功
// （不再报「稍后再试」——即「切到 V4 立即点添加设备」主诉求的锁语义回归）。
func TestV4UserScanPreemptsBackgroundScan(t *testing.T) {
	svc := newV4SvcForTest(t)
	// 注入真实 Scanner + 假发现器，让后台扫描走 ScanForDiscover 的完整真实路径
	//（拿锁 → 挂 cancel → Scan 响应 ctx 取消 → unlock）
	d := &blockingDiscoverer{}
	svc.scanner = v4host.NewScanner(d, svc.configManager)

	// 后台静默扫描（模拟切模式触发的 RefreshNow → discoverAndUpdate v4 分支）
	bgDone := make(chan error, 1)
	go func() { bgDone <- svc.ScanForDiscover(context.Background(), nil) }()

	// 给后台扫描一点时间拿锁（扫描在 UDP 等待窗口阻塞）
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if svc.scan.busy() {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !svc.scan.busy() {
		t.Fatal("后台扫描未能在 2s 内拿到锁（测试前置失败）")
	}

	// 用户扫描：应取消后台批次并接管，不报 busy
	_, err := svc.ScanV4Hosts()
	if err != nil {
		t.Fatalf("用户扫描应抢占后台扫描成功，实际报错: %v", err)
	}
	// 后台扫描已退出且静默让位
	select {
	case bgErr := <-bgDone:
		if bgErr != nil {
			t.Errorf("后台扫描被抢占后应静默退出，实际: %v", bgErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("后台扫描未在用户抢占后 2s 内退出")
	}
	// 锁已释放，可再次获取
	if ok, _ := svc.scan.tryLock("user"); !ok {
		t.Error("用户扫描结束后锁应可再次获取")
	} else {
		svc.scan.unlock()
	}
}

// TestV4BackgroundScanYieldsToUserScan 后台静默扫描撞用户扫描：静默让位（返回 nil，
// 不报错不挡用户），下一轮 discoverAndUpdate 再来。
func TestV4BackgroundScanYieldsToUserScan(t *testing.T) {
	svc := newV4SvcForTest(t)

	if ok, _ := svc.scan.tryLock("user"); !ok {
		t.Fatal("占住用户锁失败")
	}
	defer svc.scan.unlock()

	if err := svc.ScanForDiscover(context.Background(), nil); err != nil {
		t.Errorf("后台扫描撞用户扫描应静默让位（nil），实际: %v", err)
	}
	// 锁未被后台越权释放
	if ok, cur := svc.scan.tryLock("user"); ok || cur != "user" {
		t.Errorf("原持有者应仍是 user，ok=%v cur=%q", ok, cur)
	}
}

// TestV4BackgroundScanAttachesCancel 后台扫描持锁期间取消函数已挂 guard：
// 用户扫描的 cancelOwner 能级联取消扫描 ctx（在途 /health 探测提前退出）。
func TestV4BackgroundScanAttachesCancel(t *testing.T) {
	svc := newV4SvcForTest(t)

	// 后台扫描真实走 ScanForDiscover 的拿锁+挂 cancel 路径：scanner 为 nil，
	// Scan 会 panic——用 injectable 入口不可行（Scan 是方法），改为直接验证
	// guard 行为：拿锁 → attachCancel → cancelOwner 生效。
	if ok, _ := svc.scan.tryLock("background"); !ok {
		t.Fatal("后台 tryLock 应成功")
	}
	ctx, cancel := context.WithCancel(context.Background())
	svc.scan.attachCancel(cancel)
	svc.scan.cancelOwner()
	select {
	case <-ctx.Done():
	default:
		t.Error("cancelOwner 应级联取消已 attach 的扫描 ctx")
	}
	svc.scan.unlock()
}

// TestV4ScanPreemptWaitTimeout 后台批次在 waitDone 上限内不退出时，
// 用户扫描本次报忙（不无限等）。
func TestV4ScanPreemptWaitTimeout(t *testing.T) {
	if v4ScanPreemptWait != 3*time.Second {
		t.Fatalf("v4ScanPreemptWait 应与 v2ScanPreemptWait 同值 3s，实际 %v", v4ScanPreemptWait)
	}
	svc := newV4SvcForTest(t)

	// 占后台锁且 cancel 不退出批次：waitDone 3s 超时 → 报忙。
	// 不真等 3s：直接缩短验证——手动模拟「tryLock 失败 + 未释放」的终态
	if ok, _ := svc.scan.tryLock("background"); !ok {
		t.Fatal("占后台锁失败")
	}
	// 不等真实 3s（拖慢测试）：只验证 ScanV4Hosts 在锁被后台长期占住时
	// 不会无限阻塞——用 goroutine + 超时兜底。
	done := make(chan struct{})
	var err error
	go func() {
		defer close(done)
		_, err = svc.ScanV4Hosts()
	}()
	select {
	case <-done:
		if err == nil {
			t.Error("后台批次不退出时用户扫描应报忙，实际成功")
		}
	case <-time.After(4 * time.Second):
		t.Fatal("用户扫描被后台批次卡住超过 4s（waitDone 未生效）")
	}
	svc.scan.unlock()
}

// TestV4ScanForDiscoverModeGuard 非 v4 模式下静默扫描直接跳过（不碰锁）。
func TestV4ScanForDiscoverModeGuard(t *testing.T) {
	cm := config.NewManager(t.TempDir())
	svc := &V4ServiceWrapper{configManager: cm} // 默认 local 模式
	if err := svc.ScanForDiscover(context.Background(), nil); err != nil {
		t.Errorf("非 v4 模式应直接返回 nil，实际: %v", err)
	}
	if ok, _ := svc.scan.tryLock("user"); !ok {
		t.Error("锁不应被非 v4 调用占用")
	} else {
		svc.scan.unlock()
	}
}

// TestV4ScanGuardConcurrentPreempt 并发压测：多轮后台扫描 + 用户扫描交替，
// 锁必须最终可被用户拿到（无死锁），且同一时刻只有一个持有者。
// （scanGuard 内部 mu 保护 owner，不做双重持有断言——由 tryLock 唯一性保证。）
func TestV4ScanGuardConcurrentPreempt(t *testing.T) {
	svc := newV4SvcForTest(t)
	var userWon int32

	// 后台扫描反复占锁（模拟切模式后的多轮静默发现）
	stop := make(chan struct{})
	go func() {
		for {
			select {
			case <-stop:
				return
			default:
			}
			if ok, _ := svc.scan.tryLock("background"); ok {
				time.Sleep(20 * time.Millisecond)
				svc.scan.unlock()
			}
			time.Sleep(5 * time.Millisecond)
		}
	}()

	// 用户扫描：应在抢占等待窗口内拿到锁
	done := make(chan struct{})
	go func() {
		defer close(done)
		ok, cur := svc.scan.tryLock("user")
		if !ok {
			// 撞后台 → 取消 + 等待接管（复刻 ScanV4Hosts 的抢占路径）
			if cur == "background" {
				svc.scan.cancelOwner()
				svc.scan.waitDone(v4ScanPreemptWait)
				_, cur = svc.scan.tryLock("user")
			}
		}
		if cur == "" {
			atomic.StoreInt32(&userWon, 1)
		}
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		close(stop)
		t.Fatal("用户扫描未在 5s 内完成（疑似死锁）")
	}
	close(stop)
	if atomic.LoadInt32(&userWon) != 1 {
		t.Error("用户扫描最终应拿到锁（用户优先语义失效）")
	}
}

// 兜底：等待窗口内后台批次退出时 waitDone 提前返回（不空耗 3s）。
func TestV4WaitDoneEarlyReturn(t *testing.T) {
	svc := newV4SvcForTest(t)
	if ok, _ := svc.scan.tryLock("background"); !ok {
		t.Fatal("占后台锁失败")
	}
	go func() {
		time.Sleep(50 * time.Millisecond)
		svc.scan.unlock()
	}()
	start := time.Now()
	svc.scan.waitDone(v4ScanPreemptWait)
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("waitDone 应在批次退出后提前返回，实际耗时 %v", elapsed)
	}
}
