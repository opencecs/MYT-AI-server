package androidlist

import (
	"errors"
	"sync"
	"testing"

	"github.com/moyunteng/myt/internal/config"
)

// newServiceForQueryTest 构造带通知捕获的 Service。
func newServiceForQueryTest(t *testing.T) (*Service, *int) {
	t.Helper()
	s := NewService(config.NewManager(t.TempDir()))
	s.Start()
	t.Cleanup(s.Stop)
	notifyCount := 0
	s.SetOnUpdated(func() { notifyCount++ })
	return s, &notifyCount
}

// TestQueryDevicePublicViaGateway 公网设备（ip 为空）经注入的网关查询补拉——
// 修复前直接 return：未知容器事件的兜底补拉在公网断链（事件到达但状态静默丢失）。
func TestQueryDevicePublicViaGateway(t *testing.T) {
	s, notifyCount := newServiceForQueryTest(t)
	s.SetPublicListAndroids(func(deviceID string) ([]config.AndroidInfo, error) {
		if deviceID != "pub1" {
			t.Errorf("网关查询应收到 deviceID=pub1，实际 %s", deviceID)
		}
		return []config.AndroidInfo{{ID: "c1", Name: "myt-1", Status: "running", IndexNum: 1, DeviceID: "pub1", IP: "pub1"}}, nil
	})

	s.QueryDevice("pub1", "")

	got := s.configManager.GetAndroidsByDevice("pub1")
	if len(got) != 1 || got[0].ID != "c1" {
		t.Fatalf("公网补拉结果应落缓存，实际 %v", got)
	}
	if *notifyCount == 0 {
		t.Fatal("补拉成功后应触发 onUpdated 通知")
	}
}

// TestQueryDevicePublicNoInjector 未注入网关查询（本地模式形态）时维持 no-op。
func TestQueryDevicePublicNoInjector(t *testing.T) {
	s, notifyCount := newServiceForQueryTest(t)
	s.QueryDevice("pub1", "")
	if *notifyCount != 0 {
		t.Fatal("未注入 publicListAndroids 时不应有任何查询/通知")
	}
}

// TestQueryDevicePublicQueryFailure 网关查询失败：不落缓存、不通知、不 panic。
func TestQueryDevicePublicQueryFailure(t *testing.T) {
	s, notifyCount := newServiceForQueryTest(t)
	s.SetPublicListAndroids(func(deviceID string) ([]config.AndroidInfo, error) {
		return nil, errors.New("gateway timeout")
	})
	s.QueryDevice("pub1", "")
	if got := s.configManager.GetAndroidsByDevice("pub1"); len(got) != 0 {
		t.Fatalf("查询失败不应落缓存，实际 %v", got)
	}
	if *notifyCount != 0 {
		t.Fatal("查询失败不应触发通知")
	}
}

// TestQueryDeviceLocalUnchanged 本地路径（ip 非空）不受公网分支影响——无注入也不干扰。
// 用不可达 IP 验证失败路径静默返回（不通知）。
func TestQueryDeviceLocalUnchanged(t *testing.T) {
	s, notifyCount := newServiceForQueryTest(t)
	var mu sync.Mutex
	called := false
	// 即使注入了公网查询，本地设备（有 IP）也绝不能走网关
	s.SetPublicListAndroids(func(deviceID string) ([]config.AndroidInfo, error) {
		mu.Lock()
		called = true
		mu.Unlock()
		return nil, nil
	})
	s.QueryDevice("dev1", "127.0.0.1:1") // 不可达地址 → 查询失败静默返回
	if *notifyCount != 0 {
		t.Fatal("本地查询失败不应触发通知")
	}
	mu.Lock()
	defer mu.Unlock()
	if called {
		t.Fatal("本地设备（ip 非空）不得走网关查询")
	}
}
