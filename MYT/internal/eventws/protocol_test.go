package eventws

import (
	"encoding/json"
	"testing"
)

// 全部帧样例取自协议文档附录「真实帧序列样例」（真机抓取），可直接做测试 fixture。

func mustParse(t *testing.T, data string) *Frame {
	t.Helper()
	f, err := parseFrame([]byte(data))
	if err != nil {
		t.Fatalf("parseFrame(%s) 错误: %v", data, err)
	}
	return f
}

func TestParseHello(t *testing.T) {
	f := mustParse(t, `{"type":"hello","proto":1,"sdkVersion":"v206","serverTime":1788339142,"deviceId":"r99d3fd5e79c313c18fc4a8f5f96847e","deviceVersion":"1.9.5","model":"V2133A_v29"}`)
	if f.Kind != "hello" || f.Hello == nil {
		t.Fatalf("期望 hello 帧，得到 kind=%s", f.Kind)
	}
	if f.Hello.Proto != 1 || f.Hello.SDKVersion != "v206" || f.Hello.ServerTime != 1788339142 {
		t.Errorf("hello 基础字段解析错误: %+v", f.Hello)
	}
	if f.Hello.DeviceID != "r99d3fd5e79c313c18fc4a8f5f96847e" || f.Hello.DeviceVersion != "1.9.5" || f.Hello.Model != "V2133A_v29" {
		t.Errorf("hello 身份字段解析错误: %+v", f.Hello)
	}
}

func TestParseHelloTags(t *testing.T) {
	// v208+ hello 帧 tags 字段：本机标签名扁平全集（跨容器去重、字典序，同 GET /android/tags）
	f := mustParse(t, `{"type":"hello","proto":1,"sdkVersion":"v208","serverTime":1788339142,
		"deviceId":"r99d3fd5e79c313c18fc4a8f5f96847e","tags":["生产","压测","灰度"]}`)
	if f.Hello == nil {
		t.Fatalf("期望 hello 帧")
	}
	if len(f.Hello.Tags) != 3 || f.Hello.Tags[0] != "生产" || f.Hello.Tags[2] != "灰度" {
		t.Errorf("hello tags 解析错误: %+v", f.Hello.Tags)
	}
	// 老版本无 tags 字段：nil，容忍缺失
	old := mustParse(t, `{"type":"hello","proto":1,"sdkVersion":"v206","serverTime":100}`)
	if old.Hello == nil || old.Hello.Tags != nil {
		t.Errorf("无 tags 字段的 hello 应解析为 nil: %+v", old.Hello.Tags)
	}
	// 空全集（设备端序列化为 [] 而非 null）
	empty := mustParse(t, `{"type":"hello","proto":1,"sdkVersion":"v208","tags":[]}`)
	if empty.Hello == nil || empty.Hello.Tags == nil || len(empty.Hello.Tags) != 0 {
		t.Errorf("空 tags 全集应为非 nil 空数组: %+v", empty.Hello.Tags)
	}
}

func TestParseHelloIdentityOptional(t *testing.T) {
	// 老版本 v206 基础帧：无身份字段，必须容忍缺失
	f := mustParse(t, `{"type":"hello","proto":1,"sdkVersion":"v206","serverTime":1788339142}`)
	if f.Hello == nil || f.Hello.DeviceID != "" || f.Hello.Model != "" {
		t.Errorf("hello 身份字段应可缺省: %+v", f.Hello)
	}
}

// TestHelloSDKVersionNum hello 的 SDK 版本数值解析（"v206"→206）：
// 设备重启必经 WS 重连 hello，该值是前端版本缓存的实时失效信号。
func TestHelloSDKVersionNum(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want int
	}{
		{"标准 v 前缀", `{"type":"hello","proto":1,"sdkVersion":"v206"}`, 206},
		{"无前缀裸数字", `{"type":"hello","proto":1,"sdkVersion":"208"}`, 208},
		{"缺省版本", `{"type":"hello","proto":1}`, 0},
		{"非法格式", `{"type":"hello","proto":1,"sdkVersion":"beta"}`, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := mustParse(t, tt.raw)
			if got := f.Hello.SDKVersionNum(); got != tt.want {
				t.Errorf("SDKVersionNum() = %d, want %d", got, tt.want)
			}
		})
	}
	// nil 容忍（hello 缺省字段时 HandleHello 早退前可能传 nil）
	var nilFrame *HelloFrame
	if got := nilFrame.SDKVersionNum(); got != 0 {
		t.Errorf("nil 帧应返回 0，得到 %d", got)
	}
}

func TestParseSnapshot(t *testing.T) {
	f := mustParse(t, `{"type":"snapshot","seq":123,"data":{"count":2,"list":[
		{"id":"9fb2dfba","name":"wr_hot_test","status":"running","indexNum":1,"cpuUsage":12.5,"memUsage":2147483648,"diskUsage":17179869184,"tags":["生产","压测"]},
		{"id":"a1b2c3d4","name":"c2","status":"exited","tags":[]}
	]}}`)
	if f.Kind != "snapshot" || f.Snapshot == nil {
		t.Fatalf("期望 snapshot 帧")
	}
	if f.Snapshot.Seq != 123 || f.Snapshot.Data.Count != 2 || len(f.Snapshot.Data.List) != 2 {
		t.Errorf("snapshot 字段解析错误: seq=%d count=%d len=%d", f.Snapshot.Seq, f.Snapshot.Data.Count, len(f.Snapshot.Data.List))
	}
	item := f.Snapshot.Data.List[0]
	if item.ID != "9fb2dfba" || item.Status != "running" || item.CPUUsage != 12.5 || item.MemUsage != 2147483648 || item.DiskUsage != 17179869184 {
		t.Errorf("snapshot 列表项解析错误: %+v", item)
	}
	// tags 透传：非空数组原样解析、空数组保持空（非 nil）
	if len(item.Tags) != 2 || item.Tags[0] != "生产" || item.Tags[1] != "压测" {
		t.Errorf("snapshot tags 解析错误: %+v", item.Tags)
	}
	if f.Snapshot.Data.List[1].CPUUsage != 0 {
		t.Errorf("exited 容器 stats 应为零值")
	}
	if tags := f.Snapshot.Data.List[1].Tags; tags == nil || len(tags) != 0 {
		t.Errorf("空 tags 应为非 nil 空数组（设备端双重兜底）: %v", tags)
	}
}

func TestParseEventContainer(t *testing.T) {
	f := mustParse(t, `{"type":"event","seq":124,"event":"container","action":"stop","timeNano":1788338560645000000,"data":{"containerId":"9fb2dfba","name":"wr_hot_test","status":"stopped"}}`)
	if f.Kind != "event" || f.Event == nil {
		t.Fatalf("期望 event 帧")
	}
	if f.Event.Seq != 124 || f.Event.Event != "container" || f.Event.Action != "stop" {
		t.Errorf("event 基础字段解析错误: %+v", f.Event)
	}
	d, err := ParseContainerEventData(f.Event.Data)
	if err != nil {
		t.Fatalf("container data 解析失败: %v", err)
	}
	if d.ContainerID != "9fb2dfba" || d.Name != "wr_hot_test" || d.Status != "stopped" {
		t.Errorf("container data 解析错误: %+v", d)
	}
}

func TestParseEventContainerStats(t *testing.T) {
	// stats 事件的 data 是数组（lifecycle 是单对象）——形态区分是关键
	f := mustParse(t, `{"type":"event","seq":2182,"event":"container","action":"stats","timeNano":1788338570645000000,"data":[
		{"containerId":"9fb2dfba","name":"wr_hot_test","status":"running","cpuUsage":12.5,"memUsage":2147483648,"diskUsage":17179869184,"tags":["生产"]},
		{"containerId":"a1b2c3d4","name":"c2","status":"exited"}
	]}`)
	stats, err := ParseContainerStatsData(f.Event.Data)
	if err != nil {
		t.Fatalf("stats data 解析失败: %v", err)
	}
	if len(stats) != 2 || stats[0].CPUUsage != 12.5 || stats[0].MemUsage != 2147483648 || stats[1].CPUUsage != 0 {
		t.Errorf("stats 数组解析错误: %+v", stats)
	}
	// tags：带的解析原样；未带的保持 nil（合并层据此区分「本帧未带」与「无标签」）
	if len(stats[0].Tags) != 1 || stats[0].Tags[0] != "生产" {
		t.Errorf("stats tags 解析错误: %+v", stats[0].Tags)
	}
	if stats[1].Tags != nil {
		t.Errorf("未带 tags 字段的帧应解析为 nil: %v", stats[1].Tags)
	}
}

func TestParseTagsChangedEvent(t *testing.T) {
	// system/tagsChanged：data = 变更后的本机标签名扁平全集（{"tags":[...]}）
	f := mustParse(t, `{"type":"event","seq":2401,"event":"system","action":"tagsChanged",
		"timeNano":1788339140000000000,"data":{"tags":["生产","压测"]}}`)
	if f.Event == nil || f.Event.Event != "system" || f.Event.Action != "tagsChanged" {
		t.Fatalf("期望 system/tagsChanged 事件帧: %+v", f.Event)
	}
	tags, err := ParseTagsChangedData(f.Event.Data)
	if err != nil {
		t.Fatalf("tagsChanged data 解析失败: %v", err)
	}
	if len(tags) != 2 || tags[0] != "生产" || tags[1] != "压测" {
		t.Errorf("tagsChanged 全集解析错误: %+v", tags)
	}
	// 标签被清光：空数组（非 nil）
	empty, err := ParseTagsChangedData(json.RawMessage(`{"tags":[]}`))
	if err != nil || empty == nil || len(empty) != 0 {
		t.Errorf("空全集应解析为非 nil 空数组: %+v err=%v", empty, err)
	}
}

func TestParseEventBoot(t *testing.T) {
	f := mustParse(t, `{"type":"event","seq":131,"event":"boot","action":"queued","timeNano":1788338560000000000,"data":{"containerId":"9fb2dfba"}}`)
	d, err := ParseBootEventData(f.Event.Data)
	if err != nil {
		t.Fatalf("boot data 解析失败: %v", err)
	}
	if d.ContainerID != "9fb2dfba" {
		t.Errorf("boot data 解析错误: %+v", d)
	}
}

func TestParseResyncRequired(t *testing.T) {
	f := mustParse(t, `{"type":"resync-required"}`)
	if f.Kind != "resync-required" {
		t.Fatalf("期望 resync-required 帧，得到 %s", f.Kind)
	}
}

func TestParseUnknownTypeIgnored(t *testing.T) {
	// 前向兼容：未知帧类型静默忽略
	f, err := parseFrame([]byte(`{"type":"future-frame","whatever":1}`))
	if err != nil {
		t.Fatalf("未知帧不应报错: %v", err)
	}
	if f != nil {
		t.Fatalf("未知帧应返回 nil，得到 %+v", f)
	}
}

func TestSeqRules(t *testing.T) {
	var tr seqTracker

	// hello 重置
	tr.onHello()
	tr.onSnapshot(126)
	apply, gap := tr.onEvent(127)
	if !apply || gap {
		t.Errorf("seq=127 (last=126) 应 (apply=true, gap=false)，得到 (%v,%v)", apply, gap)
	}
	// 重复丢弃
	apply, gap = tr.onEvent(127)
	if apply || gap {
		t.Errorf("重复 seq=127 应丢弃，得到 (%v,%v)", apply, gap)
	}
	// 回退丢弃
	apply, gap = tr.onEvent(125)
	if apply || gap {
		t.Errorf("回退 seq=125 应丢弃，得到 (%v,%v)", apply, gap)
	}
	// 断档：应用 + gap
	apply, gap = tr.onEvent(200)
	if !apply || !gap {
		t.Errorf("跳变 seq=200 应 (apply=true, gap=true)，得到 (%v,%v)", apply, gap)
	}
	// 断档后继续连续
	apply, gap = tr.onEvent(201)
	if !apply || gap {
		t.Errorf("seq=201 (last=200) 应 (apply=true, gap=false)，得到 (%v,%v)", apply, gap)
	}
	// hello 重置后再来旧 seq 不丢弃（新序列）
	tr.onHello()
	apply, gap = tr.onEvent(5)
	if !apply {
		t.Errorf("hello 重置后 seq=5 应应用")
	}
}

func TestSeqEventBeforeSnapshot(t *testing.T) {
	// 异常时序：未收到 snapshot 直接来事件——应用并以此为基准
	var tr seqTracker
	tr.onHello()
	apply, gap := tr.onEvent(100)
	if !apply || gap {
		t.Errorf("无基准时 event 应 (apply=true, gap=false)，得到 (%v,%v)", apply, gap)
	}
	apply, gap = tr.onEvent(101)
	if !apply || gap {
		t.Errorf("基准建立后连续 event 应应用")
	}
}

func TestBootActionMapping(t *testing.T) {
	cases := map[string]string{
		"queued": "starting", "booting": "starting",
		"completed": "running", "timeout": "running", "ready": "running",
		"aborted": "stopped",
		"unknown-action": "",
	}
	for action, want := range cases {
		if got := BootActionToStatus(action); got != want {
			t.Errorf("BootActionToStatus(%q) = %q, 期望 %q", action, got, want)
		}
	}
}

func TestContainerActionMapping(t *testing.T) {
	cases := map[string]string{
		"create": "created", "start": "running",
		"restart": "restarting",
		"pause":   "starting", "unpause": "starting", // 排队语义，不离开运行侧
		"stop": "stopped", "die": "stopped", "kill": "stopped", "destroy": "removed",
		"oom": "running", // 状态不变
		"stats":    "", // 数值路径专用，不改状态
		"unknown":  "", // 前向兼容
	}
	for action, want := range cases {
		if got := ContainerActionToStatus(action); got != want {
			t.Errorf("ContainerActionToStatus(%q) = %q, 期望 %q", action, got, want)
		}
	}
}

// 确认 raw json 保持原始字节（Data 字段二次解析的前提）
func TestEventDataRawPreserved(t *testing.T) {
	raw := json.RawMessage(`{"containerId":"x"}`)
	var ef EventFrame
	if err := json.Unmarshal([]byte(`{"type":"event","seq":1,"event":"container","action":"start","data":{"containerId":"x"}}`), &ef); err != nil {
		t.Fatal(err)
	}
	if string(ef.Data) != string(raw) {
		t.Errorf("Data 原始字节未保留: %s", ef.Data)
	}
}
