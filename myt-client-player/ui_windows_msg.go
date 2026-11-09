//go:build windows

package main

import (
	"fmt"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

func (ui *WindowsUI) rebuildVideoPipeline() {
	a := ui.app
	logf("[ui] D3D11 device lost, rebuilding renderer+decoder pipeline\n")

	// 1. 关闭旧解码器：decodeLoop 因 Decoder=nil 丢弃缓冲帧（RTSP 继续收流）
	if old := a.Decoder.Swap(nil); old != nil {
		old.Close()
	}
	a.framesDecoded.Store(0)
	a.firstFrameSent.Store(false)
	a.firstFramePresented.Store(false)
	a.texFallbackDone.Store(false)

	// 2. 关闭旧渲染器，建新渲染器（自建新 device）
	if oldD3d := a.D3D11Ren.Swap(nil); oldD3d != nil {
		oldD3d.Close()
	}
	if oldRen := a.Renderer.Swap(nil); oldRen != nil {
		oldRen.Close()
	}

	var rc struct{ Left, Top, Right, Bottom int32 }
	procGetClientRect.Call(uintptr(ui.hSdlArea), uintptr(unsafe.Pointer(&rc)))
	w := rc.Right - rc.Left
	h := rc.Bottom - rc.Top

	if d3d, err := NewD3D11RendererWithDevice(uintptr(ui.hSdlArea), int(w), int(h), a.Rotated.Load(), *flagFill, 0); err == nil {
		a.D3D11Ren.Store(d3d)
		d3d.SetInputHandler(a)
		d3d.SetOnFrame(func() { // 帧到即渲染
			procPostMessageW.Call(uintptr(ui.hParent), WM_APP_RENDER, 0, 0)
		})
		d3d.SetOnDeviceLost(func() { // 再次丢失仍可重建（防重入：重建入口在 UI 线程串行）
			procPostMessageW.Call(uintptr(ui.hParent), WM_APP_D3D11_REBUILD, 0, 0)
		})
		logf("[ui] renderer rebuilt with new device after device lost\n")

		// 3. 新建解码器复用新 renderer 的 device
		go func() {
			dec := NewH264Decoder(a.HardwareDecode)
			if !d3d.IsWarp() {
				if dev := d3d.DeviceHandle(); dev != 0 {
					dec.SetD3D11Device(dev)
				}
			}
			if err := dec.Initialize(); err != nil {
				logf("[ui] rebuild: decoder init failed: %v\n", err)
				return
			}
			if !d3d.CanRenderNV12() {
				dec.SetTextureOutput(false)
			}
			a.Decoder.Store(dec)
		}()
	} else {
		logf("[ui] rebuild renderer failed: %v, falling back to GDI\n", err)
		// GPU 彻底不可用：回退 GDI 渲染。解码器无渲染器 device → 可能硬解输出纹理，
		// 由 Connect 的 SetTextureOutput 降级逻辑兜底（此处仅建渲染器）。
		if ren, err := NewRenderer(uintptr(ui.hSdlArea), int(w), int(h), a.Rotated.Load(), *flagFill); err == nil {
			a.Renderer.Store(ren)
			ren.SetInputHandler(a)
		}
	}
}

// checkFrameStall 已删除（回退旧版行为）："等待画面"提示有两个缺陷——
// 1. 隐藏条件 <200ms 只对 5fps 以上流成立，慢帧率设备（省电模式 1-2s 推一帧）一旦
//    触发提示就永远隐藏不了 → "画面能动能渲染却一直显示等待中"；
// 2. 显示期间每 2s 发 PLI 催帧，打断设备省电节奏，编码器反复出 IDR 过载。
// 无帧恢复改由 stall 看门狗直接断连重连（app_control.go onStallTick，5s 阈值，旧版行为）。

// handleFirstFrame processes WM_APP_FIRST_FRAME.
func (ui *WindowsUI) handleFirstFrame() {
	procKillTimer.Call(uintptr(ui.hParent), TIMER_CONN_TIMEOUT)
	// 定尺寸投屏（Controlled 且主程序传 -winw）：窗口尺寸由主程序网格定，不 fit 改尺寸。
	// 只调 layoutChildren（sdlArea 填满客户区，winw 已含 sideW+navBar，比例无黑边）。
	// 非 controlled（单台手动调）才 fit 到视频比例去黑边。
	// tebox 恢复首帧 fit：按流比例设定一次初始窗口尺寸（消除默认窗口比例黑边）。
	// fit 内部源校验未就绪会空跑（无害），且 teboxInitialFitDone 只在真正执行时
	// 置位，未就绪由下一拍泵兜底补齐；初始 fit 后恒不再 fit（用户拖动不回弹）。
	if !(ui.app.Controlled && *flagWinW > 0) {
		ui.fitWindowToVideo()
	}
	// fit 后强制重算布局：fit 可能被防振荡/工作区 clamp 导致未改尺寸（无 WM_SIZE），
	// 但首帧后 source 尺寸已知，layoutChildren 需重跑（sdlArea 填满无黑边）。
	ui.layoutChildren()
	if d3d := ui.app.D3D11Ren.Load(); d3d != nil {
		// 这里不 Present（会与首帧纹理未就绪竞争闪烁），也不靠固定延迟隐藏 overlay。
		// 首帧上屏由 onFrame→WM_APP_RENDER→Present 完成；overlay 在 WM_APP_RENDER
		// 首次 Present 成功后立即隐藏（见 WM_APP_RENDER 处理），确保画面已上屏再隐藏。
	} else if ren := ui.app.Renderer.Load(); ren != nil {
		ren.RenderFrame()
		// 软解路径：RenderFrame 后立即隐藏 overlay
		if ui.hOverlay != 0 && ui.app.firstFramePresented.CompareAndSwap(false, true) {
			procShowWindow.Call(uintptr(ui.hOverlay), 0)
		}
	}
}

// handleDestroy processes WM_DESTROY.
func (ui *WindowsUI) handleDestroy() {
	ui.app.Shutdown()
	procKillTimer.Call(uintptr(ui.hParent), TIMER_SDL_PUMP)
	procKillTimer.Call(uintptr(ui.hParent), TIMER_INIT_LAYOUT)
	if ui.hOverlay != 0 {
		procKillTimer.Call(uintptr(ui.hOverlay), TIMER_OVERLAY_HIDE)
		procDestroyWindow.Call(uintptr(ui.hOverlay))
	}
	if ui.hToast != 0 {
		procKillTimer.Call(uintptr(ui.hToast), TIMER_TOAST_HIDE)
		procDestroyWindow.Call(uintptr(ui.hToast))
	}
	if ui.hKMOverlay != 0 {
		ui.removeKmWheelHook() // 编辑中退出:全局低级钩子必须随窗口销毁卸载
		procDestroyWindow.Call(uintptr(ui.hKMOverlay))
	}
	for _, h := range ui.gdiObjects() {
		if h != 0 {
			procDeleteObject.Call(h)
		}
	}
	if ui.navMemDC != 0 {
		procSelectObject.Call(ui.navMemDC, ui.navOldBmp)
		procDeleteObject.Call(ui.navMemBmp)
		procDeleteDC.Call(ui.navMemDC)
	}
	if ui.chromium != nil {
		ui.chromium.Release()
	}
	procPostQuitMessage.Call(0)
}

func (ui *WindowsUI) handleScreenshotResult() {
	ui.app.screenshotMu.Lock()
	path := ui.app.screenshotPath
	ui.app.screenshotPath = ""
	ui.app.screenshotMu.Unlock()
	if path == "" {
		return
	}
	fileName := path
	if idx := strings.LastIndex(path, `\`); idx >= 0 {
		fileName = path[idx+1:]
	}
	ui.showToast("截图已保存！", fileName, "打开文件夹", func() {
		exe, _ := windows.UTF16PtrFromString("explorer")
		arg, _ := windows.UTF16PtrFromString(`/select,"` + path + `"`)
		procShellExecuteW.Call(0, 0, uintptr(unsafe.Pointer(exe)), uintptr(unsafe.Pointer(arg)), 0, 1)
	})
}

func (ui *WindowsUI) syncStateToPanel() {
	if ui.hParent == 0 {
		return
	}
	procPostMessageW.Call(uintptr(ui.hParent), WM_APP_SYNC_STATE, 0, 0)
}

func (ui *WindowsUI) doSyncStateToPanel() {
	if ui.chromium == nil {
		return
	}
	js := fmt.Sprintf(`if(window.updateState)updateState(%s)`, ui.app.stateJSON())
	ui.chromium.Eval(js)
	// 首次同步时启动摄像头推流状态轮询
	ui.camPollOnce.Do(func() { ui.pollCameraPushState() })
}

func (ui *WindowsUI) doIPLocUpdate() {
	if ui.chromium == nil {
		return
	}
	ui.mu.Lock()
	js := ui.pendingJS
	ui.pendingJS = ""
	ui.mu.Unlock()
	if js != "" {
		ui.chromium.Eval(js)
	}
}

func (ui *WindowsUI) doSMSResultUpdate() {
	if ui.chromium == nil {
		return
	}
	ui.mu.Lock()
	js := ui.smsResultJS
	ui.smsResultJS = ""
	ui.mu.Unlock()
	if js != "" {
		ui.chromium.Eval(js)
	}
}

// doUploadResultUpdate 上传状态/进度同步到侧边栏（upload-status + 进度条）。
// 高频进度下 uploadJS 每次被覆盖，多余消息取到空串自然合并。
func (ui *WindowsUI) doUploadResultUpdate() {
	if ui.chromium == nil {
		return
	}
	ui.mu.Lock()
	js := ui.uploadJS
	ui.uploadJS = ""
	ui.mu.Unlock()
	if js != "" {
		ui.chromium.Eval(js)
	}
}

func (ui *WindowsUI) doContactResultUpdate() {
	if ui.chromium == nil {
		return
	}
	ui.mu.Lock()
	js := ui.contactResultJS
	ui.contactResultJS = ""
	ui.mu.Unlock()
	if js != "" {
		ui.chromium.Eval(js)
	}
}

// ─── Window arrangement ───────────────────────────────────────────────────────

// matrixScale 矩阵投屏窗口缩放因子（0.6~1.0），由主程序通过 -scale 参数设置
var matrixScale float64 = 0.6

func (ui *WindowsUI) ArrangeWindows() { ui.arrangePlayerWindows() }

type tagRECT struct{ Left, Top, Right, Bottom int32 }

func (ui *WindowsUI) arrangePlayerWindows() {
	var hwnds []uintptr
	cb := syscall.NewCallback(func(hwnd uintptr, lParam uintptr) uintptr {
		var cls [256]uint16
		procGetClassNameW.Call(hwnd, uintptr(unsafe.Pointer(&cls[0])), 255)
		name := syscall.UTF16ToString(cls[:])
		if name == matrixClassName {
			hwnds = append(hwnds, hwnd)
		}
		return 1
	})
	procEnumWindows.Call(cb, 0)
	num := len(hwnds)
	if num < 2 {
		return
	}
	goodCount := 0
	for _, h := range hwnds {
		ret, _, _ := procIsWindow.Call(h)
		if ret != 0 {
			procShowWindow.Call(h, SW_SHOWNORMAL)
			goodCount++
		}
	}
	if goodCount < 2 {
		return
	}

	// 统一排列算法（与主程序 ComputeGridLayout 一致，照旧客户端）：
	//   cols = ceil(√n)，rows = ceil(n/cols)
	//   rowHeight = 屏高/rows - 2×margin，windowWidth = rowHeight × videoAspect
	//   宽超屏才以宽算高。extraH 含 navBar(48)+边框。
	// 不用窗口当前尺寸（旧逻辑），重算铺满屏幕。
	const margin = 2.0
	count := goodCount

	// 工作区（排除任务栏）。player 排列贴左上角，用全屏简化（任务栏在底时顶部不受影响）。
	sx, _, _ := procGetSystemMetrics.Call(SM_CXSCREEN)
	sy, _, _ := procGetSystemMetrics.Call(SM_CYSCREEN)
	cx, cy := int(sx), int(sy)
	// 边框（实测第一个窗口，含实际 navBar 缩放后的高度）
	extraW, extraH := 0, 0
	if len(hwnds) > 0 {
		var rcClient, rcWindow tagRECT
		procGetClientRect.Call(hwnds[0], uintptr(unsafe.Pointer(&rcClient)))
		if r, _, _ := procGetWindowRect.Call(hwnds[0], uintptr(unsafe.Pointer(&rcWindow))); r != 0 {
			extraW = int(rcWindow.Right-rcWindow.Left) - int(rcClient.Right-rcClient.Left)
			extraH = int(rcWindow.Bottom-rcWindow.Top) - int(rcClient.Bottom-rcClient.Top) + int(ui.navBarHeight)
			if extraW < 0 { extraW = 0 }
			if extraH < 0 { extraH = 0 }
		}
	}
	sideW := int(ui.sideCollapsedW)

	// 枚举列数选窗口面积最大的布局（与主程序 ComputeGridLayout 一致）。
	bestCols, bestRows := 1, count
	var bestW, bestH float64
	for c := 1; c <= count; c++ {
		r := (count + c - 1) / c
		availW := (float64(cx) - float64(c-1)*margin) / float64(c)
		availH := (float64(cy) - float64(r-1)*margin) / float64(r)
		if availW <= 0 || availH <= 0 {
			continue
		}
		// 双向推算取窄：sdlArea = (winW-sideW-extraW)×(winH-extraH)，比例=videoAspect
		// 从宽推
		sdlW := availW - float64(sideW) - float64(extraW)
		winHfromW, winWfromW := 0.0, 0.0
		if sdlW > 0 {
			winHfromW = sdlW/videoAspect + float64(extraH)
			winWfromW = availW
		}
		// 从高推
		sdlH := availH - float64(extraH)
		winWfromH, winHfromH := 0.0, 0.0
		if sdlH > 0 {
			winWfromH = sdlH*videoAspect + float64(sideW) + float64(extraW)
			winHfromH = availH
		}
		var w, h float64
		if winHfromW > 0 && winHfromW <= availH && winWfromW > 0 {
			w, h = winWfromW, winHfromW
		} else if winWfromH > 0 && winWfromH <= availW && winHfromH > 0 {
			w, h = winWfromH, winHfromH
		} else if winHfromW > 0 && winWfromH > 0 {
			if winWfromW*winHfromW <= winWfromH*winHfromH {
				w, h = winWfromW, winHfromW
			} else {
				w, h = winWfromH, winHfromH
			}
		} else {
			continue
		}
		if w*h > bestW*bestH {
			bestW, bestH = w, h
			bestCols, bestRows = c, r
		}
	}
	winW := int(bestW)
	winH := int(bestH)
	if winW <= 0 || winH <= 0 {
		return
	}

	// 保留 WS_THICKFRAME（可缩放边框）：去掉后窗口无法拖边缘缩放。
	// 边框通过 stepX=winW-extraW 重叠覆盖实现视觉无缝，同时禁用 DWM 阴影 + 圆角方角
	const (
		DWMWA_NCRENDERING_POLICY = 2
		DWMNCRP_DISABLED         = 2
		DWMWA_WINDOW_CORNER_PREF = 33
		DWMWCP_DONOTROUND        = 1
	)
	for _, h := range hwnds {
		if ret, _, _ := procIsWindow.Call(h); ret != 0 {
			ncrp := int32(DWMNCRP_DISABLED)
			procDwmSetWindowAttribute.Call(h, DWMWA_NCRENDERING_POLICY,
				uintptr(unsafe.Pointer(&ncrp)), unsafe.Sizeof(ncrp))
			corner := int32(DWMWCP_DONOTROUND)
			procDwmSetWindowAttribute.Call(h, DWMWA_WINDOW_CORNER_PREF,
				uintptr(unsafe.Pointer(&corner)), unsafe.Sizeof(corner))
		}
	}

	logf("[winsz] arrangePlayerWindows: count=%d winW=%d winH=%d extraW=%d extraH=%d cols=%d rows=%d (枚举面积最大)\n",
		goodCount, winW, winH, extraW, extraH, bestCols, bestRows)

	// 网格定位：x = margin + col×(winW+margin)，y = margin + row×(winH+margin)，贴左上角
	stepX := winW + int(margin)
	stepY := winH + int(margin)

	originX := int(margin)
	originY := int(margin)

	idx := 0
	for _, h := range hwnds {
		ret, _, _ := procIsWindow.Call(h)
		if ret != 0 {
			// 跳过已全屏/最大化的窗口：不能把用户点全屏的窗口拽回网格（否则自动靠左）。
			// 全屏=已移除 WS_OVERLAPPEDWINDOW 样式，最大化=IsZoomed。
			if zr, _, _ := procIsZoomed.Call(h); zr != 0 {
				logf("[winsz] arrange skip maximized hwnd=%x\n", h)
				continue
			}
			if st, _, _ := procGetWindowLongW.Call(h, uintptr(GWL_STYLE)); st&uintptr(WS_OVERLAPPEDWINDOW) == 0 {
				logf("[winsz] arrange skip fullscreen hwnd=%x\n", h)
				continue
			}
			x := uintptr(originX + (idx%bestCols)*stepX)
			y := uintptr(originY + (idx/bestCols)*stepY)
			procSetWindowPos.Call(h, uintptr(HWND_TOPMOST), x, y, uintptr(winW), uintptr(winH), SWP_SHOWWINDOW)
			procSetWindowPos.Call(h, uintptr(HWND_NOTOPMOST), x, y, uintptr(winW), uintptr(winH), SWP_SHOWWINDOW)
			idx++
		}
	}
}