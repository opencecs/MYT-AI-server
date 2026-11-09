package main

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync/atomic"
	"time"

	"go_legacy/teboxcast"
)

type App struct {
	connConfig
	connState
	rendererFields
	frameState
	ctrlState
	matrixState
	stallState
	screenshotState
	clipboardState

	// 组件引用（连接/解码/音频/控制通道，不分组）
	RTSP    *RTSPClient
	Ctrl    atomic.Pointer[controlSink] // 控制出口（RTSP: ControlChannel；tebox: TeboxInput）
	Decoder atomic.Pointer[H264Decoder]
	Audio   atomic.Pointer[AudioDecoder]

	// Video 当前视频源（VideoSource 抽象：RTSPSource / TeboxSource，Connect 时赋值）
	Video VideoSource
	// teboxAudio tebox 模式音频回放器（connectTebox 创建，tebox 模式专用）
	teboxAudio atomic.Pointer[teboxAudio]
	// teboxAudioConn tebox 独立音频 TCP 客户端（AudioPort>0 时 connectTebox 创建，
	// 断连时 Close；nil=未启用独立音频，走 cast 流内 codec=16）
	teboxAudioConn atomic.Pointer[teboxcast.AudioClient]
	// teboxIn tebox 输入适配器（tebox 模式连接时创建，Ctrl+V 粘贴用）
	teboxIn atomic.Pointer[TeboxInput]
	// connectGen 连接代际（tebox 异步建连兜底定时器防旧代际误触发重连）
	connectGen atomic.Int64

	// 听筒开关（false=播放设备声音，true=静音）
	SpeakerOff atomic.Bool

	// UI 回调（由 UI 层注入，不分组）
	OnConnStateChange func(state int, retry int)
	OnFirstFrame      func()
	OnSignalShutdown  func()
	OnClaritySwitched func(level int) // 清晰度切换成功（RTSP 重连完成，档位已生效）
	// OnTeboxRefit tebox 方向自适应翻转（竖↔横）后 UI 层窗口 refit 请求
	// （tebox_orientation.go 调用；Windows 实现重置一次性初始 fit 标记，
	// 泵下一拍 tryRefitOnSourceChange 按新方向 refit；未注册=无操作）。
	OnTeboxRefit func()
	// RequestClose 请求关闭窗口（control server POST /close 用；UI 层注入，
	// 实现为 PostMessage(hParent, WM_DESTROY)，与 OnSignalShutdown 同款）。
	RequestClose func()

	// ControlPort 本地控制 HTTP 端口（-control-port，0=不启用）
	ControlPort int

	// UI 状态（UI 层频繁读写，不分组）
	Rotated atomic.Bool
	Topmost atomic.Bool
	Clarity int32
	// claritySwitching 清晰度切换的 RTSP 快速重连防重入标记
	claritySwitching atomic.Bool
	// overlayQuiet 临时静默 UI 层"已连接，等待画面"overlay（清晰度切换重连用：
	// 切换语义=旧帧定格过渡，非初次连接等待，避免 overlay 永不消失——
	// firstFrameSent 已为 true，OnFirstFrame 不会再触发隐藏）
	overlayQuiet atomic.Bool

	// 摄像头推流管理器（由主程序传入 ffmpeg.exe 路径时启用）
	CamPusher *CameraPusher

	// playerHwnd 主窗口句柄（UI 层创建窗口后 Store；screenWindow 源选
	// 「被遮挡窗口」的参照矩形用，见 camera_push_wgc.go）
	playerHwnd atomic.Uintptr

	// Keymap 键位映射引擎（NewApp 中构造，输入入口处拦截）
	Keymap *KeymapEngine
}

func NewApp() *App {
	app := &App{
		RTSP:      NewRTSPClient(),
		Clarity:   2,
		connState: newConnState(),
	}
	app.Keymap = NewKeymapEngine(app)
	return app
}

// storeCtrl 存控制出口（atomic.Pointer 存接口值需取址包装）。
func (a *App) storeCtrl(s controlSink) { a.Ctrl.Store(&s) }

// SetPlayerHwnd / PlayerHwnd 主窗口句柄存取（UI 层创建窗口后调用）。
func (a *App) SetPlayerHwnd(hwnd uintptr) { a.playerHwnd.Store(hwnd) }
func (a *App) PlayerHwnd() uintptr        { return a.playerHwnd.Load() }

func (a *App) Connect() error {
	if a.Connected.Load() {
		return nil
	}

	// Bail out early if the app is shutting down.
	select {
	case <-a.shutdownCh:
		return fmt.Errorf("shutting down")
	default:
	}

	// tebox 模式：独立连接路径（teboxcast 拉流 + UDP JSON 控制），不与 RTSP 共用
	if a.CastMode == CastModeTebox {
		return a.connectTebox()
	}

	// RTSP 模式（原有逻辑）
	host := parseRTSPHost(a.RTSPURL)
	if host == "" {
		return fmt.Errorf("no rtsp url")
	}

	a.rtspHost.Store(&host)
	port := parseRTSPPort(a.RTSPURL)
	a.rtspPort.Store(&port)

	connectStart := time.Now()

	ctrl := NewControlChannel(a.UseTCP)
	ctrl.OnDisconnect = func() {
		a.setConnState(ConnDisconnected, 0)
	}
	t0 := time.Now()
	if err := ctrl.Connect(host, a.CPort); err != nil {
		a.setConnState(ConnError, 0)
		return fmt.Errorf("ctrl connect: %w", err)
	}
	a.storeCtrl(ctrl)
	logf("[perf] ctrl connect: %dms\n", time.Since(t0).Milliseconds())

	a.ctrlMu.Lock()
	ctrlItems := make([]ctrlItem, len(a.CtrlItems))
	copy(ctrlItems, a.CtrlItems)
	a.ctrlMu.Unlock()
	if len(ctrlItems) > 0 {
		applyCtrlList(ctrlItems, ctrl)
	}

	a.setConnState(ConnConnecting, 0)
	a.kicked.Store(false)
	a.setupRTSPCallbacks(connectStart)

	_, decErr := a.initDecoder()
	if decErr != nil {
		logf("[app] decoder init failed: %v (video will not render)\n", decErr)
	}

	t2 := time.Now()

	// Don't attempt RTSP connection if shutdown is already in progress.
	select {
	case <-a.shutdownCh:
		if ctrl := a.Ctrl.Load(); ctrl != nil {
			(*ctrl).Close()
		}
		if d := a.Decoder.Swap(nil); d != nil {
			d.Close()
		}
		return fmt.Errorf("shutting down")
	default:
	}

	// 视频源经 VideoSource 抽象触发连接（错误语义与 RTSPClient.Connect 一致）
	a.Video = NewRTSPSource(a.RTSP, a.RTSPURL, a.User, a.Pass, a.RTSPTCP)
	if err := a.Video.Start(); err != nil {
		if errors.Is(err, ErrSessionKicked) {
			logf("[conn] session kicked by another client (454 in SETUP)\n")
			a.kicked.Store(true)
			a.setConnState(ConnKicked, 0)
		} else {
			a.setConnState(ConnError, 0)
		}
		if ctrl := a.Ctrl.Load(); ctrl != nil {
			(*ctrl).Close()
		}
		if d := a.Decoder.Swap(nil); d != nil {
			d.Close()
		}
		return err
	}
	logf("[perf] rtsp connect: %dms\n", time.Since(t2).Milliseconds())

	a.Connected.Store(true)
	a.setConnState(ConnConnected, 0)
	logf("[perf] total connect: %dms\n", time.Since(connectStart).Milliseconds())

	a.startStallWatchdog()
	gateOn() // 丢包感知帧门控：新会话参考链从 IDR 起步，清历史状态（frame_gate.go）

	go func() {
		// 首帧看护：设备批量投屏高负载时，响应 PLAY 后可能不立即推流。
		// 实测：重连后设备立即推流（首帧 171ms）。连接后 12s 仍无首帧则主动
		// 断连重连（新连接触发设备重新推流，实测恢复快）。设备端 hwcodec 不
		// 处理 RTCP PLI（实机验证），不再发催帧请求。
		deadline := time.Now().Add(12 * time.Second)
		for time.Now().Before(deadline) && !a.firstFrameSent.Load() {
			time.Sleep(250 * time.Millisecond)
		}
		if !a.firstFrameSent.Load() && a.Connected.Load() {
			logf("[app] no first frame after 12s, forcing reconnect\n")
			a.setConnState(ConnDisconnected, 0)
		}
	}()

	return nil
}

// Disconnect stops all connections. Safe to call multiple times.
func (a *App) Disconnect() {
	if !atomic.CompareAndSwapInt32(&a.disconnected, 0, 1) {
		return
	}
	a.Connected.Store(false)
	a.firstFrameSent.Store(false)
	a.firstFramePresented.Store(false) // 重连时重置，让 overlay 重新显示加载提示
	a.framesDecoded.Store(0)
	a.droppedFrames.Store(0)
	a.gpuFrameLogCount.Store(0) // 帧管线日志是会话级的，重连后要重新打（防排障误导）
	decodeLogOnce.Store(false)  // 同上：首帧解码日志重连后重新打一次
	a.audioInitFailed.Store(false)
	a.texFallbackDone.Store(false)
	a.swFallbackDone.Store(false)
	a.stopStallWatchdog()
	gateOff() // 关帧门控（断连：复位污染标志、停 IDR 看门狗）
	a.stopClipboardSync()
	// 断连前抬起键位映射的残留触点(防粘滞)
	if a.Keymap != nil {
		a.Keymap.releaseAllPublic()
	}
	// Close the NALU channel to terminate the decode goroutine
	if a.naluCh != nil {
		close(a.naluCh)
		a.naluCh = nil
	}
	// tebox 模式：停视频源（RTSP 模式仍走下方 a.RTSP.Close()，避免双重关闭）
	if a.CastMode == CastModeTebox {
		if v := a.Video; v != nil {
			v.Stop()
			a.Video = nil
		}
		a.teboxIn.Swap(nil) // 输入适配器经上方 Ctrl.Swap 关闭，这里清引用
		if ac := a.teboxAudioConn.Swap(nil); ac != nil {
			ac.Close() // 独立音频客户端（AudioPort>0 时存在）
		}
		if aud := a.teboxAudio.Swap(nil); aud != nil {
			aud.Close()
		}
	}
	a.RTSP.Close()
	if dec := a.Decoder.Swap(nil); dec != nil {
		dec.Close()
	}
	// 不关渲染器：渲染器跟窗口生命周期（UI 启动创建，窗口关闭销毁），不跟 RTSP 连接。
	// 重连复用渲染器，解码器才能拿到 device 保持硬解。关了重连后没渲染器→软解+慢。
	// 渲染器在 Shutdown/进程退出时由 UI 层 Close。
	// a.closeRenderer()  // 已移除
	if aud := a.Audio.Swap(nil); aud != nil {
		aud.Close()
	}
	if ctrl := a.Ctrl.Swap(nil); ctrl != nil {
		(*ctrl).Close()
	}
	if t, ok := a.httpClient.Transport.(*http.Transport); ok {
		t.CloseIdleConnections()
	}
}

func (a *App) resetDisconnected() { atomic.StoreInt32(&a.disconnected, 0) }

// notifyReady 首帧就绪后向父进程的 notifyport 发 UDP 通知。
// 父进程批量启动投屏时等此通知再启动下一个，错开 GPU 初始化峰值
// （12 窗口同时创建 D3D11 设备/硬解器会打爆 GPU → 访问违例崩溃）。
func (a *App) notifyReady() {
	if a.NotifyPort <= 0 {
		return
	}
	go func() {
		addr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("127.0.0.1:%d", a.NotifyPort))
		if err != nil {
			return
		}
		conn, err := net.DialUDP("udp", nil, addr)
		if err != nil {
			return
		}
		defer conn.Close()
		conn.SetWriteDeadline(time.Now().Add(500 * time.Millisecond))
		conn.Write([]byte("firstframe"))
	}()
}

func (a *App) hwndForSignal() {
	if a.OnSignalShutdown != nil {
		a.OnSignalShutdown()
	}
}

func (a *App) Shutdown() {
	a.shutdownOnce.Do(func() {
		close(a.shutdownCh)
	})
	// 停止摄像头推流（避免窗口关闭后推流进程残留）
	if a.CamPusher != nil {
		a.CamPusher.Stop()
	}
	a.Disconnect()
}

// startStallWatchdog/stopStallWatchdog/DeviceHost/DeviceCtrlPort/DeviceApiAddr/
// CanControlInput/SetPrimaryAddr/SendCmd/SendKeyboard/SendText/SetClarity/
// ToggleRotate/saveScreenshot/ToggleTopmost/OnMouseEvent/OnKeyboardEvent
// 已拆分到 app_control.go

// initDecoder 创建并初始化 H264 解码器（RTSP/tebox 两种视频源共用）：
// 优先复用渲染器 device 同设备解码（mpv/VLC 标准方案，根治跨设备拷贝），
// 初始化超时 10s，按渲染能力降级纹理输出（无 D3D11.3 PlaneSlice/GDI 回退时输出 BGRA）。
func (a *App) initDecoder() (*H264Decoder, error) {
	dec := NewH264Decoder(a.HardwareDecode)
	a.framesDecoded.Store(0)

	// 须在 Initialize 前调用；解码器内部用 av_hwdevice_ctx_alloc+手动设 hwctx->device。
	if d3d := a.D3D11Ren.Load(); d3d != nil {
		if d3d.IsWarp() {
			logf("[app] renderer is WARP (no GPU), decoder will use software decode (no D3D11VA)\n")
		} else if dev := d3d.DeviceHandle(); dev != 0 {
			logf("[app] set decoder device from renderer: 0x%x\n", dev)
			dec.SetD3D11Device(dev)
		}
	}

	var decErr error
	decReady := make(chan struct{})
	go func() {
		t1 := time.Now()
		decErr = dec.Initialize()
		logf("[perf] decoder init: %dms\n", time.Since(t1).Milliseconds())
		close(decReady)
	}()
	a.Decoder.Store(dec)

	select {
	case <-decReady:
	case <-time.After(10 * time.Second):
		return dec, fmt.Errorf("decoder init timeout (10s)")
	}
	if decErr != nil {
		return dec, decErr
	}

	// 渲染器无 NV12 SRV 能力（无 D3D11.3 PlaneSlice）或 GDI 回退时，解码器直接输出
	// BGRA，避免纹理渲染永远失败 → GPU 模式黑屏（旧驱动/Win8 场景）。
	if d3d := a.D3D11Ren.Load(); d3d == nil {
		dec.SetTextureOutput(false)
		logf("[app] GDI fallback mode, decoder outputs BGRA\n")
	} else if !d3d.CanRenderNV12() {
		dec.SetTextureOutput(false)
		logf("[app] renderer lacks NV12 SRV support, decoder outputs BGRA (hardware decode still on)\n")
	}
	// 同设备方案：解码器已用渲染器 device，直接渲染（设备一致性由
	// C 侧 CopyTexture 的设备检查兑底保障）。
	return dec, nil
}
