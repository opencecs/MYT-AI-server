package tools

// biz_device.go — 设备/云机实例域工具：设备列表与截图、实例生命周期
// （创建/开关机/重启/删除/批量）、容器内命令、设备 API 通用透传。

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/moyunteng/myt/internal/config"
	"github.com/moyunteng/myt/internal/mcpserver/reg"
	"github.com/moyunteng/myt/internal/sdkapi"
)

// --- 工具参数结构 ---

// androidWithCast list_androids 响应项：容器信息 + 投屏状态标注
// （嵌入保持原 AndroidInfo json 字段不变）。
type androidWithCast struct {
	config.AndroidInfo
	Casting bool   `json:"casting"`
	CastKey string `json:"castKey,omitempty"`
}

type deviceIPArgs struct {
	DeviceIP string `json:"deviceIp" jsonschema_desc:"设备 IP（如 192.168.1.100）"`
}

type containerRefArgs struct {
	DeviceIP string `json:"deviceIp" jsonschema_desc:"设备 IP"`
	Name     string `json:"name" jsonschema_desc:"云机（容器）名称，如 android-0"`
}

type listContainersArgs struct {
	DeviceIP string `json:"deviceIp" jsonschema_desc:"设备 IP；留空返回全部设备的容器列表"`
}

type createAndroidArgs struct {
	DeviceIP string `json:"deviceIp" jsonschema_desc:"设备 IP"`
	Name     string `json:"name" jsonschema_desc:"新云机名称"`
	ImageURL string `json:"imageUrl" jsonschema_desc:"镜像 URL（可从 list_mirrors 获取）"`
	IndexNum int    `json:"indexNum" jsonschema_desc:"坑位号（从 0 开始，需未被占用）"`
	ModelID  string `json:"modelId,omitempty" jsonschema_desc:"机型 ID（可从 list_phone_models 获取，留空用默认机型）"`
	Start    bool   `json:"start" jsonschema_desc:"创建后是否立即开机（默认 true）"`
}

type screenshotArgs struct {
	DeviceIP string `json:"deviceIp" jsonschema_desc:"设备 IP"`
	IndexNum int    `json:"indexNum" jsonschema_desc:"坑位号"`
}

type batchOpArgs struct {
	DeviceIPs []string `json:"deviceIps" jsonschema_desc:"设备 IP 列表"`
	Names     []string `json:"names" jsonschema_desc:"云机名称列表（与 deviceIps 一一对应）"`
}

type dockerExecArgs struct {
	DeviceIP      string `json:"deviceIp" jsonschema_desc:"设备 IP"`
	ContainerName string `json:"containerName" jsonschema_desc:"容器名"`
	Command       string `json:"command" jsonschema_desc:"要在容器内执行的 shell 命令"`
}

type batchDockerExecArgs struct {
	DeviceIPs      []string `json:"deviceIps" jsonschema_desc:"设备 IP 列表"`
	ContainerNames []string `json:"containerNames" jsonschema_desc:"容器名列表（与 deviceIps 一一对应）"`
	Command        string   `json:"command" jsonschema_desc:"要在容器内执行的 shell 命令"`
}

type deviceFetchArgs struct {
	DeviceIP string `json:"deviceIp" jsonschema_desc:"设备 IP"`
	Method   string `json:"method" jsonschema_desc:"HTTP 方法：GET/POST/PUT/DELETE"`
	Path     string `json:"path" jsonschema_desc:"设备 API 路径（以 / 开头，如 /info/device）"`
	Body     string `json:"body,omitempty" jsonschema_desc:"请求体 JSON（GET 可留空）"`
}

// registerDeviceTools 注册设备列表/截图/发现类工具。
func registerDeviceTools(srv *mcp.Server, sink reg.Sink, d *BizDeps) {
	if d.AndroidList != nil {
		reg.Reg(srv, sink, "list_androids",
			"列出客户端当前缓存的全部云机（容器）列表，含设备 IP/坑位号/容器名/运行状态；"+
				"casting=true 表示该容器投屏播放器已打开（castKey 为投屏 key，可传给 "+
				"start_screencast/screenshot_window/close_screencast 的 android 参数）。无破坏性。",
			func(context.Context, struct{}) (any, error) {
				list := d.AndroidList.GetAndroidList()
				castKeys := map[string]bool{}
				if d.CastKeys != nil {
					for _, k := range d.CastKeys() {
						castKeys[k] = true
					}
				}
				out := make([]androidWithCast, 0, len(list))
				for _, a := range list {
					item := androidWithCast{AndroidInfo: a}
					key := fmt.Sprintf("%s_%d", a.IP, a.IndexNum)
					if castKeys[key] {
						item.Casting = true
						item.CastKey = key
					}
					out = append(out, item)
				}
				return out, nil
			}, reg.ReadOnly())

		reg.Reg(srv, sink, "list_running_androids", "列出当前运行中的云机（容器）。无破坏性。",
			func(context.Context, struct{}) (any, error) { return d.AndroidList.GetRunningAndroids(), nil }, reg.ReadOnly())

		reg.Reg(srv, sink, "refresh_device_containers", "立即查询某台设备的最新容器列表（绕过缓存）。无破坏性。",
			func(_ context.Context, a deviceIPArgs) (any, error) {
				if a.DeviceIP == "" {
					return nil, fmt.Errorf("deviceIp 不能为空")
				}
				d.AndroidList.QueryDeviceByIdent(a.DeviceIP)
				return map[string]any{"result": "已触发查询（异步，稍后可用 list_androids 查看结果）"}, nil
			})
	}
	if d.Screenshot != nil {
		reg.Reg(srv, sink, "get_screenshot",
			"获取指定云机（设备 IP + 坑位号）的实时截屏，返回 data:image/png;base64 字符串。"+
				"缓存未命中时会现场抓取（约 1~3 秒）。无破坏性。",
			func(_ context.Context, a screenshotArgs) (any, error) {
				if a.DeviceIP == "" {
					return nil, fmt.Errorf("deviceIp 不能为空（格式：deviceIp + indexNum，如 192.168.1.100 + 0；可用 list_androids 查询）")
				}
				data, err := d.Screenshot.GetScreenshot(a.DeviceIP, a.IndexNum)
				if err != nil {
					return nil, err
				}
				if data == "" {
					return nil, fmt.Errorf("截图为空: %s_%d（设备可能离线、容器未运行或抓图失败，可用 list_androids 确认后重试）",
						a.DeviceIP, a.IndexNum)
				}
				return map[string]any{"deviceIp": a.DeviceIP, "indexNum": a.IndexNum, "image": data}, nil
			}, reg.ReadOnly())
	}
	if d.Discover != nil {
		reg.Reg(srv, sink, "list_devices", "列出客户端已添加的 ARM 设备列表（设备 ID/IP/在线状态/版本等）。无破坏性。",
			func(context.Context, struct{}) (any, error) { return d.Discover.GetDeviceList(), nil }, reg.ReadOnly())

		reg.Reg(srv, sink, "refresh_devices", "触发一次全网设备发现刷新（UDP 广播 + 云端同步，异步执行）。无破坏性。",
			func(context.Context, struct{}) (any, error) {
				d.Discover.RefreshDevices()
				return map[string]any{"result": "已触发刷新（异步）"}, nil
			})
	}
}

// registerInstanceTools 注册云机实例域工具：实时查询/创建/生命周期/批量/容器命令。
func registerInstanceTools(srv *mcp.Server, sink reg.Sink, d *BizDeps) {
	if d.SDK == nil {
		return
	}
	reg.Reg(srv, sink, "list_device_containers", "实时查询某台设备上的容器列表（直连设备 API，非缓存）。无破坏性。",
		func(_ context.Context, a deviceIPArgs) (any, error) {
			return d.SDK.ListAndroids(a.DeviceIP)
		}, reg.ReadOnly())

	reg.Reg(srv, sink, "list_phone_models", "列出某台设备可用的机型模板。无破坏性。",
		func(_ context.Context, a deviceIPArgs) (any, error) { return d.SDK.ListPhoneModels(a.DeviceIP) }, reg.ReadOnly())

	reg.Reg(srv, sink, "create_android",
		"在一台设备上创建新云机（容器）。非破坏性但会占用坑位与镜像资源；image_url 可从 list_mirrors 获取，"+
			"model_id 可从 list_phone_models 获取（留空用设备默认机型）。",
		func(_ context.Context, a createAndroidArgs) (any, error) {
			if a.Name == "" || a.DeviceIP == "" {
				return nil, fmt.Errorf("deviceIp 与 name 不能为空")
			}
			if err := d.SDK.CreateAndroid(a.DeviceIP, a.Name, a.ImageURL, "", a.IndexNum,
				a.ModelID, "", "", "", "", "", "", a.Start, "", ""); err != nil {
				return nil, err
			}
			return map[string]any{"result": "创建请求已提交", "name": a.Name}, nil
		})

	lifecycle := []struct {
		name, desc  string
		fn          func(deviceIP, name string) error
		destructive bool
	}{
		{"start_android", "启动一台云机（容器开机）。", d.SDK.StartAndroid, false},
		{"stop_android", "停止一台云机（容器关机，运行中数据可能丢失）。", d.SDK.StopAndroid, true},
		{"restart_android", "重启一台云机（容器重启，运行中数据可能丢失）。", d.SDK.RestartAndroid, true},
		{"delete_android", "【破坏性】永久删除一台云机（容器及其数据不可恢复！）。",
			d.SDK.DeleteAndroid, true},
	}
	for _, lc := range lifecycle {
		lc := lc
		var ann *mcp.ToolAnnotations
		if lc.destructive {
			ann = reg.Destructive()
		}
		reg.Reg(srv, sink, lc.name, lc.desc+"参数：deviceIp + name。",
			func(_ context.Context, a containerRefArgs) (any, error) {
				if err := lc.fn(a.DeviceIP, a.Name); err != nil {
					return nil, err
				}
				return map[string]any{"result": "操作已提交", "name": a.Name}, nil
			}, ann)
	}

	batches := []struct {
		name, desc string
		fn         func([]string, []string) []sdkapi.BatchOpResult
		destruct   bool
	}{
		{"batch_restart_androids", "批量重启云机（每台独立返回成功/失败）。", d.SDK.BatchRestart, true},
		{"batch_start_androids", "批量启动云机（每台独立返回成功/失败）。", d.SDK.BatchStart, false},
		{"batch_stop_androids", "批量停止云机（会中断运行中的云机）。", d.SDK.BatchStop, true},
		{"batch_delete_androids", "【破坏性】批量永久删除云机（数据不可恢复，务必核对名单）。", d.SDK.BatchDelete, true},
	}
	for _, b := range batches {
		b := b
		var ann *mcp.ToolAnnotations
		if b.destruct {
			ann = reg.Destructive()
		}
		reg.Reg(srv, sink, b.name, b.desc+"参数：deviceIps[] + names[]（一一对应）。",
			func(_ context.Context, a batchOpArgs) (any, error) {
				return b.fn(a.DeviceIPs, a.Names), nil
			}, ann)
	}

	reg.Reg(srv, sink, "docker_exec",
		"【高危】在云机容器内执行任意 shell 命令（root 权限，可直接改动/删除容器内数据）。"+
			"仅用于诊断与维护，执行前务必与用户确认命令内容。",
		func(_ context.Context, a dockerExecArgs) (any, error) {
			out, err := d.SDK.DockerExec(a.DeviceIP, a.ContainerName, a.Command)
			if err != nil {
				return nil, err
			}
			return map[string]any{"output": out}, nil
		}, reg.Destructive())

	reg.Reg(srv, sink, "batch_docker_exec",
		"【高危】在多台云机容器内批量执行同一条 shell 命令（每台独立返回结果，root 权限）。"+
			"影响面大，执行前务必与用户确认命令内容与目标名单。",
		func(_ context.Context, a batchDockerExecArgs) (any, error) {
			return d.SDK.BatchDockerExec(a.DeviceIPs, a.ContainerNames, a.Command), nil
		}, reg.Destructive())
}

// registerDeviceFetchTool 通用兜底：任意设备 API 透传。
func registerDeviceFetchTool(srv *mcp.Server, sink reg.Sink, d *BizDeps) {
	if d.SDK == nil {
		return
	}
	reg.Reg(srv, sink, "device_fetch",
		"【高级/高危】向设备管理 API（端口 8000）发起任意 HTTP 请求并返回原始响应。"+
			"仅当既有工具无法满足时使用：路径/方法/请求体格式需自行参考设备 API 文档，"+
			"POST/DELETE 可能触发不可逆的设备级操作，务必先与用户确认。",
		func(_ context.Context, a deviceFetchArgs) (any, error) {
			method := validFetchMethod(a.Method)
			if method == "" {
				return nil, fmt.Errorf("不支持的 HTTP 方法 %q（仅 GET/POST/PUT/DELETE）", a.Method)
			}
			if a.DeviceIP == "" || !strings.HasPrefix(a.Path, "/") {
				return nil, fmt.Errorf("deviceIp 不能为空，path 必须以 / 开头")
			}
			out, err := d.SDK.DeviceFetch(a.DeviceIP, method, a.Path, a.Body)
			if err != nil {
				return nil, err
			}
			return map[string]any{"response": out}, nil
		}, reg.Destructive())
}

// validFetchMethod device_fetch 的 HTTP 方法白名单校验（合法返回大写方法名）。
func validFetchMethod(method string) string {
	m := strings.ToUpper(strings.TrimSpace(method))
	switch m {
	case "GET", "POST", "PUT", "DELETE":
		return m
	default:
		return ""
	}
}
