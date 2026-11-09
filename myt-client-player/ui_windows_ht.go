//go:build windows

package main

import (
	"sync"
	"unsafe"
)

// htDiagOnce 限制诊断日志只打一次，避免 WM_NCHITTEST 高频触发时刷屏
var htDiagOnce sync.Once

// edgeHitTest 在窗口无边框（WS_THICKFRAME 被主程序排版去除）的情况下，
// 仍允许用户拖动窗口边缘进行缩放：把落在窗口边缘 5px 内的客户区鼠标
// 重新映射为系统缩放 hit-test（HTLEFT/HTRIGHT/HTTOP/HTBOTTOM 及四角）。
//
// 用法：WM_NCHITTEST 先调 DefWindowProc 拿默认结果，
// 仅当默认结果是 HTCLIENT（落在客户区）时才进入边缘判定，
// 标题栏拖动、关闭/最小化/最大化按钮等默认行为不受影响。
//
// 选型说明（方案 1）：顶部边缘 5px 优先返回 HTTOP，牺牲顶部 5px 的
// 标题栏拖动，换取四边四角均可缩放，最贴合"可放大缩小"诉求。
func (ui *WindowsUI) edgeHitTest(lParam uintptr) uintptr {
	// 鼠标屏幕坐标
	x := int32(int16(lParam & 0xFFFF))
	y := int32(int16((lParam >> 16) & 0xFFFF))

	// 窗口屏幕矩形
	var rc struct{ Left, Top, Right, Bottom int32 }
	if r, _, _ := procGetWindowRect.Call(uintptr(ui.hParent), uintptr(unsafe.Pointer(&rc))); r == 0 {
		return HTCLIENT
	}

	// 边缘判定宽度：取系统可缩放边框厚度，DPI 感知；最小 5px 兜底
	cx, _, _ := procGetSystemMetrics.Call(SM_CXSIZEFRAME)
	cy, _, _ := procGetSystemMetrics.Call(SM_CYSIZEFRAME)
	edgeX := int32(cx)
	if edgeX < 5 {
		edgeX = 5
	}
	edgeY := int32(cy)
	if edgeY < 5 {
		edgeY = 5
	}

	// 鼠标相对窗口左/右/上/下的偏移
	dxLeft := x - rc.Left
	dxRight := rc.Right - x
	dyTop := y - rc.Top
	dyBottom := rc.Bottom - y

	// 是否落在某条边
	onLeft := dxLeft < edgeX
	onRight := dxRight < edgeX
	onTop := dyTop < edgeY
	onBottom := dyBottom < edgeY

	// 四角优先
	if onTop && onLeft {
		return uintptr(HTTOPLEFT)
	}
	if onTop && onRight {
		return uintptr(HTTOPRIGHT)
	}
	if onBottom && onLeft {
		return uintptr(HTBOTTOMLEFT)
	}
	if onBottom && onRight {
		return uintptr(HTBOTTOMRIGHT)
	}
	// 四边
	if onLeft {
		return uintptr(HTLEFT)
	}
	if onRight {
		return uintptr(HTRIGHT)
	}
	if onTop {
		return uintptr(HTTOP)
	}
	if onBottom {
		return uintptr(HTBOTTOM)
	}
	return HTCLIENT
}

// nchHitTest 包装为 syscall 回调可直接调用的形式（保留 DefWindowProc 兜底）
func (ui *WindowsUI) nchHitTest(lParam uintptr) uintptr {
	// 先交给系统：标题栏→HTCAPTION、系统按钮、客户区→HTCLIENT 等都正常返回
	def, _, _ := procDefWindowProcW.Call(uintptr(ui.hParent), WM_NCHITTEST, 0, lParam)
	// 一次性诊断：记录窗口当前样式与 DefWindowProc 返回值，确认去边框后 hit-test 链路
	htDiagOnce.Do(func() {
		style, _, _ := procGetWindowLongW.Call(uintptr(ui.hParent), uintptr(GWL_STYLE))
		var rc struct{ Left, Top, Right, Bottom int32 }
		procGetWindowRect.Call(uintptr(ui.hParent), uintptr(unsafe.Pointer(&rc)))
		logf("[hit-test-diag] first NCHITTEST def=%d style=0x%x rect=(%d,%d,%dx%d) mouse=(%d,%d) thickframe=%v\n",
			int(def), int(style), rc.Left, rc.Top, rc.Right-rc.Left, rc.Bottom-rc.Top,
			int32(int16(lParam&0xFFFF)), int32(int16((lParam>>16)&0xFFFF)), style&uintptr(WS_THICKFRAME) != 0)
	})
	if def != HTCLIENT {
		return def
	}
	// Controlled 批量投屏禁止边缘缩放：窗口尺寸由主程序排列定夺，用户拖边会
	// 导致与排版不一致（拖大缩不回、双 navBar、排列采样异常窗口尺寸连累所有窗口）。
	if ui.app.Controlled {
		return HTCLIENT
	}
	ht := ui.edgeHitTest(lParam)
	if ht != HTCLIENT {
		logf("[hit-test] edge hit x=%d y=%d -> %d\n", int32(int16(lParam&0xFFFF)), int32(int16((lParam>>16)&0xFFFF)), int(ht))
	}
	return ht
}
