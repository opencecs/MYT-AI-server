package eventws

// 在线性上报回归测试：零值吞上报 bug、config 对账自愈、密码 getter 纯 IP。

import (
	"strings"
	"sync"
	"testing"
	"time"
)

// reportRecord 记录上报序列
type reportRecord struct {
	mu     sync.Mutex
	reports []struct {
		id    string
		state int
	}
}

func (r *reportRecord) add(id string, state int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reports = append(r.reports, struct {
		id    string
		state int
	}{id, state})
}

func (r *reportRecord) countOf(id string, state int) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, x := range r.reports {
		if x.id == id && x.state == state {
			n++
		}
	}
	return n
}

// TestReportFirstOnlineNotSwallowed 回归：lastState 零值吞首次在线上报。
// 旧实现 s.lastState[deviceID] != state 对缺失 key 返回零值 0，恰与
// stateOnline(0) 相等——任何设备先报过一次后（创建 map），后续每台设备的
// 首次「在线」上报都被静默吞掉。多设备场景必现（单设备测试发现不了）。
func TestReportFirstOnlineNotSwallowed(t *testing.T) {
	svc := NewService()
	defer svc.Stop()
	rec := &reportRecord{}
	svc.SetStateReporter(rec.add)

	// 设备 A 先报（拨号失败类，创建 lastState map）
	svc.reportDeviceState("first", stateOffline)
	// 设备 B 首次报在线——旧实现在此被吞
	svc.reportDeviceState("second", stateOnline)

	if got := rec.countOf("second", stateOnline); got != 1 {
		t.Fatalf("新设备首次在线上报被吞：期望 1 次，实际 %d", got)
	}
	// 同状态重复上报：去重不重复
	svc.reportDeviceState("second", stateOnline)
	if got := rec.countOf("second", stateOnline); got != 1 {
		t.Fatalf("同状态重复上报未去重：%d 次", got)
	}
}

// TestReportConfigReconcile 心跳对账：config 被外部写歪后，同状态重报自愈。
func TestReportConfigReconcile(t *testing.T) {
	svc := NewService()
	defer svc.Stop()
	rec := &reportRecord{}
	svc.SetStateReporter(rec.add)

	configState := stateOnline // 模拟 config 实际状态（可被外部改写）
	svc.SetStateQueryer(func(id string) int { return configState })

	svc.reportDeviceState("dev", stateOnline) // 首次：上报
	if got := rec.countOf("dev", stateOnline); got != 1 {
		t.Fatalf("首次上报丢失：%d 次", got)
	}

	// 外部把 config 写歪为离线 → 心跳帧再报同状态必须穿透去重（自愈）
	configState = stateOffline
	svc.reportDeviceState("dev", stateOnline)
	if got := rec.countOf("dev", stateOnline); got != 2 {
		t.Fatalf("config 写歪后心跳未重报（期望 2 次，实际 %d）", got)
	}

	// config 恢复一致后：不再重复上报
	configState = stateOnline
	svc.reportDeviceState("dev", stateOnline)
	if got := rec.countOf("dev", stateOnline); got != 2 {
		t.Fatalf("config 一致后仍在重复上报：%d 次", got)
	}
}

// TestReportDedupForceInterval 回归：降级死锁自愈。prev/state/config 三者一致的
// 重复上报（404 持续报离线 + config 已被复核失败写成离线）超过 dedupForceInterval
// 后必须强制转发——否则 setup 的 HTTP 复核失去触发点，设备永远显示离线
// （设备 SDK 降级重启期间复核失败写 State=1，起来后 WS 404 全被去重吞掉）。
func TestReportDedupForceInterval(t *testing.T) {
	svc := NewService()
	defer svc.Stop()
	rec := &reportRecord{}
	svc.SetStateReporter(rec.add)
	// config 也报 1（模拟复核失败已写离线）——三者一致
	svc.SetStateQueryer(func(id string) int { return stateOffline })

	svc.reportDeviceState("dev", stateOffline) // 首次：转发
	if got := rec.countOf("dev", stateOffline); got != 1 {
		t.Fatalf("首次上报丢失：%d", got)
	}
	svc.reportDeviceState("dev", stateOffline) // 5 分钟内重复：去重
	if got := rec.countOf("dev", stateOffline); got != 1 {
		t.Fatalf("近期重复未去重：%d", got)
	}

	// 时间兜底到期后的重复上报：强制转发（复核自愈入口）
	svc.mu.Lock()
	svc.lastForward["dev"] = time.Now().Add(-dedupForceInterval - time.Minute)
	svc.mu.Unlock()
	svc.reportDeviceState("dev", stateOffline)
	if got := rec.countOf("dev", stateOffline); got != 2 {
		t.Fatalf("超时重复上报未强制转发（死锁未自愈）：%d", got)
	}

	// Sync（用户刷新）清除时间兜底：下次上报立即转发
	svc.mu.Lock()
	svc.lastForward["dev"] = time.Now().Add(-dedupForceInterval - time.Minute)
	svc.mu.Unlock()
	svc.Sync(nil)
	svc.reportDeviceState("dev", stateOffline)
	if got := rec.countOf("dev", stateOffline); got != 3 {
		t.Fatalf("Sync 后上报未解锁转发：%d", got)
	}
}

// TestReportOnlineRepeatNotForced 回归：时间兜底仅限离线态——在线心跳帧的
// 重复上报即使超过 dedupForceInterval 也必须保持去重。旧实现全状态兜底时，
// 300 台在线设备每 5 分钟各触发一次完整上线级联（全量 devicelist:updated +
// QueryDevice + TriggerNow）≈ 每秒 1 条全量广播，正是事件铁律禁止的周期数据
// 混入列表事件。
func TestReportOnlineRepeatNotForced(t *testing.T) {
	svc := NewService()
	defer svc.Stop()
	rec := &reportRecord{}
	svc.SetStateReporter(rec.add)
	svc.SetStateQueryer(func(id string) int { return stateOnline })

	svc.reportDeviceState("dev", stateOnline) // 首次：转发
	if got := rec.countOf("dev", stateOnline); got != 1 {
		t.Fatalf("首次上报丢失：%d", got)
	}
	// 超过兜底间隔的在线重复上报：仍去重（无级联重放）
	svc.mu.Lock()
	svc.lastForward["dev"] = time.Now().Add(-dedupForceInterval - time.Minute)
	svc.mu.Unlock()
	svc.reportDeviceState("dev", stateOnline)
	if got := rec.countOf("dev", stateOnline); got != 1 {
		t.Fatalf("在线重复上报不应强制转发（级联重放回归）：%d", got)
	}
}

// TestDialOptionsPasswordGetsBareIP 回归：密码 getter 必须收到纯 IP。
// 旧实现传拨号 host（ip:port），GetDevicePassword 按纯 IP 查 ipToDeviceID
// 永远 miss → 不带 Authorization → 配密码设备全部 401。
func TestDialOptionsPasswordGetsBareIP(t *testing.T) {
	var gotParam string
	c := newConn("dev1", "192.0.2.202:8000", func(ip string) string {
		gotParam = ip
		return "secret"
	}, nil)

	opts := c.dialOptions()
	if gotParam != "192.0.2.202" {
		t.Fatalf("密码 getter 应收到纯 IP，实际 %q", gotParam)
	}
	auth := opts.HTTPHeader.Get("Authorization")
	if !strings.HasPrefix(auth, "Basic ") {
		t.Fatalf("应携带 Authorization 头：%q", auth)
	}
	if opts.HTTPHeader.Get("deviceId") != "dev1" {
		t.Fatalf("deviceId 头缺失")
	}
}

// TestOnlineHealsAfterExternalOverwrite 全链路：连接健康时 config 被写歪为离线，
// 下一个心跳帧（任意事件帧）把在线状态纠正回来。
func TestOnlineHealsAfterExternalOverwrite(t *testing.T) {
	m := newMockSDK(t)
	h := &recordingHandlers{}

	svc := NewService()
	svc.dialPortOverride = m.host()[strings.LastIndex(m.host(), ":")+1:]
	svc.SetHandlers(h)
	defer svc.Stop()

	rec := &reportRecord{}
	svc.SetStateReporter(rec.add)
	configState := stateOnline
	svc.SetStateQueryer(func(id string) int { return configState })

	svc.Connect("dev1", "127.0.0.1")
	waitFor(t, 5*time.Second, func() bool { return rec.countOf("dev1", stateOnline) >= 1 })

	// 模拟外部把 config 状态写歪为离线，广播一帧 system/stats（心跳）
	configState = stateOffline
	m.broadcast(`{"type":"event","seq":11,"event":"system","action":"stats","data":{"cputemp":40}}`)
	waitFor(t, 5*time.Second, func() bool { return rec.countOf("dev1", stateOnline) >= 2 })
}

// TestHeartbeatReportOnAnyFrame 任意帧（非 snapshot）也上报在线——
// 连接活着但只来事件流（无 resync/snapshot）时在线性依然成立。
func TestHeartbeatReportOnAnyFrame(t *testing.T) {
	m := newMockSDK(t)
	h := &recordingHandlers{}

	c := newConn("dev1", m.host(), nil, h)
	states := make(chan int, 16)
	c.onState = func(state int) { states <- state }
	go c.run()
	defer c.Stop()

	waitFor(t, 5*time.Second, func() bool { return len(states) > 0 })
	// 首帧（hello）即应上报在线
	select {
	case st := <-states:
		if st != stateOnline {
			t.Fatalf("首帧应上报在线，实际 %d", st)
		}
	default:
		t.Fatalf("无状态上报")
	}
}
