package main

import (
	"testing"

	"github.com/moyunteng/myt/internal/config"
)

// newAppSvcForTest 只注入 config 管理器与切换回调——
// SetDeployMode 其余依赖（screenshotSvc/sdkApiSvc/discoverSvc/app）均有 nil 守卫。
// 返回 eventws 对账调用计数、对账时刻的容器列表长度（验证对账先于清空）、
// 切回 local 的数据刷新调用计数。
func newAppSvcForTest(t *testing.T) (*AppService, *int, *int, *int) {
	t.Helper()
	cm := config.NewManager(t.TempDir())
	if err := cm.InitDirs(); err != nil {
		t.Fatalf("初始化配置目录失败: %v", err)
	}
	svc := NewAppService(cm)
	resyncCalls := 0
	androidLenAtResync := -1
	svc.SetEventwsResync(func() {
		resyncCalls++
		androidLenAtResync = len(cm.GetAndroidList())
	})
	localRefreshCalls := 0
	svc.SetOnSwitchToLocal(func() { localRefreshCalls++ })
	return svc, &resyncCalls, &androidLenAtResync, &localRefreshCalls
}

// TestSetDeployModeResyncsEventwsBeforePrune 模式切换必须触发 eventws 对账，
// 且对账先于容器缓存裁剪执行（停连接后残余帧毫秒级排空，若裁剪在前，
// 残余 snapshot 事件会把旧模式设备的容器回灌进当前模式的数据桶）。
// 容器列表已按模式隔离，切换不再整表清空：只裁剪「设备不在当前模式列表」的条目。
func TestSetDeployModeResyncsEventwsBeforePrune(t *testing.T) {
	svc, calls, androidLen, _ := newAppSvcForTest(t)

	// 预置 public 桶：一台在列表内的设备 dev1（容器 c1）+ 一台不在列表的 dev-other（容器 cX，
	// 模拟旧模式连接残余事件写进来的跨模式污染）
	if err := svc.cm.SetDeployMode("public"); err != nil {
		t.Fatalf("预置 public 模式失败: %v", err)
	}
	svc.cm.SetDeviceList([]config.DeviceCache{{DeviceID: "dev1", State: 1}})
	svc.cm.SetAndroidList([]config.AndroidInfo{
		{ID: "c1", DeviceID: "dev1", Status: "running"},
		{ID: "cX", DeviceID: "dev-other", Status: "running"},
	})
	if err := svc.cm.SetDeployMode("local"); err != nil {
		t.Fatalf("预置 local 模式失败: %v", err)
	}

	if err := svc.SetDeployMode("public"); err != nil {
		t.Fatalf("切换 public 失败: %v", err)
	}

	if *calls != 1 {
		t.Fatalf("切 public 应触发一次 eventws 对账，实际 %d 次", *calls)
	}
	if *androidLen != 2 {
		t.Fatalf("对账执行时容器缓存已被裁剪（len=%d）——对账必须先于裁剪，否则残余 WS 事件会回灌污染", *androidLen)
	}
	// 当前模式设备列表内的容器保留（切回来还是原来的数据），跨模式残留被裁剪
	got := svc.cm.GetAndroidList()
	if len(got) != 1 || got[0].ID != "c1" {
		t.Fatalf("应只保留属于当前模式设备的容器 c1，实际 %v", got)
	}
}

// TestSetDeployModeResyncBackToLocal 切回 local 同样触发对账
// （按 local 缓存列表重建连接——与 discover 完成后的对账幂等）。
func TestSetDeployModeResyncBackToLocal(t *testing.T) {
	svc, calls, _, localRefresh := newAppSvcForTest(t)
	if err := svc.SetDeployMode("public"); err != nil {
		t.Fatalf("切换 public 失败: %v", err)
	}
	if err := svc.SetDeployMode("local"); err != nil {
		t.Fatalf("切换 local 失败: %v", err)
	}
	if *calls != 2 {
		t.Fatalf("两次切换应各触发一次对账，实际 %d 次", *calls)
	}
	if *localRefresh != 1 {
		t.Fatalf("切回 local 应触发一次数据刷新，实际 %d 次", *localRefresh)
	}
}

// TestSetDeployModeLocalRefreshNotFiredOnPublic 切到 public 不触发本地数据刷新
// （public 方向由 discoverFromCloud 阶段1 的 TriggerNow 网关查询覆盖）。
func TestSetDeployModeLocalRefreshNotFiredOnPublic(t *testing.T) {
	svc, _, _, localRefresh := newAppSvcForTest(t)
	if err := svc.SetDeployMode("local"); err != nil {
		t.Fatalf("切换 local 失败: %v", err)
	}
	if err := svc.SetDeployMode("public"); err != nil {
		t.Fatalf("切换 public 失败: %v", err)
	}
	if *localRefresh != 1 {
		t.Fatalf("仅切 local 应触发数据刷新（切 public 不触发），实际 %d 次", *localRefresh)
	}
}

// TestSelectedDevicesWithIP 切回 local 的补拉范围 = 持久化勾选（order>0）且有 IP 的设备：
// order 是前后端共同的勾选持久化字段；IP 为空的 public 残留条目 REST 直连不可达，必须排除。
func TestSelectedDevicesWithIP(t *testing.T) {
	list := []config.DeviceCache{
		{DeviceID: "sel-1", IP: "10.10.0.1", Order: 1},
		{DeviceID: "unselected", IP: "10.10.0.2", Order: 0},
		{DeviceID: "sel-2", IP: "10.10.0.3", Order: 5},
		{DeviceID: "pub-leftover", IP: "", Order: 3}, // public 残留：勾选但无 IP
	}
	got := selectedDevicesWithIP(list)
	if len(got) != 2 {
		t.Fatalf("应选出 2 台（order>0 且有 IP），实际 %d 台", len(got))
	}
	if got[0].DeviceID != "sel-1" || got[1].DeviceID != "sel-2" {
		t.Fatalf("选出设备不符: %s, %s", got[0].DeviceID, got[1].DeviceID)
	}
	if selectedDevicesWithIP(nil) != nil {
		t.Fatalf("空列表应返回 nil")
	}
}

// TestSetDeployModeWithoutResyncCallback 未注入回调（组装缺环/测试环境）不得 panic。
func TestSetDeployModeWithoutResyncCallback(t *testing.T) {
	cm := config.NewManager(t.TempDir())
	if err := cm.InitDirs(); err != nil {
		t.Fatalf("初始化配置目录失败: %v", err)
	}
	svc := NewAppService(cm)
	if err := svc.SetDeployMode("public"); err != nil {
		t.Fatalf("切换 public 失败: %v", err)
	}
	if svc.GetDeployMode() != "public" {
		t.Fatalf("模式未生效: %s", svc.GetDeployMode())
	}
}
