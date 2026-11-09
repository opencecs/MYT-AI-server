package v4ws

import (
	"encoding/json"
	"sync"
	"testing"
	"time"
)

// --- 协议层纯逻辑单测（帧解析 / seq 规则 / 联合主键）---

func TestInstanceKey(t *testing.T) {
	if got := InstanceKey("coloros", "coloros-1"); got != "coloros/coloros-1" {
		t.Errorf("InstanceKey = %q", got)
	}
	// 不同后端同名 id 必须区分开（协议文档 §6）
	if InstanceKey("a", "x") == InstanceKey("b", "x") {
		t.Error("不同 backend 的同名 id 不应相同")
	}
	// backend 缺省退化裸 id
	if got := InstanceKey("", "x"); got != "x" {
		t.Errorf("空 backend 应退化为裸 id, got %q", got)
	}
	b, id := SplitInstanceKey("coloros/coloros-1")
	if b != "coloros" || id != "coloros-1" {
		t.Errorf("SplitInstanceKey = (%q,%q)", b, id)
	}
	b, id = SplitInstanceKey("bare")
	if b != "" || id != "bare" {
		t.Errorf("无分隔符应得 (\"\",\"bare\"), got (%q,%q)", b, id)
	}
}

func TestParseFrameHello(t *testing.T) {
	f, err := parseFrame([]byte(`{"type":"hello","proto":1,"sdkVersion":"0.7.0","serverTime":1792406110}`))
	if err != nil || f == nil || f.Kind != frameHello || f.Hello == nil {
		t.Fatalf("hello 解析失败: %v %+v", err, f)
	}
	if f.Hello.Proto != 1 || f.Hello.SDKVersion != "0.7.0" {
		t.Errorf("hello 字段不符: %+v", f.Hello)
	}
}

func TestParseFrameSnapshot(t *testing.T) {
	body := `{"type":"snapshot","seq":7,"data":{"instances":[
		{"backend":"coloros","id":"coloros-1","name":"coloros-1","variant":"v1","state":"running","running":true,
		 "pid":19,"adb_port":5556,"cast_port":12002,"control_port":28061,"cpu_cores":6,"memory_mb":8192,
		 "disk_mb":8192,"disk_used_mb":7806,"profile":{"latitude":31.2,"root":false}}],"total":1}}`
	f, err := parseFrame([]byte(body))
	if err != nil || f == nil || f.Kind != frameSnapshot {
		t.Fatalf("snapshot 解析失败: %v %+v", err, f)
	}
	if f.Snapshot.Seq != 7 || f.Snapshot.Data.Total != 1 || len(f.Snapshot.Data.Instances) != 1 {
		t.Fatalf("snapshot 字段不符: %+v", f.Snapshot)
	}
	it := f.Snapshot.Data.Instances[0]
	if it.Backend != "coloros" || it.ID != "coloros-1" || it.State != "running" || !it.Running {
		t.Errorf("实例字段不符: %+v", it)
	}
	if it.CastPort != 12002 || it.ControlPort != 28061 || it.MemoryMB != 8192 || it.DiskUsedMB != 7806 {
		t.Errorf("数值字段不符（snake_case tag 是否写错？）: %+v", it)
	}
	if it.Profile == nil || it.Profile.Latitude != 31.2 {
		t.Errorf("profile 未解析: %+v", it.Profile)
	}
}

func TestParseFrameEvent(t *testing.T) {
	body := `{"type":"event","seq":1191,"event":"instance","action":"state","timeNano":1792406034642214100,
		"data":{"backend":"cap1","id":"coloros-1","name":"coloros-1","running":true,"state":"running"}}`
	f, err := parseFrame([]byte(body))
	if err != nil || f == nil || f.Kind != frameEvent {
		t.Fatalf("event 解析失败: %v %+v", err, f)
	}
	if f.Event.Seq != 1191 || f.Event.Event != EventInstance || f.Event.Action != ActionState {
		t.Fatalf("event 字段不符: %+v", f.Event)
	}
	item, err := ParseInstanceStateData(f.Event.Data)
	if err != nil {
		t.Fatalf("instance data 解析失败: %v", err)
	}
	if item.Backend != "cap1" || item.State != "running" || !item.Running {
		t.Errorf("instance data 不符: %+v", item)
	}
}

func TestParseFrameResyncRequired(t *testing.T) {
	f, err := parseFrame([]byte(`{"type":"resync-required"}`))
	if err != nil || f == nil || f.Kind != frameResyncRequired {
		t.Fatalf("resync-required 解析失败: %v %+v", err, f)
	}
}

func TestParseFrameUnknownAndInvalid(t *testing.T) {
	// 未知类型：静默忽略（前向兼容）
	if f, err := parseFrame([]byte(`{"type":"future-thing"}`)); err != nil || f != nil {
		t.Errorf("未知帧应返回 (nil,nil), got (%+v,%v)", f, err)
	}
	// 非法 JSON：报错（消费循环丢弃）
	if _, err := parseFrame([]byte(`not json`)); err == nil {
		t.Error("非法 JSON 应返回错误")
	}
}

func TestParseClipboardData(t *testing.T) {
	d, err := ParseClipboardData(json.RawMessage(`{"backend":"default","id":"origin-1","text":"hello"}`))
	if err != nil || d.Text != "hello" || d.ID != "origin-1" {
		t.Fatalf("clipboard data 不符: %+v %v", d, err)
	}
}

// seq 规则（协议文档 §7.2）：<=last 丢弃 / ==last+1 应用 / >last+1 应用+断档
func TestSeqTracker(t *testing.T) {
	var tr seqTracker
	// 未收到 snapshot 前来了事件：应用并以此为基准
	apply, gap := tr.onEvent(5)
	if !apply || gap {
		t.Fatalf("无基准时应应用且不断档, got apply=%v gap=%v", apply, gap)
	}

	tr.onHello()
	tr.onSnapshot(10)
	cases := []struct {
		seq       int64
		wantApply bool
		wantGap   bool
		desc      string
	}{
		{10, false, false, "等于快照基准：窗口重放，丢弃"},
		{9, false, false, "小于基准：丢弃"},
		{11, true, false, "连续：应用"},
		{11, false, false, "重复：丢弃"},
		{13, true, true, "断档：应用并 resync"},
		{14, true, false, "断档后恢复连续"},
	}
	for _, c := range cases {
		apply, gap := tr.onEvent(c.seq)
		if apply != c.wantApply || gap != c.wantGap {
			t.Errorf("%s（seq=%d）: got apply=%v gap=%v, want %v/%v", c.desc, c.seq, apply, gap, c.wantApply, c.wantGap)
		}
	}
}

// hello 重置 seq 状态（新会话的 seq 与旧连接无关）
func TestSeqTrackerResetOnHello(t *testing.T) {
	var tr seqTracker
	tr.onSnapshot(100)
	tr.onHello()
	// 重置后首个事件（哪怕 seq 很小）应被应用，因为基准已清空
	apply, gap := tr.onEvent(1)
	if !apply || gap {
		t.Errorf("hello 重置后首帧应应用, got apply=%v gap=%v", apply, gap)
	}
}

// hasPort / normalizeHost（V4 主机地址归一）
func TestNormalizeHost(t *testing.T) {
	if got := normalizeHost("10.10.2.142"); got != "10.10.2.142:8088" {
		t.Errorf("normalizeHost 补端口失败: %q", got)
	}
	if got := normalizeHost("10.10.2.142:9999"); got != "10.10.2.142:9999" {
		t.Errorf("已有端口不应改动: %q", got)
	}
	if got := normalizeHost(""); got != "" {
		t.Errorf("空 host 应返回空: %q", got)
	}
	if got := stripPort("10.10.2.142:8088"); got != "10.10.2.142" {
		t.Errorf("stripPort = %q", got)
	}
}

// 并发安全 smoke：adapter/manager 的状态字段在 -race 下不报竞争（此处只测 seqTracker 的
// 串行契约——它明确非并发安全，由消费 goroutine 独占）
func TestSeqTrackerSingleOwner(t *testing.T) {
	var tr seqTracker
	tr.onSnapshot(0)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := int64(1); i <= 100; i++ {
			tr.onEvent(i)
		}
	}()
	wg.Wait()
	if tr.last != 100 {
		t.Errorf("last = %d, want 100", tr.last)
	}
	_ = time.Now
}

// --- 事件分发（clipboard / system/resync）---

// system/resync（后端增删）：发 resync 并回调 OnSystemResync
func TestClientSystemResync(t *testing.T) {
	m := newMockTobox(t)
	rec := &recorder{}
	c := fastConn(t, m.host(), rec)
	go c.run()
	defer c.Stop()

	mc := m.waitConn(t, 2*time.Second)
	waitFor(t, 2*time.Second, func() bool { return rec.snapshotCount() == 1 }, "snapshot 未到达")

	mc.send(t, `{"type":"event","seq":11,"event":"system","action":"resync","data":null}`)
	waitFor(t, 2*time.Second, func() bool { return len(mc.sentFrames()) == 1 }, "system/resync 未发 resync 请求")
	rec.mu.Lock()
	sys := rec.systems
	rec.mu.Unlock()
	if sys != 1 {
		t.Errorf("OnSystemResync 调用次数 = %d, want 1", sys)
	}
}

// clipboard/push 事件分发
func TestClientClipboardEvent(t *testing.T) {
	m := newMockTobox(t)
	rec := &recorder{}
	c := fastConn(t, m.host(), rec)
	go c.run()
	defer c.Stop()

	mc := m.waitConn(t, 2*time.Second)
	waitFor(t, 2*time.Second, func() bool { return rec.snapshotCount() == 1 }, "snapshot 未到达")

	mc.send(t, `{"type":"event","seq":11,"event":"clipboard","action":"push","data":{"backend":"default","id":"origin-1","text":"hello"}}`)
	waitFor(t, 2*time.Second, func() bool {
		rec.mu.Lock()
		defer rec.mu.Unlock()
		return rec.clipboards == 1
	}, "clipboard 事件未分发")
}
