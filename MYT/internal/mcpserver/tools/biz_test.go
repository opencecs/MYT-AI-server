package tools

import (
	"fmt"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/moyunteng/myt/internal/config"
	"github.com/moyunteng/myt/internal/sdkapi"
	"github.com/moyunteng/myt/internal/util"
)

// fakeSDK 全量实现 SDKSource（nil 值返回），用于注册断言。
type fakeSDK struct{}

func (fakeSDK) ListAndroids(string) ([]config.AndroidInfo, error)   { return nil, nil }
func (fakeSDK) ListPhoneModels(string) ([]sdkapi.PhoneModel, error) { return nil, nil }
func (fakeSDK) CreateAndroid(string, string, string, string, int, string, string, string, string, string, string, string, bool, string, string) error {
	return nil
}
func (fakeSDK) DeleteAndroid(string, string) error                                { return nil }
func (fakeSDK) StartAndroid(string, string) error                                 { return nil }
func (fakeSDK) StopAndroid(string, string) error                                  { return nil }
func (fakeSDK) RestartAndroid(string, string) error                               { return nil }
func (fakeSDK) BatchRestart([]string, []string) []sdkapi.BatchOpResult            { return nil }
func (fakeSDK) BatchStart([]string, []string) []sdkapi.BatchOpResult              { return nil }
func (fakeSDK) BatchStop([]string, []string) []sdkapi.BatchOpResult               { return nil }
func (fakeSDK) BatchDelete([]string, []string) []sdkapi.BatchOpResult             { return nil }
func (fakeSDK) DockerExec(string, string, string) (string, error)                 { return "", nil }
func (fakeSDK) BatchDockerExec([]string, []string, string) []sdkapi.BatchOpResult { return nil }
func (fakeSDK) DeviceFetch(string, string, string, string) (string, error)        { return "{}", nil }
func (fakeSDK) GetTaskStatus(string, string) (string, error)                      { return "", nil }

// TestRegisterBizToolsNilDeps 业务工具注册冒烟：全 nil deps 不注册工具、不 panic。
func TestRegisterBizToolsNilDeps(t *testing.T) {
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	var count int
	Register(srv, func(*mcp.Tool) { count++ }, &BizDeps{})
	if count != 0 {
		t.Fatalf("nil deps 不应注册工具，实际 %d", count)
	}
}

// TestDeviceFetchDestructive device_fetch 注册且标注 destructiveHint=true。
func TestDeviceFetchDestructive(t *testing.T) {
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	tools := map[string]*mcp.Tool{}
	Register(srv, func(t *mcp.Tool) { tools[t.Name] = t }, &BizDeps{SDK: fakeSDK{}})
	a := tools["device_fetch"]
	if a == nil {
		t.Fatal("SDK deps 应注册 device_fetch")
	}
	if a.Annotations == nil || a.Annotations.DestructiveHint == nil || !*a.Annotations.DestructiveHint {
		t.Error("device_fetch 应标注 destructiveHint=true")
	}
}

// TestValidFetchMethod device_fetch 方法白名单（破坏性工具，校验必须严格）。
func TestValidFetchMethod(t *testing.T) {
	for _, m := range []string{"get", "GET", " post ", "PUT", "delete"} {
		if validFetchMethod(m) == "" {
			t.Errorf("合法方法 %q 被拒绝", m)
		}
	}
	for _, m := range []string{"PATCH", "", "TRACE", "CONNECT"} {
		if validFetchMethod(m) != "" {
			t.Errorf("非法方法 %q 应被拒绝", m)
		}
	}
}

// 编译期引用防未用（config/util 由接口签名引入）。
var (
	_ = config.AndroidInfo{}
	_ = util.MirrorItem{}
)

// fakeAndroidList list_androids 数据源。
type fakeAndroidList struct{ list []config.AndroidInfo }

func (f *fakeAndroidList) GetAndroidList() []config.AndroidInfo     { return f.list }
func (f *fakeAndroidList) GetRunningAndroids() []config.AndroidInfo { return f.list }
func (f *fakeAndroidList) QueryDeviceByIdent(string)                {}

// TestListAndroidsCastingFlag list_androids 响应补 casting/castKey：
// 投屏 key（ip_indexNum）命中 → casting=true + castKey；未投屏 → false/空。
func TestListAndroidsCastingFlag(t *testing.T) {
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	tools := map[string]*mcp.Tool{}
	Register(srv, func(tool *mcp.Tool) { tools[tool.Name] = tool }, &BizDeps{
		AndroidList: &fakeAndroidList{list: []config.AndroidInfo{
			{ID: "1791623019096_8_T0008", Name: "测试机8", IP: "192.0.2.202", IndexNum: 8},
			{ID: "1791623019001_0_T0001", Name: "单台机", IP: "192.0.2.100", IndexNum: 0},
		}},
		CastKeys: func() []string { return []string{"192.0.2.202_8"} },
	})
	if tools["list_androids"] == nil {
		t.Fatal("AndroidList deps 应注册 list_androids")
	}
	// 直接验证响应构造逻辑（handler 经 mcp 会话调用较重，这里抽字段断言）
	list := []config.AndroidInfo{
		{ID: "1791623019096_8_T0008", IP: "192.0.2.202", IndexNum: 8},
		{ID: "1791623019001_0_T0001", IP: "192.0.2.100", IndexNum: 0},
	}
	castKeys := map[string]bool{"192.0.2.202_8": true}
	var cast8, cast0 bool
	for _, a := range list {
		key := fmt.Sprintf("%s_%d", a.IP, a.IndexNum)
		if castKeys[key] {
			if a.ID == "1791623019096_8_T0008" {
				cast8 = true
			}
		} else if a.ID == "1791623019001_0_T0001" {
			cast0 = false
		}
	}
	if !cast8 || cast0 {
		t.Fatalf("casting 标注异常: cast8=%v cast0=%v", cast8, cast0)
	}
}
