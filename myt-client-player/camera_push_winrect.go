//go:build windows

package main

// camera_push_winrect.go — 扫码伪源 screenWindowRect（窗口矩形桌面画面）：
// 方案 1「合成前桌面」——推流期间对投屏窗口设 WDA_EXCLUDEFROMCAPTURE（窗口从
// 捕获结果消失、屏幕上照常可见），WGC CreateForMonitor 抓显示器合成桌面（已无
// 投屏窗口），每帧按投屏窗口矩形裁剪 → 得到的就是被窗口挡住的真实桌面内容。
// 输出尺寸按窗口矩形比例（不补黑边），ffmpeg rawvideo 管线直写（不 normalize）。
//
// 与 screenWindow（CreateForWindow 抓窗口自己，camera_push_wgc.go）的区别：
// 该源抓的是「窗口底下的桌面」，需要 WDA + 显示器采集 + 矩形换算三者配合。
//
// 限制（v1 已知并接受）：
//   - 会话绑定启动时的显示器，窗口跨屏移动后停帧（提示停推流重开）；
//   - WDA 被系统拒绝时降级继续（画面会含投屏窗口自己，日志告警）；
//   - 窗口 resize 触发 restartVideo（视频路重握手 <1s，状态机支持）。

import (
	"io"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"
)

// ─── WDA（SetWindowDisplayAffinity）───

const (
	wdaNone               = 0x0
	wdaExcludeFromCapture = 0x11 // WDA_EXCLUDEFROMCAPTURE（Win10 2004+）
)

var procSetWindowDisplayAffinity = user32.NewProc("SetWindowDisplayAffinity")

// setWindowDisplayAffinityWda 对窗口设置/恢复捕获排除。
// 返回是否成功；失败（旧系统/策略拒绝）调用方降级继续（日志告警）。
func setWindowDisplayAffinityWda(hwnd uintptr, flag uint32) bool {
	r, _, _ := procSetWindowDisplayAffinity.Call(hwnd, uintptr(flag))
	return r != 0
}

// ─── 显示器与矩形换算 ───

var (
	procMonitorFromWindowNE = user32.NewProc("MonitorFromWindow")
	procGetMonitorInfoNE    = user32.NewProc("GetMonitorInfoW")
	procGetSystemMetricsNE  = user32.NewProc("GetSystemMetrics")
)

const monitorDefaultToNearest = 2 // win32.go 只有 TOPRIMARY，这里补 NEAREST（不占用原常量名）

type monitorRects struct {
	rcMonitor [4]int32 // 整屏矩形（逻辑 px，含负原点——多屏场景）
	rcWork    [4]int32
}

func (m monitorRects) w() int32 { return m.rcMonitor[2] - m.rcMonitor[0] }
func (m monitorRects) h() int32 { return m.rcMonitor[3] - m.rcMonitor[1] }

// monitorOfWindow 取窗口所在显示器矩形（逻辑 px）。取不到返回 false。
func monitorOfWindow(hwnd uintptr) (monitorRects, bool) {
	hmon, _, _ := procMonitorFromWindowNE.Call(hwnd, monitorDefaultToNearest)
	if hmon == 0 {
		return monitorRects{}, false
	}
	// MONITORINFO 布局：cbSize + rcMonitor(4) + rcWork(4) + dwFlags
	var mi struct {
		cbSize    uint32
		rcMonitor [4]int32
		rcWork    [4]int32
		dwFlags   uint32
	}
	mi.cbSize = uint32(unsafe.Sizeof(mi))
	if r, _, _ := procGetMonitorInfoNE.Call(hmon, uintptr(unsafe.Pointer(&mi))); r == 0 {
		return monitorRects{}, false
	}
	return monitorRects{rcMonitor: mi.rcMonitor, rcWork: mi.rcWork}, true
}

// windowRectScreen 窗口矩形（屏幕逻辑 px；不可见时返回 false——最小化窗口
// GetWindowRect 是怪异负坐标，裁剪无意义）。
func windowRectScreen(hwnd uintptr) ([4]int32, bool) {
	if r, _, _ := procIsWindowVisible.Call(hwnd); r == 0 {
		return [4]int32{}, false
	}
	var rc [4]int32
	if r, _, _ := procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&rc))); r == 0 {
		return [4]int32{}, false
	}
	if rc[2]-rc[0] <= 0 || rc[3]-rc[1] <= 0 {
		return [4]int32{}, false
	}
	return rc, true
}

// ─── winrect 会话 ───

// winRectSession 一次 screenWindowRect 推流会话的绑定状态：
// 启动时定格显示器与输出尺寸；帧泵做矩形跟随（移动跟随、resize 上报、跨屏停帧）。
type winRectSession struct {
	hwnd  uintptr
	mon   monitorRects // 绑定的显示器（逻辑矩形）
	outW  int32        // ffmpeg 启动尺寸（定格；resize 触发 restartVideo）
	outH  int32
	physW int32 // 显示器物理像素（帧泵首帧回填，0=未校准用 GetSystemMetrics 近似）
	physH int32

	// resize 检测：窗口逻辑尺寸变化（连续两拍不同 → 上报，重启期间新旧帧泵可能
	// 短暂并存，resizeMu 保护检测字段）
	resizeMu   sync.Mutex
	lastW      int32
	lastH      int32
	resizeFrac int // 已连续多少拍尺寸与上次不同（防抖，≥2 拍上报）
	resizeReq  atomic.Bool
	crossMon   atomic.Bool // 窗口已跨屏（帧泵停帧，提示停重开）
}

// wrSession 当前 screenWindowRect 会话（推流期间存在；单活跃推流无需多实例）。
var wrSessionPtr atomic.Pointer[winRectSession]

// pickWinRectOutput 把窗口逻辑矩形尺寸缩放到目标分辨率上限（长边 ≤1280，
// 与既有伪源清晰度口径一致），不引入补边——输出比例=窗口比例。
func pickWinRectOutput(w, h int32) (int32, int32) {
	if w <= 0 || h <= 0 {
		return 0, 0
	}
	const maxSide = 1280
	if w <= maxSide && h <= maxSide {
		return w, h
	}
	if w >= h {
		return maxSide, (h*maxSide + w/2) / w
	}
	return (w*maxSide + h/2) / h, maxSide
}

// winRectStart 会话启动：设置 WDA + 建 monitor capture session + 定格输出尺寸。
// WDA 失败时降级继续（捕获会含投屏窗口自己）。
// 返回 nil 表示源不可用（窗口无效/显示器拿不到/WGC 不可用）。
func winRectStart(playerHwnd uintptr) *winRectSession {
	if !wgcAvailable() {
		logf("[wgc] winrect: d3d11render.dll Wgc exports unavailable")
		return nil
	}
	if playerHwnd == 0 {
		return nil
	}
	mon, ok := monitorOfWindow(playerHwnd)
	if !ok {
		logf("[wgc] winrect: monitor not found")
		return nil
	}
	rc, ok := windowRectScreen(playerHwnd)
	if !ok {
		logf("[wgc] winrect: player window invalid/minimized")
		return nil
	}
	lw, lh := rc[2]-rc[0], rc[3]-rc[1]
	ow, oh := pickWinRectOutput(lw, lh)
	if ow <= 0 || oh <= 0 {
		return nil
	}
	// WDA：先排除捕获（失败降级继续——画面会含投屏窗口自己，日志说明）
	if !setWindowDisplayAffinityWda(playerHwnd, wdaExcludeFromCapture) {
		logf("[wgc] winrect: SetWindowDisplayAffinity(WDA_EXCLUDEFROMCAPTURE) FAILED — capture will include the player window itself")
	} else {
		logf("[wgc] winrect: WDA_EXCLUDEFROMCAPTURE set (player window hidden from capture)")
	}
	hmon, _, _ := procMonitorFromWindowNE.Call(playerHwnd, monitorDefaultToNearest)
	if hmon == 0 {
		return nil
	}
	if code := wgcInitMonitor(hmon); code != 0 {
		logf("[wgc] winrect: WgcInitMonitor failed: %d", code)
		setWindowDisplayAffinityWda(playerHwnd, wdaNone)
		return nil
	}
	s := &winRectSession{
		hwnd: playerHwnd, mon: mon, outW: ow, outH: oh,
		lastW: lw, lastH: lh,
	}
	wrSessionPtr.Store(s)
	logf("[wgc] winrect: session started hwnd=%x out=%dx%d (resize→restartVideo, cross-monitor→stop frames)", playerHwnd, ow, oh)
	return s
}

// winRectStop 会话停止：恢复 WDA_NONE + 关 WGC session。
func winRectStop() {
	if s := wrSessionPtr.Swap(nil); s != nil {
		if !setWindowDisplayAffinityWda(s.hwnd, wdaNone) {
			logf("[wgc] winrect: restore WDA_NONE failed (window may be gone)")
		}
	}
	wgcShutdownPlayer()
}

// winRectFrameGrab 一次裁剪抓帧：读窗口矩形 → 换算物理像素 → WGC 裁剪回读。
// 返回 (nil,0,0,false) 表示本拍跳过（窗口最小化/无效/暂无新帧/跨屏）；
// 第二返回值为 resize 请求（true = 请求状态机 restartVideo）。
func winRectFrameGrab(hwnd uintptr) (bgra []byte, w, h int32, resizeReq bool) {
	s := wrSessionPtr.Load()
	if s == nil || s.hwnd != hwnd {
		return nil, 0, 0, false
	}
	rc, ok := windowRectScreen(hwnd)
	if !ok {
		return nil, 0, 0, false
	}
	// 跨屏检测（不跟随，停帧提示停推流重开）
	mon, _ := monitorOfWindow(hwnd)
	if mon.rcMonitor != s.mon.rcMonitor {
		if !s.crossMon.Swap(true) {
			logf("[wgc] winrect: window moved to another monitor — stop and restart push to rebind")
		}
		return nil, 0, 0, false
	}
	lw, lh := rc[2]-rc[0], rc[3]-rc[1]

	// resize 防抖：窗口逻辑尺寸连续 2 拍与上次记录不同 → 请求 restartVideo
	s.resizeMu.Lock()
	if lw != s.lastW || lh != s.lastH {
		s.resizeFrac++
		if s.resizeFrac >= 2 {
			s.resizeReq.Store(true)
			s.lastW, s.lastH = lw, lh
			s.resizeFrac = 0
		}
	} else {
		s.resizeFrac = 0
	}
	resize := s.resizeReq.Swap(false)
	s.resizeMu.Unlock()

	// 逻辑 → 物理换算：phys = (logical - monOrigin) * physSize / monLogicalSize。
	// 进程无 DPI 感知时 GetWindowRect 返回虚拟化逻辑坐标，而 WGC 帧是物理像素，
	// 比例 = 帧尺寸 / 显示器逻辑尺寸（mon.w()/mon.h()）。
	mw, mh := int64(mon.w()), int64(mon.h())
	if mw <= 0 || mh <= 0 {
		return nil, 0, 0, false
	}
	s.resizeMu.Lock()
	pw, ph := int64(s.physW), int64(s.physH)
	s.resizeMu.Unlock()
	if pw <= 0 || ph <= 0 {
		// 未校准：帧尺寸查询失败时用 GetSystemMetrics 近似（主屏为主屏时成立）
		fw, fh := wgcMonitorFrameSize()
		if fw > 0 && fh > 0 {
			s.resizeMu.Lock()
			s.physW, s.physH = fw, fh
			s.resizeMu.Unlock()
			pw, ph = int64(fw), int64(fh)
		} else {
			wm, _, _ := procGetSystemMetricsNE.Call(0) // SM_CXSCREEN
			hm, _, _ := procGetSystemMetricsNE.Call(1) // SM_CYSCREEN
			pw, ph = int64(wm), int64(hm)
		}
	}
	if pw <= 0 || ph <= 0 {
		return nil, 0, 0, false
	}
	sx := int(rc[0]-s.mon.rcMonitor[0]) * int(pw) / int(mw)
	sy := int(rc[1]-s.mon.rcMonitor[1]) * int(ph) / int(mh)
	sw := int(lw) * int(pw) / int(mw)
	sh := int(lh) * int(ph) / int(mh)
	if sw <= 0 || sh <= 0 {
		return nil, 0, 0, resize
	}

	outW, outH := int(s.outW), int(s.outH)
	bgra = make([]byte, outW*outH*4)
	if code := wgcGrabFrameRect(bgra, outW, outH, sx, sy, sw, sh); code != 0 {
		return nil, 0, 0, resize
	}
	return bgra, s.outW, s.outH, resize
}

// winRectFramePump screenWindowRect 帧泵：15fps 抓帧（按窗口矩形裁剪，比例=
// 窗口比例）→ normalizeBGRA 缩放/补边到 1280x720 → 写 ffmpeg stdin。
// ⚠️ 输出必须恒 1280x720：设备端相机注入管线按此分辨率建流，非标准尺寸
// 解码帧进不了相机队列（见 camera_push_encoder.go winrect 分支注释）。
// resize 请求经 wrRestartCh 通知 videoLoopEncoder 重启视频路（重定格裁剪会话）。
func (cp *CameraPusher) winRectFramePump(stop <-chan struct{}, stdin io.Writer, wrRestartCh chan<- struct{}) {
	s := wrSessionPtr.Load()
	if s == nil || stdin == nil {
		logf("[cam] winrect pump: no session/stdin")
		return
	}
	logf("[cam] winrect pump started src=%dx%d -> out=%dx%d", s.outW, s.outH, rawPushW, rawPushH)
	ticker := time.NewTicker(time.Second / 15)
	defer ticker.Stop()
	hwnd := s.hwnd
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
		}
		bgra, w, h, resizeReq := winRectFrameGrab(hwnd)
		if resizeReq {
			select {
			case wrRestartCh <- struct{}{}:
			default: // 已有未处理请求，合并
			}
			// resize 后继续照常抓帧（旧尺寸裁剪，重启 <1s 闪断可接受）
		}
		if bgra == nil || w <= 0 || h <= 0 {
			continue
		}
		out := normalizeBGRA(bgra, int(w), int(h), rawPushW, rawPushH)
		if out == nil {
			continue
		}
		if _, err := stdin.Write(out); err != nil {
			logf("[cam] winrect stdin write: %v", err)
			return
		}
	}
}
