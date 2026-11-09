package tools

// biz.go — 业务级工具的依赖装配入口：BizDeps 窄接口集合 + Register 组装。
// 各业务域的工具注册分布在同目录语义化文件：
//   - biz_device.go：设备列表/云机实例/通用 API 透传
//   - biz_task.go：任务队列
//   - biz_ops.go：镜像/流媒体/维护/iSCSI
//
// BizDeps 以窄接口声明，由 main 包的各 ServiceWrapper 隐式实现
//（mcpserver 不反向依赖 main，避免循环）。

import (
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/moyunteng/myt/internal/config"
	"github.com/moyunteng/myt/internal/mcpserver/reg"
	"github.com/moyunteng/myt/internal/mcpserver/winshot"
	"github.com/moyunteng/myt/internal/sdkapi"
)

// BizDeps 业务级工具背后的服务能力。
type BizDeps struct {
	AndroidList AndroidListSource
	Discover    DiscoverSource
	Screenshot  ScreenshotSource
	SDK         SDKSource
	Tasks       TaskSource
	Mirror      MirrorSource
	Maintenance MaintenanceSource
	Iscsi       IscsiSource

	// 流媒体：返回类型 StreamInfo 定义在 main 包，接口无法引用，改用函数字段
	StreamGet  func() any
	StreamStop func(deviceIPs []string) any

	// 投屏业务工具（start/screenshot/close_screencast；nil = 不注册）
	Screencast *winshot.Deps

	// CastKeys 当前已打开播放器的投屏 key 列表（list_androids 标注 casting 用）
	CastKeys func() []string
}

type AndroidListSource interface {
	GetAndroidList() []config.AndroidInfo
	GetRunningAndroids() []config.AndroidInfo
	QueryDeviceByIdent(ident string)
}

type DiscoverSource interface {
	GetDeviceList() []config.DeviceCache
	RefreshDevices() error
}

type ScreenshotSource interface {
	GetScreenshot(deviceIP string, indexNum int) (string, error)
}

type SDKSource interface {
	ListAndroids(deviceIP string) ([]config.AndroidInfo, error)
	ListPhoneModels(deviceIP string) ([]sdkapi.PhoneModel, error)
	CreateAndroid(deviceIP, name, imageURL, dns string, indexNum int, modelID, modelName, localModel, sandboxSize, width, height, dpi string, start bool, mytBridgeName, macVlanIp string) error
	DeleteAndroid(deviceIP, name string) error
	StartAndroid(deviceIP, name string) error
	StopAndroid(deviceIP, name string) error
	RestartAndroid(deviceIP, name string) error
	BatchRestart(deviceIPs []string, names []string) []sdkapi.BatchOpResult
	BatchStart(deviceIPs []string, names []string) []sdkapi.BatchOpResult
	BatchStop(deviceIPs []string, names []string) []sdkapi.BatchOpResult
	BatchDelete(deviceIPs []string, names []string) []sdkapi.BatchOpResult
	DockerExec(deviceIP, containerName, command string) (string, error)
	BatchDockerExec(deviceIPs []string, containerNames []string, command string) []sdkapi.BatchOpResult
	DeviceFetch(deviceIP, method, path, body string) (string, error)
	GetTaskStatus(deviceIP, taskID string) (string, error)
}

type TaskSource interface {
	CreateBatch(taskType, targetsJSON string) (string, error)
	List() []map[string]any
	Get(taskID string) map[string]any
	Cancel(taskID string)
}

// Register 注册业务级工具：设备/实例/透传（biz_device.go）、任务（biz_task.go）、
// 镜像/流媒体/维护/iSCSI（biz_ops.go）。
func Register(srv *mcp.Server, sink reg.Sink, d *BizDeps) {
	registerDeviceTools(srv, sink, d)
	registerInstanceTools(srv, sink, d)
	registerTaskTools(srv, sink, d)
	registerMirrorTools(srv, sink, d)
	registerStreamTools(srv, sink, d)
	registerMaintenanceTools(srv, sink, d)
	registerIscsiTools(srv, sink, d)
	registerDeviceFetchTool(srv, sink, d)
}
