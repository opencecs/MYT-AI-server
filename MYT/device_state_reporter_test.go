package main

// device_state_reporter_test.go — StateReporter 拆分后的行为回归测试。
// 原实现是 setup.go 内嵌闭包（局部状态，不可测）：记忆节流/过期钩子守卫/
// 状态守卫此前零覆盖，拆分字段化 + 函数注入后解锁。

import (
	"sync"
	"testing"
	"time"

	"github.com/moyunteng/myt/internal/config"
)

// newTestReporter 测试构造：注入假探测/截图/emit，cm 用临时目录。
func newTestReporter(t *testing.T) (*deviceStateReporter, *config.Manager, *probeRecorder) {
	t.Helper()
	cm := config.NewManager(t.TempDir())
	if err := cm.InitDirs(); err != nil {
		t.Fatalf("InitDirs: %v", err)
	}
	rec := &probeRecorder{}
	r := &deviceStateReporter{
		cm:            cm,
		sdkApiSvc:     &SdkApiServiceWrapper{}, // emitEvent 空实现（app 为 nil 时 no-op）
		screenshotSvc: nil,
	}
	// 函数注入面（devicedetail/screenshot 不引入，纯行为测试）
	r.querySync = rec.querySync
	r.queryOnce = rec.queryOnce
	r.invalidate = rec.invalidate
	r.trigger = rec.trigger
	return r, cm, rec
}

// probeRecorder 探测/清理调用的记录器。
type probeRecorder struct {
	mu          sync.Mutex
	probeCalls  int
	queryCalls  int
	invalidated []string
	triggers    int
	result      int // querySync 返回值：0=在线 1=认证 2=不可达
}

func (p *probeRecorder) querySync(deviceID, ip string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.probeCalls++
	return p.result
}
func (p *probeRecorder) queryOnce(deviceID, ip string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.queryCalls++
}
func (p *probeRecorder) invalidate(key string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.invalidated = append(p.invalidated, key)
}
func (p *probeRecorder) trigger() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.triggers++
}

func (p *probeRecorder) probes() int { p.mu.Lock(); defer p.mu.Unlock(); return p.probeCalls }

// addDevice 建设备并等待异步探测落定（Handle 的探测在 goroutine 里）。
func waitProbes(t *testing.T, rec *probeRecorder, want int) {
	t.Helper()
	deadline := probeLoop(50, func() bool { return rec.probes() >= want })
	if !deadline {
		t.Fatalf("探测次数不足：期望 %d，实际 %d", want, rec.probes())
	}
}

func probeLoop(intervalMs int, cond func() bool) bool {
	for i := 0; i < 200; i++ {
		if cond() {
			return true
		}
		sleepMs(intervalMs)
	}
	return cond()
}

// TestProbeMemoryThrottle 回归：复核成功记忆 60s——记忆期内重复离线上报
// 不再探测（防 404 退避循环秒级 HTTP 轰炸设备）。
func TestProbeMemoryThrottle(t *testing.T) {
	r, cm, rec := newTestReporter(t)
	cm.SetDeviceList([]config.DeviceCache{{DeviceID: "d1", IP: "10.0.0.1", State: 0}})
	rec.result = 0 // 探测成功（老 SDK 在线）

	r.Handle("d1", 1) // WS 判离线 → 探测
	waitProbes(t, rec, 1)
	if got := cm.GetDevice("d1").State; got != 0 {
		t.Fatalf("探测成功应写在线：state=%d", got)
	}
	// 记忆期内的重复离线上报：不重复探测（probeCalls 不增长）
	for i := 0; i < 3; i++ {
		r.Handle("d1", 1)
	}
	if got := rec.probes(); got != 1 {
		t.Fatalf("记忆期内重复上报不应重复探测：probes=%d（期望恒 1）", got)
	}
}

// TestProbeUnreachableWritesOffline 回归：探测不可达写离线 + 清理；
// 重复上报（30s 兜底转发）不再重复写状态（守卫）。
func TestProbeUnreachableWritesOffline(t *testing.T) {
	r, cm, rec := newTestReporter(t)
	cm.SetDeviceList([]config.DeviceCache{{DeviceID: "d1", IP: "10.0.0.1", State: 1}})
	rec.result = 2 // 不可达

	r.Handle("d1", 1)
	waitProbes(t, rec, 1)
	if got := cm.GetDevice("d1").State; got != 1 {
		t.Fatalf("探测失败应维持离线：state=%d", got)
	}
	if len(rec.invalidated) == 0 {
		t.Fatalf("探测失败应失效截图缓存")
	}
}

// TestOnlineReportSkipsProbe 回归：在线上报（state=0）不走探测分支，
// 且清记忆（设备升级出 WS 端点后恢复 WS 链路）。
func TestOnlineReportSkipsProbe(t *testing.T) {
	r, cm, rec := newTestReporter(t)
	cm.SetDeviceList([]config.DeviceCache{{DeviceID: "d1", IP: "10.0.0.1", State: 1}})

	r.Handle("d1", 0)
	if got := rec.probes(); got != 0 {
		t.Fatalf("在线上报不应触发探测：probes=%d", got)
	}
	if got := cm.GetDevice("d1").State; got != 0 {
		t.Fatalf("在线上报应写在线：state=%d", got)
	}
	if rec.triggers == 0 {
		t.Fatalf("在线上报应触发截图刷新")
	}
}

// TestAuthStateNotOverwritten 回归：认证态(2)设备非 2 上报被忽略
// （认证链路独占管理，探活不覆盖）。
func TestAuthStateNotOverwritten(t *testing.T) {
	r, cm, rec := newTestReporter(t)
	cm.SetDeviceList([]config.DeviceCache{{DeviceID: "d1", IP: "10.0.0.1", State: 2}})

	r.Handle("d1", 1) // 离线上报打在认证态设备上
	if got := cm.GetDevice("d1").State; got != 2 {
		t.Fatalf("认证态不应被探活覆盖：state=%d", got)
	}
	if got := rec.probes(); got != 0 {
		t.Fatalf("认证态设备不应触发探测：probes=%d", got)
	}
}

// TestOnSdkExpiredImmediateRecovery 回归：过期钩子（版本查询成功+<208）
// 立即写在线——不等 30s 兜底（用户报障「查到 204 应立刻变」）。
func TestOnSdkExpiredImmediateRecovery(t *testing.T) {
	r, cm, _ := newTestReporter(t)
	cm.SetDeviceList([]config.DeviceCache{{DeviceID: "d1", IP: "10.0.0.1", State: 1}})

	r.OnSdkExpired("d1", "10.0.0.1")
	if got := cm.GetDevice("d1").State; got != 0 {
		t.Fatalf("过期钩子应立即写在线：state=%d", got)
	}
	// 幂等：State 已 0，再触发不产生变化（守卫跳过）
	r.OnSdkExpired("d1", "10.0.0.1")
	if got := cm.GetDevice("d1").State; got != 0 {
		t.Fatalf("重复触发应幂等：state=%d", got)
	}
}

// TestOnSdkExpiredSkipsAuthState 回归：认证态设备不碰过期钩子。
func TestOnSdkExpiredSkipsAuthState(t *testing.T) {
	r, cm, _ := newTestReporter(t)
	cm.SetDeviceList([]config.DeviceCache{{DeviceID: "d1", IP: "10.0.0.1", State: 2}})

	r.OnSdkExpired("d1", "10.0.0.1")
	if got := cm.GetDevice("d1").State; got != 2 {
		t.Fatalf("认证态不应被过期钩子覆盖：state=%d", got)
	}
}

func sleepMs(ms int) { time.Sleep(time.Duration(ms) * time.Millisecond) }
