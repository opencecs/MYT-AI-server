// devicedetail_events_test.go — hello/system-stats 事件落缓存的单测。
// 重点：SDKVersion 作为升级轮询的免探测数据源，必须 ①被 hello 写入、
// ②不被 system/stats 心跳洗掉（20s 全量事件只带动态指标）、③随设备升级刷新。
package devicedetail

import (
	"encoding/json"
	"testing"

	"github.com/moyunteng/myt/internal/config"
	"github.com/moyunteng/myt/internal/eventws"
)

func newTestService(t *testing.T) *Service {
	t.Helper()
	cm := config.NewManager(t.TempDir())
	if err := cm.InitDirs(); err != nil {
		t.Fatal(err)
	}
	return NewService(cm)
}

// helloFrame 帧样例与协议文档附录一致（字段与 HelloFrame 的 json tag 对齐）
func helloFrame(t *testing.T, raw string) *eventws.HelloFrame {
	t.Helper()
	var h eventws.HelloFrame
	if err := json.Unmarshal([]byte(raw), &h); err != nil {
		t.Fatal(err)
	}
	return &h
}

func TestHandleHelloStoresSDKVersion(t *testing.T) {
	s := newTestService(t)
	s.HandleHello("d1", helloFrame(t,
		`{"type":"hello","proto":1,"sdkVersion":"v208","deviceId":"rd1","model":"V2133A_v29"}`))

	d := s.configManager.GetDeviceDetail("d1")
	if d == nil || d.SDKVersion != 208 {
		t.Fatalf("hello 后 SDKVersion 应为 208, got %+v", d)
	}
}

func TestHandleHelloSDKOnlyFrame(t *testing.T) {
	// 只有 sdkVersion、无机型/固件版本（v206+ 身份字段可缺省）：
	// 守卫不能因 model/version 为空直接丢弃——SDK 版本是升级轮询数据源
	s := newTestService(t)
	s.HandleHello("d1", helloFrame(t, `{"type":"hello","proto":1,"sdkVersion":"v208"}`))

	d := s.configManager.GetDeviceDetail("d1")
	if d == nil || d.SDKVersion != 208 {
		t.Fatalf("仅 SDK 版本的 hello 也应落缓存, got %+v", d)
	}
}

func TestHandleSystemStatsPreservesSDKVersion(t *testing.T) {
	s := newTestService(t)
	s.HandleHello("d1", helloFrame(t,
		`{"type":"hello","proto":1,"sdkVersion":"v208","deviceId":"rd1","model":"V2133A_v29"}`))

	// system/stats 事件不带 sdkVersion（只带动态指标）——若保留清单漏了
	// SDKVersion，20s 心跳会把它洗回 0，升级轮询退化为逐台 HTTP 探测
	s.HandleSystemStats("d1", json.RawMessage(`{"cputemp":45,"cpuload":"3%","memuse":"1024"}`))

	d := s.configManager.GetDeviceDetail("d1")
	if d == nil || d.SDKVersion != 208 {
		t.Fatalf("system/stats 后 SDKVersion 应保留 208, got %+v", d)
	}
	if d.Model != "V2133A_v29" {
		t.Fatalf("model 应保留, got %+v", d)
	}
}

func TestHandleHelloRefreshesSDKVersion(t *testing.T) {
	// 设备升级（208→209）必经 WS 重连 hello：缓存必须跟随刷新，
	// 否则升级轮询看到旧版本会反复触发不必要的升级请求
	s := newTestService(t)
	s.HandleHello("d1", helloFrame(t, `{"type":"hello","proto":1,"sdkVersion":"v208"}`))
	s.HandleHello("d1", helloFrame(t, `{"type":"hello","proto":1,"sdkVersion":"v209"}`))

	d := s.configManager.GetDeviceDetail("d1")
	if d == nil || d.SDKVersion != 209 {
		t.Fatalf("hello 重复到达应刷新为 209, got %+v", d)
	}
}

// diskFrame 帧样例（新 SDK 磁盘帧，字节单位，与 panel 帧互斥）
const diskFrameRaw = `{"diskTotal":64424509440,"diskUsed":32212254720,"diskFree":32212254720}`

func TestHandleSystemStatsDiskFrameOnlyTouchesDisk(t *testing.T) {
	s := newTestService(t)
	// 预置：hello 建立静态字段 + 设备缓存 IP（disk 帧不带 ip，从详情缓存取）
	s.HandleHello("d1", helloFrame(t,
		`{"type":"hello","proto":1,"sdkVersion":"v208","deviceId":"rd1","model":"V2133A_v29"}`))
	s.configManager.UpsertDevice(config.DeviceCache{DeviceID: "d1", IP: "10.0.0.5"})
	s.configManager.SetDeviceDetail("d1", &config.DeviceDetail{
		DeviceID: "d1", IP: "10.0.0.5", Model: "V2133A_v29", SDKVersion: 208,
		MemTotal: "4096", MemUse: "1024", MMCTotal: 30000, MMCUse: 10000,
		CPUTemp: 45,
	})

	// onDeviceUpdated 不应被 disk 帧触发（弹窗已断喂）
	var deviceCalled bool
	s.SetOnDeviceUpdated(func(string, *config.DeviceDetail) { deviceCalled = true })

	s.HandleSystemStats("d1", json.RawMessage(diskFrameRaw))

	if deviceCalled {
		t.Fatal("disk 帧不应触发 onDeviceUpdated")
	}
	d := s.configManager.GetDeviceDetail("d1")
	if d == nil {
		t.Fatal("disk 帧不应清掉设备详情缓存")
	}
	// panel 字段不被 disk 帧洗掉
	if d.CPUTemp != 45 || d.MMCTotal != 30000 || d.MMCUse != 10000 {
		t.Fatalf("disk 帧不应改动 panel 字段: %+v", d)
	}
	// 磁盘三项按字节更新到设备列表
	dev := s.configManager.GetDevice("d1")
	if dev == nil || dev.DiskUsed != 32212254720 || dev.DiskTotal != 64424509440 {
		t.Fatalf("disk 帧应按字节更新设备磁盘: %+v", dev)
	}
}

func TestHandleSystemStatsPanelDiskGuard(t *testing.T) {
	s := newTestService(t)
	s.configManager.UpsertDevice(config.DeviceCache{DeviceID: "d1", IP: "10.0.0.5"})

	// 场景一：新 SDK（已见过 disk 帧）→ panel 帧的冻结磁盘值不覆盖 disk 帧的真实值
	s.HandleSystemStats("d1", json.RawMessage(diskFrameRaw)) // 置位 diskFrameSeen
	s.HandleSystemStats("d1", json.RawMessage(`{"mmctotal":30000,"mmcuse":10000,"memuse":"1024"}`))
	dev := s.configManager.GetDevice("d1")
	if dev == nil || dev.DiskUsed != 32212254720 || dev.DiskTotal != 64424509440 {
		t.Fatalf("已见过 disk 帧后 panel 帧不应覆盖磁盘: %+v", dev)
	}

	// 场景二：老 SDK（从未发过 disk 帧）→ panel 帧照旧更新磁盘（MB 换算字节）
	s2 := newTestService(t)
	s2.configManager.UpsertDevice(config.DeviceCache{DeviceID: "d2", IP: "10.0.0.6"})
	s2.HandleSystemStats("d2", json.RawMessage(`{"mmctotal":30000,"mmcuse":10000,"memuse":"1024"}`))
	dev2 := s2.configManager.GetDevice("d2")
	if dev2 == nil || dev2.DiskUsed != 10000*1048576 || dev2.DiskTotal != 30000*1048576 {
		t.Fatalf("老 SDK panel 帧应照旧更新磁盘: %+v", dev2)
	}
}

func TestParseSystemStatsFrameDiscrimination(t *testing.T) {
	// disk 帧：任一 disk* 键
	panel, disk, err := eventws.ParseSystemStatsFrame(json.RawMessage(diskFrameRaw))
	if err != nil || panel != nil || disk == nil || disk.DiskTotal != 64424509440 {
		t.Fatalf("disk 帧判别错误: panel=%v disk=%+v err=%v", panel, disk, err)
	}
	// panel 帧：含 panel 键
	panel, disk, err = eventws.ParseSystemStatsFrame(json.RawMessage(`{"cputemp":45,"cpuload":"3%"}`))
	if err != nil || disk != nil || panel == nil {
		t.Fatalf("panel 帧判别错误: panel=%v disk=%+v err=%v", panel, disk, err)
	}
	// 未知帧（两者都不含）：按 panel 旧路径透传（前向兼容）
	panel, disk, err = eventws.ParseSystemStatsFrame(json.RawMessage(`{"futureField":1}`))
	if err != nil || disk != nil || panel == nil {
		t.Fatalf("未知帧应按 panel 透传: panel=%v disk=%+v err=%v", panel, disk, err)
	}
	// 非法 JSON
	_, _, err = eventws.ParseSystemStatsFrame(json.RawMessage(`{bad`))
	if err == nil {
		t.Fatal("非法 JSON 应报错")
	}
}
