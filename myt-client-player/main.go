package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
)

// ─── Command-line flags ───────────────────────────────────────────────────────

// winPosUnset 哨兵值：表示 -winx/-winy 未传入（单台投屏居中）。
// 0 是合法的网格位置（第一列/第一行），不能用 0 判断是否传入。
const winPosUnset = -32000

var (
	flagTitle       = flag.String("title", "LgPlayer", "window title")
	flagWebURL      = flag.String("weburl", "", "web view URL (webview mode, skips RTSP)")
	flagIP          = flag.String("ip", "", "device ip address")
	flagVPort       = flag.Int("vport", 0, "rtsp video port")
	flagURL         = flag.String("url", "", "rtsp url (overrides -ip/-vport)")
	flagUser        = flag.String("user", "u001", "rtsp user")
	flagPass        = flag.String("pass", "p001", "rtsp pass")
	flagCPort       = flag.Int("cport", 30004, "control port")
	flagList        = flag.String("list", "", "batch control list (ip:port#ip:port)")
	flagAllList     = flag.String("alllist", "", "matrix all control list including primary (ip:port#ip:port)")
	flagSelf        = flag.String("self", "", "this window control address (ip:port)")
	flagLandscape   = flag.Bool("landscape", false, "landscape mode (default portrait)")
	flagTCP         = flag.Bool("tcp", false, "use TCP for control channel")
	flagRTSPTCP     = flag.Bool("rtsp_tcp", true, "use TCP for RTSP transport (default true)")
	flagNoAudio     = flag.Bool("noaudio", false, "disable audio playback")
	flagStall       = flag.Int("stall", 15, "stall timeout in seconds (0=disabled)")
	flagPlayarg     = flag.String("playarg", "imagelevel#1#platform#1", "extra SETUP arguments (default imagelevel#1#platform#1)")
	flagHeartbeat   = flag.Int("heartbeat", 5, "heartbeat interval in seconds (injected into playarg)")
	flagApiPort     = flag.Int("apiport", 0, "MYTOS API port for SMS etc. (0=disabled)")
	flagMaxRetry    = flag.Int("maxretry", 0, "max reconnect attempts (0=infinite)")
	flagControlled  = flag.Bool("controlled", false, "this window is a controlled slave (no move/resize)")
	flagPrimary     = flag.String("primary", "", "primary controller address (shown in controlled window sidebar)")
	flagScale       = flag.Float64("scale", 0, "matrix window scale factor (0.6~1.0, 0=no scaling)")
	flagNoSidebar   = flag.Bool("nosidebar", false, "disable WebView2 sidebar (saves ~80MB memory per window)")
	flagHwAccel     = flag.Bool("hwaccel", true, "enable GPU hardware acceleration for decoding (default true)")
	flagNotifyPort  = flag.Int("notifyport", 0, "UDP port to notify parent on first frame ready (0=disabled)")
	flagControlPort = flag.Int("control-port", 0, "local control HTTP port (127.0.0.1, 0=disabled; screenshot/close for parent)")

	// 公网矩阵投屏（WebURL 模式专用，独立于本地 RTSP 矩阵）
	flagMatrixListen  = flag.Int("matrixlisten", 0, "matrix UDP listen port (127.0.0.1, 0=disabled)")
	flagMatrixPrimary = flag.Bool("matrixprimary", false, "this instance starts as matrix primary (captures input)")
	flagMatrixTargets = flag.String("matrixtargets", "", "matrix target UDP ports, comma-separated (e.g. 39401,39402)")

	// 公网矩阵 v2：fanout WebSocket 扇出（pipe 分隔的 WS URL 列表）
	flagFanoutURLs = flag.String("fanout", "", "fanout WebSocket URLs for group control (pipe-separated)")

	// 公网矩阵 v3：群控导航按钮 RPA 扇出
	flagGroupRPA = flag.String("grouprpa", "", "group control RPA for nav buttons (baseURL|token|containers)")

	// 公网矩阵 v4：群控输入 HTTP 扇出（触摸/按键事件）
	flagGroupInput = flag.String("groupinput", "", "group control input fan-out (baseURL|token|targets)")

	// 隐藏窗口模式：被控实例后台运行，不显示窗口 UI
	flagHidden = flag.Bool("hidden", false, "run with hidden window (background mode, no UI shown)")

	// 启动延迟（毫秒）：批量投屏时递增以错开 D3D11VA 初始化峰值，避免 GPU 瞬时过载绿屏。
	flagInitDelay = flag.Int("initdelay", 0, "startup delay in ms (batch launch staggering)")

	// 批量投屏：启动时直接定位到网格位置（主程序预计算后传入）。
	// 哨兵 winPosUnset 表示未传（单台居中）。0 是合法位置（第一列/第一行），用哨兵判断。
	flagWinX = flag.Int("winx", winPosUnset, "window initial x (winPosUnset=center)")
	flagWinY = flag.Int("winy", winPosUnset, "window initial y (winPosUnset=center)")
	flagWinW = flag.Int("winw", 0, "window initial width (0=default 464x780)")
	flagWinH = flag.Int("winh", 0, "window initial height (0=default 464x780)")
	// -fill 已废弃（2026-10-28）：保留定义仅为兼容旧主程序传参不崩（flag 包遇
	// 未定义参数直接报错退出），值恒忽略。填满语义已由 letterbox 受限填满取代
	// （2026-10-28 用户拍板）：窗口与视频比例偏差 ≤10% 时轻微拉伸填满不出黑边
	// （单台竖屏壳子 480:798 vs 720:1280 流的 ~7% 拉伸恢复），偏差大（拖宽/
	// 最大化 16:9 等）才 letterbox 居中黑边——无需任何运行时参数，纯几何判定。
	flagFill = flag.Bool("fill", false, "deprecated: ignored, always letterbox")

	// 摄像头推流（由主程序传入 ffmpeg.exe 路径和设备 camera TCP 端口）
	flagCamFFmpeg  = flag.String("cam-ffmpeg", "", "path to ffmpeg.exe for camera push (empty=disabled)")
	flagCamTcpPort = flag.Int("cam-tcp-port", 0, "device camera TCP port for push (0=disabled)")

	// 容器安装上下文（侧边栏上传 APK 安装走 SDK /android/exec，与 MYT 客户端同路径）
	flagContainer = flag.String("container", "", "target android container name for APK install (empty=install disabled)")
	flagSdkHost   = flag.String("sdkhost", "", "device SDK host for APK install (port 8000)")
	flagSdkPwd    = flag.String("sdkpwd", "", "device SDK basic-auth password (empty=no auth)")
	// V2 部署模式：主机无 8000 SDK，安装命令走 81 /and_api/v1/shell/{ip}/{容器}
	// （body {"cmd"}，无鉴权，{code:200} 成功）。默认 false=原 8000 通道。
	flagV2Mode = flag.Bool("v2mode", false, "V2 deploy mode: install via host :81 and_api/v1/shell instead of SDK :8000")

	// tebox 投屏模式（QEMU Android 模拟器：TCP 9901 帧流 + UDP 9905 JSON 事件，
	// 阶段一手动指定实例 id 与端口；空 cast-mode = RTSP 原有路径）
	flagCastMode = flag.String("cast-mode", "", "cast mode: tebox (empty=RTSP legacy)")
	flagCastHost = flag.String("cast-host", "", "tebox-api host")
	flagCastID   = flag.String("cast-id", "", "tebox instance id")
	flagCastPort = flag.Int("cast-port", 9901, "tebox cast TCP port (video)")
	flagCtrlPort = flag.Int("ctrl-port", 9905, "tebox control UDP port")
	// 独立音频 TCP 端口（上游 0360f5e 拆分后的新部署形态）：0=不连（旧部署，
	// 音频走 cast 流内 codec=16）；新部署传 9914（tebox-1=9904，每实例 +10）。
	flagAudioPort = flag.Int("audio-port", 0, "tebox audio TCP port (0=legacy in-stream audio)")
)

func buildRTSPURL() string {
	if *flagURL != "" {
		return *flagURL
	}
	if *flagIP != "" && *flagVPort > 0 {
		return fmt.Sprintf("rtsp://%s:%d/desktop", *flagIP, *flagVPort)
	}
	return ""
}

// parseMatrixTargets parses comma-separated UDP port list for matrix fan-out.
func parseMatrixTargets(s string) []int {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	var ports []int
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		port, err := strconv.Atoi(part)
		if err != nil || port <= 0 || port > 65535 {
			logf("[main] invalid matrix target port: %q\n", part)
			continue
		}
		ports = append(ports, port)
	}
	return ports
}

func main() {
	// 多窗口并发场景（每窗口独立进程）：收紧 Go 堆，降低单进程内存占用。
	// GOGC=50 更积极 GC，GOMEMLIMIT 限制堆上限，避免 50 进程内存累积打爆系统。
	debug.SetGCPercent(50)
	debug.SetMemoryLimit(64 * 1024 * 1024) // 64MB Go 堆上限

	// Top-level recover for panics in platform UI init
	defer func() {
		if r := recover(); r != nil {
			logf("[main] PANIC: %v\n", r)
			debug.PrintStack()
		}
	}()

	flag.Parse()

	// 启动延迟：批量投屏时错开 D3D11VA 初始化峰值（主程序按索引递增 InitDelay）
	if *flagInitDelay > 0 {
		time.Sleep(time.Duration(*flagInitDelay) * time.Millisecond)
	}

	runtime.LockOSThread()

	// Instance ID = vport (unique per go_legacy process)；tebox 模式用实例 id
	instanceID := fmt.Sprintf("%d", *flagVPort)
	if *flagCastMode == CastModeTebox {
		instanceID = *flagCastID
	}
	logInit(instanceID, "logs")

	startTime := time.Now()
	logf("[main] go_legacy starting at %s, args=%v\n", startTime.Format("15:04:05.000"), os.Args)

	// Pre-extract ffdecode dynamic library eagerly so decoder init doesn't wait for file I/O
	tDll := time.Now()
	loadFFDecode()
	logf("[perf] loadFFDecode: %dms\n", time.Since(tDll).Milliseconds())

	app := NewApp()
	app.UseTCP = *flagTCP
	app.RTSPTCP = *flagRTSPTCP
	app.Landscape = *flagLandscape
	app.NoAudio = *flagNoAudio
	app.StallTimeout = *flagStall
	app.Controlled = *flagControlled
	app.SelfAddr = *flagSelf
	app.PrimaryAddr = *flagPrimary
	app.CtrlItems = initCtrlItems(*flagList)
	app.AllCtrlItems = initCtrlItems(*flagAllList)
	if app.Controlled && app.PrimaryAddr == "" && app.SelfAddr != "" {
		app.PrimaryAddr = app.SelfAddr
	}

	// Build playarg string with optional heartbeat
	playarg := *flagPlayarg
	if *flagHeartbeat > 0 {
		if playarg != "" {
			playarg += "#"
		}
		playarg += fmt.Sprintf("heartbeat#%d", *flagHeartbeat)
	}
	app.Playarg = playarg
	// 清晰度初始档与 playarg 的 imagelevel 对齐（UI 选中态反映真实流档位）
	if m := imageLevelRe.FindStringSubmatch(playarg); m != nil {
		if lv, err := strconv.Atoi(m[0][len("imagelevel#"):]); err == nil {
			atomic.StoreInt32(&app.Clarity, int32(lv))
		}
	}
	app.ApiPort = *flagApiPort
	app.MaxRetry = *flagMaxRetry
	app.ContainerName = *flagContainer
	app.SdkHost = *flagSdkHost
	app.SdkPassword = *flagSdkPwd
	app.V2Mode = *flagV2Mode

	// 摄像头推流：主程序传入 WindowsPusher.exe 路径和 camera TCP 端口时启用
	if *flagCamFFmpeg != "" && *flagCamTcpPort > 0 {
		app.CamPusher = NewCameraPusher(*flagCamFFmpeg, *flagIP, *flagCamTcpPort, *flagVPort)
		app.CamPusher.SetApp(app) // 伪源（投屏画面/桌面）抓帧需要宿主引用
		logf("[cam] camera push enabled: ffmpeg=%s port=%d", *flagCamFFmpeg, *flagCamTcpPort)
	}
	app.Controlled = *flagControlled
	app.PrimaryAddr = *flagPrimary
	if app.PrimaryAddr == "" && app.SelfAddr != "" && len(app.CtrlItems) > 0 {
		app.PrimaryAddr = app.SelfAddr
	}
	matrixScale = *flagScale
	app.NoSidebar = *flagNoSidebar
	app.HardwareDecode = *flagHwAccel
	app.NotifyPort = *flagNotifyPort
	app.Hidden = *flagHidden
	app.ControlPort = *flagControlPort

	// 公网矩阵投屏参数（WebURL 模式）
	app.MatrixListenPort = *flagMatrixListen
	app.MatrixPrimary = *flagMatrixPrimary
	app.MatrixTargets = parseMatrixTargets(*flagMatrixTargets)

	// 公网矩阵 v2 fanout + v3 导航按钮 RPA + v4 群控输入
	app.FanoutURLs = parseFanoutURLs(*flagFanoutURLs)
	initGroupRPA(*flagGroupRPA)
	parseGroupInputParam(*flagGroupInput)

	if *flagLandscape {
		app.Rotated.Store(false)
	} else {
		app.Rotated.Store(true)
	}

	// tebox 投屏模式参数 + 源方向：tebox 横屏由 guest 内部旋转触发——QEMU GPU
	// 预旋转后推流分辨率直接变横屏（如 1600x720），线协议不变，客户端唯一可
	// 感知的信号是解码分辨率（上游横屏机制，与 V3 RTSP 流形态一致）。因此
	// -landscape 在 tebox 下被忽略：方向由流分辨率决定，不由参数决定。竖屏
	// （720x1600）大概率先到，固定 Rotated=false 起步；首帧/分辨率变化时由
	// ApplyTeboxOrientation 自适应（tebox_orientation.go，挂在 decodeLoop
	// 出帧路径，帧渲染前生效）。
	if *flagCastMode == CastModeTebox {
		app.Rotated.Store(false)
		app.CastMode = CastModeTebox
		app.CastHost = *flagCastHost
		app.CastID = *flagCastID
		app.CastPort = *flagCastPort
		app.CtrlPort = *flagCtrlPort
		app.AudioPort = *flagAudioPort
		if app.CastHost == "" || app.CastID == "" {
			logln("[main] tebox mode requires -cast-host and -cast-id")
			os.Exit(1)
		}
	}

	// WebURL 模式：WebView2 加载指定 URL，跳过 RTSP 连接
	app.WebURL = *flagWebURL

	switch {
	case app.CastMode == CastModeTebox:
		logf("[main] tebox mode: host=%s id=%s cast=%d ctrl=%d audio=%d\n",
			app.CastHost, app.CastID, app.CastPort, app.CtrlPort, app.AudioPort)
	case app.WebURL != "":
		logf("[main] webview mode: %s\n", app.WebURL)
	default:
		// RTSP 模式
		app.RTSPURL = buildRTSPURL()
		app.User = *flagUser
		app.Pass = *flagPass
		app.CPort = *flagCPort
		if app.RTSPURL == "" {
			logln("[main] no RTSP URL specified, use -url or -ip/-vport")
			os.Exit(1)
		}
		logf("[main] connecting to %s ...\n", app.RTSPURL)
	}

	// 本地控制接口（-control-port > 0 时启用；主程序投屏拉起时注入随机端口）
	if app.ControlPort > 0 {
		startControlServer(app, app.ControlPort)
	}

	// Signal handling for graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		logln("[main] signal received, shutting down...")
		app.hwndForSignal()
	}()

	// Run platform-specific UI (handles renderer + event loop + window management)
	ui := newPlatformUI(app)
	ui.Run()
}

// newPlatformUI is implemented per platform.
func newPlatformUI(app *App) PlatformUI {
	return newPlatformUIImpl(app)
}

// PlatformUI is the interface for platform-specific UI.
type PlatformUI interface {
	Run()
}

// Stub for non-windows, non-darwin (e.g. Linux)
// The real implementations are in ui_windows.go and ui_darwin.go

var _newPlatformUIImpl func(app *App) PlatformUI

func newPlatformUIImpl(app *App) PlatformUI {
	return _newPlatformUIImpl(app)
}

// ─── Simple fallback UI ──────────────────────────────────────────────────────
// SimpleUI 的 Run 与 MessageHost 方法按平台拆分：
//   - linux:  ui_linux.go（X11 真窗口实现）
//   - 其他平台: ui_other.go（无窗口桩，Connect 后阻塞等信号）

type SimpleUI struct {
	app *App
}
