package config

import (
	"encoding/json"
	"os"
	"testing"
)

// 落档防呆（flushDirty）：内存设备列表为空时不覆盖磁盘已有内容，
// 但用户显式删空必须放行，待添加列表为空不受影响。
// 背景：网关异常返回空集合时，SetDeviceList([]) 会让 5s 后的落档把
// 用户的设备列表写成 []，重启即永久丢失。

// readDeviceListFile 读取指定路径的设备列表文件
func readDeviceListFile(t *testing.T, path string) []DeviceCache {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取 %s 失败: %v", path, err)
	}
	var list []DeviceCache
	if err := json.Unmarshal(data, &list); err != nil {
		t.Fatalf("解析 %s 失败: %v", path, err)
	}
	return list
}

// 内存被写成空列表（网关异常）时，磁盘已有设备必须保持不变
func TestFlushDirtyGuardKeepsDiskWhenMemoryEmpty(t *testing.T) {
	m := tempManager(t)
	path := m.deviceListLocalPath()

	// 先落档一台真实设备
	m.SetDeviceList([]DeviceCache{{DeviceID: "dev1", IP: "10.10.0.1"}})
	m.flushDirty()
	if got := readDeviceListFile(t, path); len(got) != 1 {
		t.Fatalf("首次落档后磁盘应为 1 台，实际 %d 台", len(got))
	}

	// 模拟网关异常：内存被清成空列表，再次落档
	m.SetDeviceList([]DeviceCache{})
	m.flushDirty()

	got := readDeviceListFile(t, path)
	if len(got) != 1 || got[0].DeviceID != "dev1" {
		t.Fatalf("防呆失效：磁盘设备被空列表覆盖，实际 %v", got)
	}
}

// 磁盘为空时，空列表照常落档（新建环境不能被永久拦住）
func TestFlushDirtyGuardAllowsEmptyWhenDiskEmpty(t *testing.T) {
	m := tempManager(t)
	path := m.deviceListLocalPath()

	m.SetDeviceList([]DeviceCache{})
	m.flushDirty()

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("磁盘无内容时应正常写入空列表: %v", err)
	}
	if got := readDeviceListFile(t, path); len(got) != 0 {
		t.Fatalf("应写入空列表，实际 %d 台", len(got))
	}
}

// 用户逐台删到空是显式意图，必须放行（否则重启后设备"复活"）
func TestFlushDirtyGuardAllowsExplicitDeleteToEmpty(t *testing.T) {
	m := tempManager(t)
	path := m.deviceListLocalPath()

	m.SetDeviceList([]DeviceCache{{DeviceID: "dev1", IP: "10.10.0.1"}})
	m.flushDirty()
	if got := readDeviceListFile(t, path); len(got) != 1 {
		t.Fatalf("首次落档后磁盘应为 1 台，实际 %d 台", len(got))
	}

	// 删掉最后一台设备
	m.RemoveDevice("dev1")
	m.flushDirty()

	if got := readDeviceListFile(t, path); len(got) != 0 {
		t.Fatalf("显式删空未落档：磁盘仍有 %d 台（重启后会复活）", len(got))
	}
}

// 待添加列表为空是常态，不受防呆影响
func TestFlushDirtyGuardIgnoresPendingList(t *testing.T) {
	m := tempManager(t)
	path := m.pendingDevicesLocalPath()

	// 先放一台待添加设备并落档
	m.UpsertPendingDevice(DeviceCache{DeviceID: "pend1", IP: "10.10.0.2"})
	m.flushDirty()
	if got := readDeviceListFile(t, path); len(got) != 1 {
		t.Fatalf("首次落档后待添加列表应为 1 台，实际 %d 台", len(got))
	}

	// 清空待添加列表后落档，应正常写入（不触发设备列表的防呆）
	m.ClearPendingDevices()
	m.flushDirty()

	if got := readDeviceListFile(t, path); len(got) != 0 {
		t.Fatalf("待添加列表应正常落档为空，实际仍有 %d 台", len(got))
	}
}
