//go:build windows

package screencast

import "fmt"

// buildArgs 构造 go_legacy.exe 命令行参数（WebURL 公网模式 + RTSP 本地模式 + tebox 模式）
func (w *ProjectionWindow) buildArgs() []string {
	// tebox 模式（QEMU Android 模拟器：TCP 帧流 + UDP JSON 事件，见 tebox.go）
	if w.config.CastMode == "tebox" {
		args := []string{
			"-cast-mode", "tebox",
			"-cast-host", w.config.CastHost,
			"-cast-id", w.config.CastID,
			"-cast-port", fmt.Sprintf("%d", w.config.CastPort),
			"-ctrl-port", fmt.Sprintf("%d", w.config.CastCtrlPort),
		}
		// 独立音频 TCP 端口（上游 0360f5e 拆分后的新部署）：0=不传（旧部署，
		// 音频走 cast 流内 codec=16；tebox-1=9904 / coloros-1=9914，每实例 +10）
		if w.config.CastAudioPort > 0 {
			args = append(args, "-audio-port", fmt.Sprintf("%d", w.config.CastAudioPort))
		}
		if w.config.Title != "" {
			args = append(args, "-title", w.config.Title)
		}
		if w.config.InitDelay > 0 {
			args = append(args, "-initdelay", fmt.Sprintf("%d", w.config.InitDelay))
		}
		// GPU 硬件加速：默认 true（硬解），关闭后传 -hwaccel=false 走 CPU 软解
		if !w.config.GpuAccel {
			args = append(args, "-hwaccel=false")
		}
		// 窗口尺寸：WinW>0 表示定尺寸投屏，传给 go_legacy 固定窗口大小
		if w.config.WinW > 0 {
			args = append(args, "-winw", fmt.Sprintf("%d", w.config.WinW))
			args = append(args, "-winh", fmt.Sprintf("%d", w.config.WinH))
		}
		// 窗口位置：哨兵 WinPosUnset = 未设（居中）。0 是合法位置必须保留。
		if w.config.WinX != WinPosUnset {
			args = append(args, "-winx", fmt.Sprintf("%d", w.config.WinX))
		}
		if w.config.WinY != WinPosUnset {
			args = append(args, "-winy", fmt.Sprintf("%d", w.config.WinY))
		}
		if w.config.NotifyPort > 0 {
			args = append(args, "-notifyport", fmt.Sprintf("%d", w.config.NotifyPort))
		}
		// 本地控制接口（screenshot_window / close_screencast 工具用；0=不追加）
		if w.controlPort > 0 {
			args = append(args, "-control-port", fmt.Sprintf("%d", w.controlPort))
		}
		return args
	}

	// WebURL 模式（公网投屏）
	if w.config.WebURL != "" {
		args := []string{
			"-weburl", w.config.WebURL,
		}
		if w.config.Title != "" {
			args = append(args, "-title", w.config.Title)
		}
		if w.config.Controlled {
			args = append(args, "-controlled")
		}
		if w.config.Scale > 0 {
			args = append(args, "-scale", fmt.Sprintf("%.2f", w.config.Scale))
		}
		if w.config.InitDelay > 0 {
			args = append(args, "-initdelay", fmt.Sprintf("%d", w.config.InitDelay))
		}
		if w.config.Landscape {
			args = append(args, "-landscape")
		}
		// 公网矩阵输入扇出角色参数
		if w.config.MatrixListen > 0 {
			args = append(args, "-matrixlisten", fmt.Sprintf("%d", w.config.MatrixListen))
		}
		if w.config.MatrixPrimary {
			args = append(args, "-matrixprimary")
		}
		if w.config.MatrixTargets != "" {
			args = append(args, "-matrixtargets", w.config.MatrixTargets)
		}
		// 公网矩阵 v2 fanout：主控页面内多 WebSocket 扇出
		if w.config.FanoutURLs != "" {
			args = append(args, "-fanout", w.config.FanoutURLs)
		}
		// 公网矩阵导航按钮群控 RPA
		if w.config.GroupRPA != "" {
			args = append(args, "-grouprpa", w.config.GroupRPA)
		}
		// 公网矩阵 v4：群控输入 HTTP 扇出
		if w.config.GroupInput != "" {
			args = append(args, "-groupinput", w.config.GroupInput)
		}
		// 隐藏窗口模式（被控后台运行）
		if w.config.Hidden {
			args = append(args, "-hidden")
		}
		// 窗口尺寸：WinW>0 表示定尺寸投屏（批量/单台统一走矩阵窗口），传给 go_legacy 固定窗口大小。
		if w.config.WinW > 0 {
			args = append(args, "-winw", fmt.Sprintf("%d", w.config.WinW))
			args = append(args, "-winh", fmt.Sprintf("%d", w.config.WinH))
		}
		// 窗口位置：用哨兵 WinPosUnset 判断未设（单台/不排列时居中）。0 是合法位置必须保留。
		if w.config.WinX != WinPosUnset {
			args = append(args, "-winx", fmt.Sprintf("%d", w.config.WinX))
		}
		if w.config.WinY != WinPosUnset {
			args = append(args, "-winy", fmt.Sprintf("%d", w.config.WinY))
		}
		if w.config.NotifyPort > 0 {
			args = append(args, "-notifyport", fmt.Sprintf("%d", w.config.NotifyPort))
		}
		return args
	}

	// RTSP 模式（原有逻辑）
	// 不传 -playarg 和 -heartbeat：go_legacy 内部默认拼接 imagelevel#1#platform#1#heartbeat#5，
	// 与老客户端 player.exe 行为一致。设备端需要收到 playarg（含 heartbeat）才主动推帧。
	// 传 -rtsp_tcp：TCP 传输稳定，避免 UDP 3s 初始超时触发协议切换。
	args := []string{
		"-ip", w.config.DeviceIP,
		"-vport", fmt.Sprintf("%d", w.config.VideoPort),
		"-cport", fmt.Sprintf("%d", w.config.ControlPort),
		"-rtsp_tcp",
	}
	// 容器安装上下文：侧边栏上传 APK 安装走 SDK /android/exec（与客户端 UploadToCloud 同路径）。
	// 缺容器名时播放器禁用安装（仅上传），不影响其他功能。
	if w.config.ContainerName != "" {
		args = append(args, "-container", w.config.ContainerName)
	}
	if w.config.SdkHost != "" {
		args = append(args, "-sdkhost", w.config.SdkHost)
	}
	if w.config.SdkPassword != "" {
		args = append(args, "-sdkpwd", w.config.SdkPassword)
	}
	// V2 部署模式：主机无 8000 SDK，安装命令改走 81 /and_api/v1/shell（播放器 sdkExec 分支）
	if w.config.V2Mode {
		args = append(args, "-v2mode")
	}
	if w.config.ApiPort > 0 {
		args = append(args, "-apiport", fmt.Sprintf("%d", w.config.ApiPort))
	}
	if w.config.Title != "" {
		args = append(args, "-title", w.config.Title)
	}
	if w.config.CtrlList != "" {
		args = append(args, "-list", w.config.CtrlList)
	}
	if w.config.AllCtrlList != "" {
		args = append(args, "-alllist", w.config.AllCtrlList)
	}
	if w.config.SelfAddr != "" {
		args = append(args, "-self", w.config.SelfAddr)
	}
	if w.config.Controlled {
		args = append(args, "-controlled")
	}
	if w.config.PrimaryAddr != "" {
		args = append(args, "-primary", w.config.PrimaryAddr)
	}
	if w.config.Scale > 0 {
		args = append(args, "-scale", fmt.Sprintf("%.2f", w.config.Scale))
	}
	if w.config.InitDelay > 0 {
		args = append(args, "-initdelay", fmt.Sprintf("%d", w.config.InitDelay))
	}
	if w.config.NoSidebar {
		args = append(args, "-nosidebar")
	}
	// GPU 硬件加速：默认 true（硬解），关闭后传 -hwaccel=false 走 CPU 软解
	if !w.config.GpuAccel {
		args = append(args, "-hwaccel=false")
	}
	if w.config.Landscape {
		args = append(args, "-landscape")
	}
	// 窗口尺寸：WinW>0 表示定尺寸投屏（批量/单台统一走矩阵窗口），传给 go_legacy 固定窗口大小，
	// 避免启动后 fitWindowToVideo 撑大与主程序排列互相拉扯（即“超级大” bug）。
	if w.config.WinW > 0 {
		args = append(args, "-winw", fmt.Sprintf("%d", w.config.WinW))
		args = append(args, "-winh", fmt.Sprintf("%d", w.config.WinH))
	}
	// 画面拉伸填满：cfg.Fill=true 才传。-fill 已废弃且播放器恒忽略——播放器
	// 侧已改为 letterbox 受限填满（2026-10-28）：比例偏差 ≤10% 自动轻微拉伸
	// 填满（单台竖屏壳子对齐流比例，无黑边），偏差大才 letterbox 黑边，无需
	// 主程序传参。字段保留供通用透传。
	if w.config.Fill {
		args = append(args, "-fill")
	}
	// 窗口位置：用哨兵 WinPosUnset 判断未设（单台/不排列时居中）。0 是合法位置必须保留。
	if w.config.WinX != WinPosUnset {
		args = append(args, "-winx", fmt.Sprintf("%d", w.config.WinX))
	}
	if w.config.WinY != WinPosUnset {
		args = append(args, "-winy", fmt.Sprintf("%d", w.config.WinY))
	}
	if w.config.NotifyPort > 0 {
		args = append(args, "-notifyport", fmt.Sprintf("%d", w.config.NotifyPort))
	}
	// 摄像头推流：传入 ffmpeg.exe 路径和 camera TCP 端口
	if w.config.CamPushExe != "" && w.config.CamTCPPort > 0 {
		args = append(args, "-cam-ffmpeg", w.config.CamPushExe)
		args = append(args, "-cam-tcp-port", fmt.Sprintf("%d", w.config.CamTCPPort))
	}
	// 本地控制接口（screenshot_window / close_screencast 工具用；0=不追加）
	if w.controlPort > 0 {
		args = append(args, "-control-port", fmt.Sprintf("%d", w.controlPort))
	}
	return args
}
