package screencast

import (
	"fmt"
	"log"
	"strings"
	"sync/atomic"

	"github.com/moyunteng/myt/internal/config"
	"github.com/moyunteng/myt/internal/ports"
	"github.com/moyunteng/myt/internal/util"
)

// ExitCodeRotate 旋转重开请求：go_legacy 退出码 42 = WebURL 模式请求翻转 orientation 重新打开。
// player 侧 exit_codes.go 定义同值常量，两处必须一致。
const ExitCodeRotate = 42

// SlotRef 设备坑位引用，替代 (deviceIP, indexNum) 数据泥团
type SlotRef struct {
	DeviceIP string
	IndexNum int
}

func (r SlotRef) String() string {
	return fmt.Sprintf("%s_%d", r.DeviceIP, r.IndexNum)
}

// WinPosUnset 窗口位置哨兵：表示 -winx/-winy 未传，go_legacy 端居中显示。
// 与 go_legacy 的 winPosUnset 常量保持一致。0 是合法位置（第一列/第一行）。
const WinPosUnset = -32000

// ─── 尺寸常量（与 myt-client-player 的 layout_constants.go 保持一致）──────────
// 跨 Go module 不能直接引用，两处值必须一致。修改时同步改。
const (
	// 视频源比例：横屏 1280x720 转竖后 eff=720x1280。排列用此比例匹配横屏主流，
	// 竖屏源(480:800)的 ~6px 偏差由 player letterbox 填满阈值处理。
	VideoAspectW = 720.0
	VideoAspectH = 1280.0
	VideoAspect  = VideoAspectW / VideoAspectH // 0.5625

	// 横屏投屏（-landscape，视频不旋转）：窗口视频区按横屏源 1280x720 匹配，
	// 否则横屏视频塞竖屏格子会上下大黑边（视频仅占窗口高 ~32%）。
	LandscapeAspect = VideoAspectH / VideoAspectW // 1.7778

	// player 导航栏高度（返回/主页/最近按钮）。排列 cellH 必须含此值，
	// 否则视频区比预期小 → “两个底部栏”/拖滑块变大变不回。
	// NoSidebar 批量投屏仍保留导航栏（用户需要按钮）。
	NavBarHeight = 48

	// player 侧边栏折叠宽度（有侧边栏时占用的客户区宽）。排列 winw 必须含此值，
	// 否则 sdlArea(=客户区-sideW) 比例偏瘦 → 有侧边栏上下黑边。
	// NoSidebar 时 sideW=0。与 player layout_constants.go sideCollapsedDefault 一致。
	SideCollapsedW = 52

	// player matrixUIScale 缩放下限（与 player layout_constants.go sideCollapsedMin 一致）。
	// 批量小窗口 sideW 被 matrixUIScale 缩到 SideCollapsedMin，固定 52 会导致 sdlArea 比例偏移黑边。
	SideCollapsedMin = 18.0

	// player matrixUIScale 基准客户区尺寸（与 player baseClientW/H 一致），
	// 用于反推批量小窗口 navBar 缩放：navBar = NavBarHeight × min(w/baseW, h/baseH)。
	// 批量小窗口 navBar 被缩到 NavBarMinHeight(18)，固定 48 会导致 sdlArea 比例偏移黑边。
	BaseClientW     = 464.0
	BaseClientH     = 780.0
	NavBarMinHeight = 18.0

	// 排列兜底（防 0/负，与 player layout_constants.go minBaseW 一致）
	MinBaseW = 40.0
)

// currentLandscape 当前批量投屏会话的横竖屏方向（批量/单台启动时设置，
// 排列按钮 doArrangePlayerWindows 读取）。与 SetSideW 同款会话级语义：
// 混合横竖屏窗口时按最后一批投屏方向排格。
// 平台无关单一定义（currentSideW 在 windows/darwin layout 文件里按 build tag 各声明一份，
// 这里避免再复制一份），darwin 批量投屏同样会调用 SetLandscape。
var currentLandscape atomic.Bool

// SetLandscape 更新当前投屏会话的横竖屏方向（批量启动时调用）。
func SetLandscape(v bool) { currentLandscape.Store(v) }

// GetLandscape 读取当前投屏会话的横竖屏方向。
func GetLandscape() bool { return currentLandscape.Load() }

// Projection 投屏窗口接口，由平台特定实现
type Projection interface {
	Stop()
	SetOnExit(fn func(exitCode int, failed bool, cfg *ProjectionConfig))
	UpdateTitle(title string)
	BringToFront()
	// PID 播放器进程 PID（0 = 平台未跟踪进程句柄）。
	// screenshot_window 工具经「容器 key → PID → 窗口」链路定位投屏窗口。
	PID() int
	// ControlPort 播放器本地控制 HTTP 端口（0 = 未启用）。
	// screenshot_window/close_screencast 经 http://127.0.0.1:<port> 控制播放器。
	ControlPort() int
}

// RTSPParams RTSP 本地投屏专用参数
type RTSPParams struct {
	VideoPort   int
	ControlPort int
	ApiPort     int
	DeviceIP    string
}

// MatrixParams 矩阵/群控投屏参数（本地 CtrlList 广播 + 公网 UDP/fanout/RPA 扇出）
type MatrixParams struct {
	CtrlList      string // 批量控制目标列表 "ip:port#ip:port"
	AllCtrlList   string // 矩阵投屏全部控制地址列表（含主控）
	SelfAddr      string // 当前窗口自己的控制地址
	PrimaryAddr   string // 主控地址（被控窗口侧边栏显示）
	MatrixListen  int    // 本实例 UDP 监听端口（127.0.0.1），矩阵实例必传
	MatrixPrimary bool   // 本实例初始为主控（捕获页面输入并广播）
	MatrixTargets string // 主控广播目标端口列表 "p1,p2"（仅主控有意义）
	FanoutURLs    string // 公网矩阵 v2：fanout WebSocket 地址列表 "ws://...|ws://..."
	GroupRPA      string // 公网矩阵 v3：导航按钮群控 RPA "baseURL|token|containers"
	GroupInput    string // 公网矩阵 v4：群控输入 HTTP 扇出 "baseURL|token|targets_str"
	Hidden        bool   // 隐藏窗口模式（被控实例后台运行，不显示窗口 UI）
}

// WindowLayout 窗口位置与缩放参数（主程序预计算后传入）
type WindowLayout struct {
	WinX  int // 哨兵 WinPosUnset(-32000) = 未设=居中
	WinY  int
	WinW  int
	WinH  int
	Scale float64 // 矩阵投屏窗口缩放因子（0.2~1.0，默认0.4）
	Fill  bool    // 画面拉伸填满 SDL 区（跳过 letterbox 比例，轻微变形）——单台竖屏对齐壳子画面比例用
}

// ProjectionConfig 投屏参数（嵌入 RTSPParams + MatrixParams + WindowLayout，
// 字段通过 Go 结构体嵌入自动提升，现有代码 cfg.VideoPort 等访问无需修改）
type ProjectionConfig struct {
	RTSPParams
	MatrixParams
	WindowLayout
	TeboxParams

	Title      string
	Controlled bool   // 此窗口是被控窗口（禁止移动/调整大小）
	InitDelay  int    // 启动延迟（毫秒），批量投屏时递增以错开 D3D11VA 初始化
	NotifyPort int    // 首帧就绪通知 UDP 端口（父进程错开启动用，0=禁用）
	NoSidebar  bool   // 禁用侧边栏（不启动 WebView2，省 ~80MB/窗口）
	GpuAccel   bool   // GPU 硬件加速解码（默认 false，关闭后走 CPU 软解）
	WebURL     string // WebView2 加载的 URL（公网投屏模式）
	Landscape  bool   // 横屏模式（true=横屏，false=竖屏）

	// 容器安装上下文（侧边栏上传 APK 后安装用，与 MYT 客户端 UploadToCloud 同路径）：
	// ContainerName 目标云机容器名；SdkHost 设备 SDK 宿主地址（8000 端口固定）；
	// SdkPassword 设备 SDK Basic Auth 密码（可空）。空 ContainerName = 播放器禁用安装（仅上传）。
	ContainerName string
	SdkHost       string
	SdkPassword   string

	// V2Mode V2 部署模式标志：V2 主机无 8000 SDK，安装命令改走主机 81 的
	// /and_api/v1/shell/{ip}/{容器名}（body {"cmd"}，无鉴权，{code:200} 成功），
	// 语义对照 internal/sdkapi/v2route.go 的 /android/exec 翻译。false=原 8000 通道。
	V2Mode bool

	// RevealOnArranged 排版后再显示（已弃用：改为启动时直接传位置 WinX/Y/W/H，
	// 窗口一出来就在网格位置，无需隐藏/排列/显示三阶段）。保留字段兼容旧调用。
	RevealOnArranged bool

	// 摄像头推流（由主程序传入 WindowsPusher.exe 路径和设备 camera TCP 端口）
	CamPushExe string // WindowsPusher.exe 路径（空=不启用摄像头推流侧边栏功能）
	CamTCPPort int    // 设备 camera TCP 端口（0=不启用）
}

// macvlan直连：容器IP + 10000/10001
// 端口映射：宿主机IP + portBindings映射端口（或公式计算）
func CalcProjectionParams(android config.AndroidInfo, deviceIP string) ProjectionConfig {
	isMacvlan := ports.IsMacvlanDirect(android.NetworkName, android.PortBindings)
	// 宽松直连判断：网络名含 "myt" 且容器有独立 IP，即使 portBindings 非空也尝试直连。
	// 某些 myt 网络容器有部分端口映射条目（如 9082/tcp），但投屏端口 10000/10001 走容器 IP 直连。
	// IsMacvlanDirect 要求 portBindings 完全为空会漏判这种情况，导致 fallback 到公式端口连不上。
	isMytWithContainerIP := strings.Contains(strings.ToLower(android.NetworkName), "myt") && android.ContainerIP != ""
	if util.DebugEnabled {
		log.Printf("[screencast/ports:DEBUG] CalcProjectionParams: ip=%s index=%d networkName=%s containerIP=%s isMacvlan=%v isMytWithContainerIP=%v portBindings=%v",
			android.IP, android.IndexNum, android.NetworkName, android.ContainerIP, isMacvlan, isMytWithContainerIP, android.PortBindings)
	}
	if isMacvlan && android.ContainerIP != "" {
		vd := ports.VideoDirect(android)
		cd := ports.ControlDirect(android)
		ad := ports.MYTOSDirect(android)
		camD := ports.CameraTCPDirect(android)
		if util.DebugEnabled {
			log.Printf("[screencast/ports:DEBUG] → macvlan直连: video=%s:%d control=%s:%d api=%s:%d cam=%s:%d", vd.Host, vd.Port, cd.Host, cd.Port, ad.Host, ad.Port, camD.Host, camD.Port)
		}
		return ProjectionConfig{
			RTSPParams: RTSPParams{
				VideoPort:   vd.Port,
				ControlPort: cd.Port,
				ApiPort:     ad.Port,
				DeviceIP:    vd.Host,
			},
			CamTCPPort:    camD.Port,
			ContainerName: android.Name,
			SdkHost:       deviceIP,
		}
	}

	videoAddr := ports.Video(android, deviceIP)
	controlAddr := ports.Control(android, deviceIP)

	// 宽松直连 fallback：端口映射路径查不到 10000/10001 的映射（fallback 到公式端口），
	// 但网络名含 "myt" 且容器有独立 IP → 改用容器 IP + 容器内部端口直连
	if isMytWithContainerIP {
		hasVideoMapping := ports.GetMappedPortAny(android.PortBindings, "10000") > 0
		hasControlMapping := ports.GetMappedPortAny(android.PortBindings, "10001") > 0
		if !hasVideoMapping || !hasControlMapping {
			vd := ports.VideoDirect(android)
			cd := ports.ControlDirect(android)
			ad := ports.MYTOSDirect(android)
			camD := ports.CameraTCPDirect(android)
			if util.DebugEnabled {
				log.Printf("[screencast/ports:DEBUG] → myt宽松直连fallback(端口映射缺10000/10001): video=%s:%d control=%s:%d api=%s:%d cam=%s:%d",
					vd.Host, vd.Port, cd.Host, cd.Port, ad.Host, ad.Port, camD.Host, camD.Port)
			}
			return ProjectionConfig{
				RTSPParams: RTSPParams{
					VideoPort:   vd.Port,
					ControlPort: cd.Port,
					ApiPort:     ad.Port,
					DeviceIP:    vd.Host,
				},
				CamTCPPort:    camD.Port,
				ContainerName: android.Name,
				SdkHost:       deviceIP,
			}
		}
	}

	apiAddr := ports.MYTOS(android, deviceIP)
	camAddr := ports.CameraTCP(android, deviceIP)
	// 摄像头端口口径与前端 resolveCameraPort 统一（P2 核对结论：原先缺 10006 映射时
	// 这里走公式兜底 30000+(index-1)*100+5，前端却返回 null 报 cameraPortMissing）：
	// 1) myt 网络容器（有容器 IP）：恒用容器端口 10006 直连（前端 isMacvlanDirect 恒取容器端口）；
	// 2) 非 myt：仅 portBindings 有 10006 映射才用映射端口，缺映射置 0（= 不启用摄像头推流）。
	if isMytWithContainerIP {
		camAddr = ports.CameraTCPDirect(android)
	} else if ports.GetMappedPortAny(android.PortBindings, "10006") == 0 {
		camAddr = ports.Addr{Host: deviceIP, Port: 0}
	}
	if util.DebugEnabled {
		log.Printf("[screencast/ports:DEBUG] → 端口映射: video=%s:%d control=%s:%d api=%s:%d cam=%s:%d", videoAddr.Host, videoAddr.Port, controlAddr.Host, controlAddr.Port, apiAddr.Host, apiAddr.Port, camAddr.Host, camAddr.Port)
	}

	return ProjectionConfig{
		RTSPParams: RTSPParams{
			VideoPort:   videoAddr.Port,
			ControlPort: controlAddr.Port,
			ApiPort:     apiAddr.Port,
			DeviceIP:    videoAddr.Host,
		},
		CamTCPPort:    camAddr.Port,
		ContainerName: android.Name,
		SdkHost:       deviceIP,
	}
}

// CtrlAddr 计算单个安卓的控制地址 "ip:port"
func CtrlAddr(android config.AndroidInfo, deviceIP string) string {
	if ports.IsMacvlanDirect(android.NetworkName, android.PortBindings) && android.ContainerIP != "" {
		return ports.ControlDirect(android).String()
	}
	// myt 宽松直连 fallback：端口映射中查不到 10001 但网络名含 "myt" 且有容器 IP
	if strings.Contains(strings.ToLower(android.NetworkName), "myt") && android.ContainerIP != "" {
		if ports.GetMappedPortAny(android.PortBindings, "10001") == 0 {
			return ports.ControlDirect(android).String()
		}
	}
	addr := ports.Control(android, deviceIP)
	return addr.String()
}

// BuildCtrlList 组装批量控制地址列表（排除主控安卓）
// deviceIPs: 每台安卓对应的宿主机 IP（端口映射模式需要），与 androids 一一对应
func BuildCtrlList(androids []config.AndroidInfo, primary config.AndroidInfo, deviceIPs []string) string {
	var parts []string
	for i, a := range androids {
		if a.ID == primary.ID {
			continue
		}
		ip := a.IP
		if i < len(deviceIPs) {
			ip = deviceIPs[i]
		}
		parts = append(parts, CtrlAddr(a, ip))
	}
	return strings.Join(parts, "#")
}

// BuildAllCtrlList 组装矩阵投屏全部控制地址列表（包含主控）。
// deviceIPs: 每台安卓对应的宿主机 IP（端口映射模式需要），与 androids 一一对应
func BuildAllCtrlList(androids []config.AndroidInfo, deviceIPs []string) string {
	parts := make([]string, 0, len(androids))
	for i, a := range androids {
		ip := a.IP
		if i < len(deviceIPs) {
			ip = deviceIPs[i]
		}
		parts = append(parts, CtrlAddr(a, ip))
	}
	return strings.Join(parts, "#")
}
