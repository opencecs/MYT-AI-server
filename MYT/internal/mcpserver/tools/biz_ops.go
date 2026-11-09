package tools

// biz_ops.go — 运维查询域工具：镜像源、流媒体会话、维护用量（日志/缓存/面板）、
// iSCSI 状态。均为只读查询（stop_device_play 除外）。

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/moyunteng/myt/internal/mcpserver/reg"
	"github.com/moyunteng/myt/internal/sdkapi"
	"github.com/moyunteng/myt/internal/util"
)

// 其余业务域的依赖接口。Stream 用函数字段：StreamInfo 定义在 main 包，
// 接口无法引用（mcpserver 不反向依赖 main）。

type MirrorSource interface {
	GetCachedMirrors() []util.MirrorItem
	FetchMirrors() ([]util.MirrorItem, error)
}

type MaintenanceSource interface {
	GetLogUsage(deviceIP, androidName string) map[string]interface{}
	GetAndroidCacheUsage(deviceIP, androidName string) map[string]interface{}
	CheckMytPanelInstalled(deviceIP string) map[string]interface{}
}

type IscsiSource interface {
	GetIscsiStatus(deviceIP string) (*sdkapi.IscsiStorageStatus, error)
	ListIscsi(deviceIP string) ([]sdkapi.IscsiSessionInfo, error)
}

// --- 工具参数结构 ---

type deviceIPOnlyArgs struct {
	DeviceIP string `json:"deviceIp" jsonschema_desc:"设备 IP"`
}

type logUsageArgs struct {
	DeviceIP    string `json:"deviceIp" jsonschema_desc:"设备 IP"`
	AndroidName string `json:"androidName" jsonschema_desc:"云机（容器）名称"`
}

// registerMirrorTools 注册镜像源工具。
func registerMirrorTools(srv *mcp.Server, sink reg.Sink, d *BizDeps) {
	if d.Mirror == nil {
		return
	}
	reg.Reg(srv, sink, "list_mirrors", "列出本地缓存的镜像源列表（含镜像 URL，可用于创建云机）。无破坏性。",
		func(context.Context, struct{}) (any, error) { return d.Mirror.GetCachedMirrors(), nil }, reg.ReadOnly())

	reg.Reg(srv, sink, "fetch_mirrors", "从镜像源实时拉取最新镜像列表（网络请求，结果同时更新本地缓存）。无破坏性。",
		func(context.Context, struct{}) (any, error) { return d.Mirror.FetchMirrors() })
}

// registerStreamTools 注册流媒体会话工具。
func registerStreamTools(srv *mcp.Server, sink reg.Sink, d *BizDeps) {
	if d.StreamGet == nil {
		return
	}
	reg.Reg(srv, sink, "get_active_streams", "列出当前活跃的推流会话（流媒体 Tab 数据）。无破坏性。",
		func(context.Context, struct{}) (any, error) { return d.StreamGet(), nil }, reg.ReadOnly())

	reg.Reg(srv, sink, "stop_device_play", "停止指定设备的投屏播放（中断正在观看的画面，不影响云机运行）。",
		func(_ context.Context, a deviceIPOnlyArgs) (any, error) {
			if a.DeviceIP == "" {
				return nil, fmt.Errorf("deviceIp 不能为空")
			}
			return d.StreamStop([]string{a.DeviceIP}), nil
		}, reg.Destructive())
}

// registerMaintenanceTools 注册维护用量查询工具。
func registerMaintenanceTools(srv *mcp.Server, sink reg.Sink, d *BizDeps) {
	if d.Maintenance == nil {
		return
	}
	reg.Reg(srv, sink, "get_log_usage", "查询云机日志占用情况（各分类大小）。无破坏性。",
		func(_ context.Context, a logUsageArgs) (any, error) {
			return d.Maintenance.GetLogUsage(a.DeviceIP, a.AndroidName), nil
		}, reg.ReadOnly())

	reg.Reg(srv, sink, "get_android_cache_usage", "查询云机缓存占用情况（各分类大小）。无破坏性。",
		func(_ context.Context, a logUsageArgs) (any, error) {
			return d.Maintenance.GetAndroidCacheUsage(a.DeviceIP, a.AndroidName), nil
		}, reg.ReadOnly())

	reg.Reg(srv, sink, "check_panel_installed", "查询设备是否安装了 MYT 面板及其版本。无破坏性。",
		func(_ context.Context, a deviceIPOnlyArgs) (any, error) {
			return d.Maintenance.CheckMytPanelInstalled(a.DeviceIP), nil
		}, reg.ReadOnly())
}

// registerIscsiTools 注册 iSCSI 状态查询工具。
func registerIscsiTools(srv *mcp.Server, sink reg.Sink, d *BizDeps) {
	if d.Iscsi == nil {
		return
	}
	reg.Reg(srv, sink, "get_iscsi_status", "查询设备的 iSCSI 存储状态（会话/挂载/容量）。无破坏性。",
		func(_ context.Context, a deviceIPOnlyArgs) (any, error) {
			return d.Iscsi.GetIscsiStatus(a.DeviceIP)
		}, reg.ReadOnly())

	reg.Reg(srv, sink, "list_iscsi_sessions", "列出设备的 iSCSI 会话。无破坏性。",
		func(_ context.Context, a deviceIPOnlyArgs) (any, error) {
			return d.Iscsi.ListIscsi(a.DeviceIP)
		}, reg.ReadOnly())
}
