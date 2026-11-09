//go:build windows

package main

import (
	"time"
	"unsafe"
)

// ui_windows_timer.go — WM_TIMER 处理与窗口自适应（fit/refit）逻辑。
// 从 ui_windows_msg.go 拆出，将定时器驱动的窗口尺寸调整与消息处理分离。

// handleTimer processes WM_TIMER messages.
// tryRefitOnSourceChange re-fits the window when the decoded source resolution
// changes. Polling in the SDL pump (not a PostMessage callback) covers the
// D3D11 renderer-rebuild race: whichever renderer instance fills in its source
// size next, the following tick converges. Returns true if the window was
// resized this tick (caller should skip Present to avoid a flash).
func (ui *WindowsUI) tryRefitOnSourceChange() bool {
	if ui.fillScreen() {
		return false
	}
	// 定尺寸投屏（Controlled 且主程序传 -winw）：窗口尺寸由主程序网格定夺，
	// winw 已含 sideW+navBar+边框，sdlArea 比例=视频比例无黑边，不需 fit 改尺寸。
	// fit 会改窗口尺寸与排版冲突（窗口比格子小→白边/变大变不回）。
	// 公网 WebURL 模式跳过：视频在 WebView2 页面内 object-fit:contain 自适应。
	// tebox 模式：仅允许一次初始 fit（源尺寸首次就绪时按流比例定窗口，消除默认
	// 窗口比例黑边），由 teboxInitialFitDone 收口。此兜底覆盖首帧竞态——首帧消息
	// 先于 SetSourceSize 到达时首帧 fit 空跑，由下一拍泵补齐。初始 fit 完成后
	// 恒跳过（用户拖动不回弹），画面由 letterbox 受限填满自适应。
	if ui.app.CastMode == CastModeTebox && ui.teboxInitialFitDone {
		return false
	}
	if ui.app.Controlled && *flagWinW > 0 {
		return false
	}
	// 矩阵投屏每个窗口也各自 fit 到自己内容比例（主程序只读比例定尺寸），故不跳过。
	scaler := ui.activeScaler()
	var w, h int32
	public := ui.app.WebURL != "" && scaler == nil
	if scaler != nil {
		w, h = scaler.SourceSize()
	} else if public {
		// 公网模式无本地 scaler：用页面汇报的视频尺寸驱动窗口自适应比例
		w, h = ui.webVideoW.Load(), ui.webVideoH.Load()
	} else {
		return false
	}
	r := ui.app.Rotated.Load()
	if w == ui.lastFitSrc.w && h == ui.lastFitSrc.h && r == ui.lastFitSrc.rotated {
		return false
	}
	if w <= 0 || h <= 0 {
		return false
	}
	var done, changed bool
	if public {
		logf("[pubfit] tryRefit public w=%d h=%d rotated=%v scaler=%v\n", w, h, r, scaler != nil)
		done, changed = ui.tryFitPublicWindow()
	} else {
		done, changed = ui.fitWindowToVideo()
	}
	if done {
		ui.lastFitSrc.w, ui.lastFitSrc.h, ui.lastFitSrc.rotated = w, h, r
	}
	return changed
}

// tryRefitOnAspectMismatch 当窗口视频区比例与内容比例偏差超过 0.5% 时重新 fit。
// 覆盖"主程序网格排版强制改了窗口尺寸/比例"的场景（否则视频永远 letterbox 出黑边），
// 依赖 fitWindowToVideo 自带的防振荡(abs32<2)避免与排版互相打架。
func (ui *WindowsUI) tryRefitOnAspectMismatch() bool {
	if ui.fillScreen() {
		return false
	}
	// 批量投屏（Controlled 且主程序传了 -winw 定尺寸）：窗口尺寸由主程序网格定夺，
	// 比例不匹配是正常的（窗口竖屏比例 vs 视频横屏）。不调 fit，避免每 16ms 反复检测
	// 不匹配→调 fit 空转→可能触发重绘闪烁。视频去黑边由 D3D11/WebView2 页面内处理。
	// tebox 模式跳过：这是「拖动弹回」的主凶——用户拖出失配比例后 16ms 内被 fit
	// 拽回；画面由 letterbox 受限填满自适应，无需窗口层面补偿。
	if ui.app.CastMode == CastModeTebox {
		return false
	}
	if ui.app.Controlled && *flagWinW > 0 {
		return false
	}
	scaler := ui.activeScaler()
	if scaler == nil {
		return false
	}
	srcW, srcH := scaler.SourceSize()
	if srcW <= 0 || srcH <= 0 {
		return false
	}
	effW, effH := srcW, srcH
	if ui.app.Rotated.Load() {
		effW, effH = srcH, srcW
	}
	var cc struct{ Left, Top, Right, Bottom int32 }
	procGetClientRect.Call(uintptr(ui.hParent), uintptr(unsafe.Pointer(&cc)))
	clientW := cc.Right - cc.Left
	clientH := cc.Bottom - cc.Top
	sideW := ui.sideCollapsedW
	if ui.panelExpanded {
		sideW = ui.sideExpandedW
	}
	sdlW := clientW - sideW
	sdlH := clientH - ui.navBarHeight
	if sdlW <= 0 || sdlH <= 0 {
		return false
	}
	curA := float64(sdlW) / float64(sdlH)
	wantA := float64(effW) / float64(effH)
	if curA > wantA*1.005 || curA < wantA*0.995 {
		_, changed := ui.fitWindowToVideo()
		return changed
	}
	return false
}

func (ui *WindowsUI) handleTimer(wParam uintptr) {
	if wParam == TIMER_INIT_LAYOUT {
		procKillTimer.Call(uintptr(ui.hParent), TIMER_INIT_LAYOUT)
		procKillTimer.Call(uintptr(ui.hParent), TIMER_CONN_TIMEOUT)
		ui.layoutChildren()
		procInvalidateRect.Call(uintptr(ui.hParent), 0, 1)
		procUpdateWindow.Call(uintptr(ui.hParent))
		return
	}
	if wParam == TIMER_SDL_PUMP {
		// 源分辨率变化/就绪，或主程序强制改尺寸导致视频区比例失配时都重新 fit；
		// 若本次改了窗口尺寸，跳过本拍 Present，等下一拍布局落定后再 Present（防绿闪）。
		if !(ui.tryRefitOnSourceChange() || ui.tryRefitOnAspectMismatch()) {
			if d3d := ui.app.D3D11Ren.Load(); d3d != nil {
				d3d.Present()
			}
			if ren := ui.app.Renderer.Load(); ren != nil {
				ren.RenderFrame()
			}
		}
		ui.repositionOverlay()
		// 低频兜底重绘+Present（500ms）：DWM surface 失效/遮挡恢复/跨屏移动后，
		// flip-model 交换链内容可能丢失，而 Present 纯新帧驱动 → 静态画面设备会黑屏
		// 直到下一帧到达。C 侧自带 300ms 节流，正常帧流时零开销。
		if d3d := ui.app.D3D11Ren.Load(); d3d != nil {
			now := time.Now()
			if now.Sub(ui.lastRepresentAt) >= 500*time.Millisecond {
				ui.lastRepresentAt = now
				d3d.Represent()
			}
		}
	}
	if wParam == TIMER_KM_EVAL {
		// 滚轮缩放的状态推送防抖到期:推一次面板状态(kmOnWheel 高频路径合并,
		// 重活绝不在 LL 钩子回调内做——超时摘钩)
		procKillTimer.Call(uintptr(ui.hParent), TIMER_KM_EVAL)
		ui.EvalKeymapUI()
		return
	}
	if wParam == TIMER_CONN_TIMEOUT {
		procKillTimer.Call(uintptr(ui.hParent), TIMER_CONN_TIMEOUT)
	}
}

// rebuildVideoPipeline 处理 D3D11 设备丢失（DEVICE_REMOVED/RESET）：
// 重建渲染器（新 device）→ 重建解码器（复用新 device）恢复渲染。
// RTSP 连接保持不动。设备丢失后旧 device 上的渲染全部静默失败（黑屏），必须重建。
