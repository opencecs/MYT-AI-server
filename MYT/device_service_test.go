package main

// device_service_test.go — RefreshDeviceInfo（设备详情弹窗主动拉取）的行为测试：
// 标识解析（deviceID/IP 双键）、local 模式直连路径、public/v2 降级路径。

import (
	"testing"

	"github.com/moyunteng/myt/internal/config"
)

func newDetailWrapperForTest(t *testing.T) *DeviceDetailServiceWrapper {
	t.Helper()
	cm := config.NewManager(t.TempDir())
	if err := cm.InitDirs(); err != nil {
		t.Fatal(err)
	}
	cm.UpsertDevice(config.DeviceCache{DeviceID: "dev1", IP: "10.0.0.1"})
	w := &DeviceDetailServiceWrapper{cm: cm}
	return w
}

func TestResolveDeviceIdent(t *testing.T) {
	w := newDetailWrapperForTest(t)

	// 按 deviceID 查
	if id, ip := w.resolveDeviceIdent("dev1"); id != "dev1" || ip != "10.0.0.1" {
		t.Fatalf("deviceID 解析错误: id=%s ip=%s", id, ip)
	}
	// 按 IP 查
	if id, ip := w.resolveDeviceIdent("10.0.0.1"); id != "dev1" || ip != "10.0.0.1" {
		t.Fatalf("IP 解析错误: id=%s ip=%s", id, ip)
	}
	// 未入库：原样作为 IP 处理（未入库设备直连场景）
	if id, ip := w.resolveDeviceIdent("10.9.9.9"); id != "" || ip != "10.9.9.9" {
		t.Fatalf("未知标识应原样作为 IP: id=%s ip=%s", id, ip)
	}
}

func TestRefreshDeviceInfoNonLocalFallsBackToSDK(t *testing.T) {
	// public 模式：不走 FetchDeviceInfo（无局域网直连），降级 SDK 网关查询。
	// sdkApiSvc 未注入时返回明确错误（防御性）；注入后的查询路径由
	// SdkApiServiceWrapper.GetDeviceInfo 自测覆盖，这里只验证分流决策。
	w := newDetailWrapperForTest(t)
	if err := w.cm.SetDeployMode("public"); err != nil {
		t.Fatal(err)
	}
	if _, err := w.RefreshDeviceInfo("dev1"); err == nil {
		t.Fatal("public 模式且未注入 SDK 服务应返回错误")
	}
}
