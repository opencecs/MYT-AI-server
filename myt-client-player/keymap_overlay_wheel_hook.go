//go:build windows

package main

// keymap_overlay_wheel_hook.go — 键位编辑会话的低级滚轮钩子。
//
// 为什么需要:覆盖窗是 WS_EX_NOACTIVATE(永不夺焦点),而 WM_MOUSEWHEEL 只发给
// 【焦点窗口】。编辑必经侧栏按钮(WebView2 抢走焦点)→ 之后滚轮全进 WebView2,
// 主窗口收不到;覆盖窗又盖住整个视频区(点标记也不还焦点)→ 不挂钩子的话,
// 整个编辑会话的滚轮缩放全部失效。
//
// 方案:编辑期间挂 WH_MOUSE_LL(低级鼠标钩子在消息产生时拦截,先于焦点窗口
// 路由)——光标落在覆盖窗矩形内且编辑态时转发给 kmOnWheel 并吞掉事件;
// 其余一律 CallNextHookEx 放行。钩子只在 UI 线程消息循环内回调,与既有
// wndproc 同上下文,共享 ui.mu 安全。

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// WH_MOUSE_LL 低级鼠标钩子类型。
const WH_MOUSE_LL = 14

var (
	kmWheelHook   uintptr // 非零 = 钩子已挂
	kmWheelHookCB uintptr // NewCallback 只注册一次,防回调槽泄漏
)

// installKmWheelHook 进入编辑会话时挂低级滚轮钩子。
func (ui *WindowsUI) installKmWheelHook() {
	if kmWheelHook != 0 {
		return
	}
	if kmWheelHookCB == 0 {
		kmWheelHookCB = windows.NewCallback(func(nCode int32, wParam, lParam uintptr) uintptr {
			if nCode < 0 || wParam != WM_MOUSEWHEEL || ui.hKMOverlay == 0 {
				ret, _, _ := procCallNextHookEx.Call(0, uintptr(nCode), wParam, lParam)
				return ret
			}
			// MSLLHOOKSTRUCT:pt 为屏幕坐标;滚轮 delta 在 mouseData 高 16 位
			info := (*struct {
				Pt        struct{ X, Y int32 }
				MouseData uint32
				Flags     uint32
				Time      uint32
				ExtraInfo uintptr
			})(unsafe.Pointer(lParam))
			delta := int32(int16(info.MouseData >> 16))
			if ui.kmForwardWheelToOverlay(delta, info.Pt.X, info.Pt.Y) {
				return 1 // 已消费:吞掉,不再下传焦点窗口(防双重缩放)
			}
			ret, _, _ := procCallNextHookEx.Call(0, uintptr(nCode), wParam, lParam)
			return ret
		})
	}
	h, _, _ := procSetWindowsHookExW.Call(WH_MOUSE_LL, kmWheelHookCB, 0, 0)
	kmWheelHook = h
}

// removeKmWheelHook 退出编辑会话时卸载。
func (ui *WindowsUI) removeKmWheelHook() {
	if kmWheelHook == 0 {
		return
	}
	h := kmWheelHook
	kmWheelHook = 0
	procUnhookWindowsHookEx.Call(h)
}

// kmForwardWheelToOverlay 把屏幕坐标滚轮转发给覆盖层缩放;返回是否已消费。
// 钩子路径(编辑态焦点在 WebView2)与主窗口 handleMouseWheel 路径(焦点在主窗口)
// 共用此助手,条件判定单一来源。
func (ui *WindowsUI) kmForwardWheelToOverlay(zDelta, screenX, screenY int32) bool {
	if zDelta == 0 {
		return false
	}
	ui.mu.Lock()
	editing := ui.kmEditing
	hov := ui.hKMOverlay
	ui.mu.Unlock()
	if !editing || hov == 0 {
		return false
	}
	var rc struct{ Left, Top, Right, Bottom int32 }
	procGetWindowRect.Call(uintptr(hov), uintptr(unsafe.Pointer(&rc)))
	if screenX < rc.Left || screenX >= rc.Right || screenY < rc.Top || screenY >= rc.Bottom {
		return false
	}
	// 遮挡/最小化校验:光标点下的窗口必须是本覆盖窗——被别的窗口盖住或
	// 本窗最小化后,原矩形区域的滚轮属于别人,吞掉会让其他应用滚不动。
	wp := struct{ X, Y int32 }{screenX, screenY}
	hwndPt, _, _ := procWindowFromPoint.Call(uintptr(unsafe.Pointer(&wp)))
	if windows.HWND(hwndPt) != hov {
		return false
	}
	pt := struct{ X, Y int32 }{screenX, screenY}
	procScreenToClient.Call(uintptr(hov), uintptr(unsafe.Pointer(&pt)))
	ui.kmOnWheel(pt.X, pt.Y, zDelta)
	return true
}
