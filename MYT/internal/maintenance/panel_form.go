package maintenance

import (
	"fmt"
)

// 面板形态（PanelForm）：魔云互联设备端面板按部署模式分两种运行形态，
// SSH 推二进制链路的所有命令/服务文件/健康检查都由形态参数驱动，
// 不再散落硬编码的 myt-panel/8081：
//   - v3（默认）：binary myt-panel，启动参数 -port 8081，监听 8081
//   - v2：binary myt-panel-v2，启动参数 -host-ip <设备IP> -sdk-port 81
//     -port 8181，监听 8181（源码 myt-panel/main.go 的 v2 编译模式参数集，
//     host-ip 必填；sdk-port 81 为超级 SDK Web API 宿主端口，与
//     v2host.SuperSdkPort 同值——maintenance 不 import v2host，此处独立常量）
//
// 注入走包级回调（与 SetV2HostIPResolver/SetPanelDeviceType 同模式，
// internal 不能 import main）。

const (
	// v3PanelPort v3 形态面板 Web 端口
	v3PanelPort = 8081
	// v2PanelPort v2 形态面板 Web 端口
	v2PanelPort = 8181
	// v2PanelSdkPort v2 形态面板启动参数 -sdk-port（超级 SDK 宿主端口）
	v2PanelSdkPort = 81
	// panelRemoteDir 面板在设备上的部署目录（服务 WorkingDirectory/日志目录）
	panelRemoteDir = "/home/user"
)

// PanelForm 设备端面板的一种运行形态（binary/启动参数/监听端口三元组）。
type PanelForm struct {
	// Binary 设备上的二进制文件名（/home/user/<Binary>）
	Binary string
	// Args 启动参数（不含二进制名本身；v2 含 -host-ip <IP>，随设备而异）
	Args string
	// Port 面板 Web 监听端口（健康检查/URL/服务文件共用）
	Port int
}

// v3PanelForm v3 默认形态（与历史硬编码行为逐字一致，v3 模式零变化）。
func v3PanelForm() PanelForm {
	return PanelForm{Binary: "myt-panel", Args: fmt.Sprintf("-port %d", v3PanelPort), Port: v3PanelPort}
}

// v2PanelForm v2 形态：host-ip 为解析后的设备宿主 IP（调用点在 resolveHostIP 之后）。
func v2PanelForm(hostIP string) PanelForm {
	return PanelForm{
		Binary: "myt-panel-v2",
		Args:   fmt.Sprintf("-host-ip %s -sdk-port %d -port %d", hostIP, v2PanelSdkPort, v2PanelPort),
		Port:   v2PanelPort,
	}
}

// RemotePath 设备上的二进制绝对路径。
func (f PanelForm) RemotePath() string { return panelRemoteDir + "/" + f.Binary }

// CommandLine 完整启动命令行（二进制 + 参数），服务文件/nohup/respawn 共用。
func (f PanelForm) CommandLine() string { return f.RemotePath() + " " + f.Args }

// RelCommandLine 相对路径形态的启动命令行（nohup 在 /home/user 下执行用）。
func (f PanelForm) RelCommandLine() string { return "./" + f.Binary + " " + f.Args }

// URL 面板 Web 访问地址。
func (f PanelForm) URL(hostIP string) string { return fmt.Sprintf("http://%s:%d", hostIP, f.Port) }

// ListenGrep 健康检查的 SSH 命令（netstat/ss 监听端口匹配）。
func (f PanelForm) ListenGrep() string {
	return fmt.Sprintf("netstat -tlnp 2>/dev/null | grep %d || ss -tlnp | grep %d", f.Port, f.Port)
}

// panelFormByMode 按部署模式返回面板形态（isV2Mode 由 setup 注入）。
func panelFormByMode(hostIP string) PanelForm {
	if isV2Mode != nil && isV2Mode() {
		return v2PanelForm(hostIP)
	}
	return v3PanelForm()
}

// isV2Mode 当前是否 v2 部署模式（setup 注入，nil = 非 v2）。
var isV2Mode func() bool

// SetPanelV2Mode 注入部署模式判定（main 包 setup 调用；与 SetPanelDeviceType
// 同模式。v2_service_panel.go 的 Match 回调带 configManager，包级注入拿不到，
// 单独收窄为 bool 判定）。
func SetPanelV2Mode(fn func() bool) {
	isV2Mode = fn
}
