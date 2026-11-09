//go:build windows

package screencast

import (
	"time"

	"golang.org/x/sys/windows"
)

// --- 窗口控制（停止/标题/置顶/退出回调）---

// Stop 请求 go_legacy 优雅退出（WM_CLOSE→WM_DESTROY→Shutdown→释放 D3D11/RTSP），
// 等待最多 3s 让其释放 GPU 资源，超时强杀兑底。
// 实测：Kill 强杀不释放 GPU 资源，马上重启新进程撞脏状态 → 黑闪（关后等很久才不闪）。
// 优雅退出保证 GPU 资源释放完，马上重启不闪。
// ControlPort 播放器本地控制 HTTP 端口（0 = 未分配/未启用）
func (w *ProjectionWindow) ControlPort() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.controlPort
}

// PID 播放器进程 PID（启动失败/已退出返回 0）
func (w *ProjectionWindow) PID() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.cmd != nil && w.cmd.Process != nil {
		return w.cmd.Process.Pid
	}
	return 0
}

func (w *ProjectionWindow) Stop() {
	w.mu.Lock()
	cmd := w.cmd
	w.mu.Unlock()
	if cmd == nil || cmd.Process == nil {
		return
	}

	pid := cmd.Process.Pid
	hwnd := findWindowByPID(pid)
	if hwnd != 0 {
		// 请求优雅关闭：WM_CLOSE → DefWindowProc → DestroyWindow → WM_DESTROY →
		// go_legacy Shutdown → Disconnect → RTSP.Close(TEARDOWN) → D3D11 设备/swapchain 释放
		const WM_CLOSE = 0x0010
		procPostMessageW.Call(hwnd, uintptr(WM_CLOSE), 0, 0)
	} else {
		// 无窗口（进程启动早期/已退出）：直接 Kill 兑底
		cmd.Process.Kill()
		return
	}

	// 等待 go_legacy 优雅退出（资源释放完成），最多 3s
	const gracefulTimeout = 3 * time.Second
	done := make(chan struct{})
	go func() {
		cmd.Wait()
		close(done)
	}()
	select {
	case <-done:
		// 优雅退出完成，GPU 资源已释放
	case <-time.After(gracefulTimeout):
		// 超时未退（僵死）：强杀兑底
		cmd.Process.Kill()
		<-done
	}
}

func (w *ProjectionWindow) UpdateTitle(title string) {
	w.mu.Lock()
	pid := 0
	if w.cmd != nil && w.cmd.Process != nil {
		pid = w.cmd.Process.Pid
	}
	w.mu.Unlock()

	if pid == 0 {
		return
	}

	hwnd := findWindowByPID(pid)
	if hwnd == 0 {
		return
	}

	titlePtr, _ := windows.UTF16PtrFromString(title)
	setWindowText(hwnd, titlePtr)
	w.mu.Lock()
	w.config.Title = title
	w.mu.Unlock()
}

// BringToFront 将投屏窗口置顶显示
func (w *ProjectionWindow) BringToFront() {
	w.mu.Lock()
	pid := 0
	if w.cmd != nil && w.cmd.Process != nil {
		pid = w.cmd.Process.Pid
	}
	w.mu.Unlock()

	if pid == 0 {
		return
	}

	hwnd := findWindowByPID(pid)
	if hwnd == 0 {
		return
	}

	// 最小化状态先恢复，否则 SetForegroundWindow 对最小化窗口无效
	if isIconic(hwnd) {
		showWindow(hwnd, swRestore)
	}

	// 先置顶再取消置顶，实现"提到最前但不锁定"
	setWindowPos(hwnd, hwndTopmost, 0, 0, 0, 0, swpNoMove|swpNoSize)
	setWindowPos(hwnd, hwndNotopmost, 0, 0, 0, 0, swpNoMove|swpNoSize)
	setForegroundWindow(hwnd)
}

const (
	hwndTopmost   = ^uintptr(0) // HWND_TOPMOST = -1
	hwndNotopmost = ^uintptr(1) // HWND_NOTOPMOST = -2
	swpNoMove     = 0x0002      // SWP_NOMOVE
	swpNoSize     = 0x0001      // SWP_NOSIZE
	swRestore     = 9           // SW_RESTORE
)

var (
	moduser32                    = windows.NewLazyDLL("user32.dll")
	modkernel32                  = windows.NewLazyDLL("kernel32.dll")
	procSetWindowTextW           = moduser32.NewProc("SetWindowTextW")
	procSetForegroundWindow      = moduser32.NewProc("SetForegroundWindow")
	procSetWindowPos             = moduser32.NewProc("SetWindowPos")
	procEnumWindows              = moduser32.NewProc("EnumWindows")
	procGetWindowThreadProcessId = moduser32.NewProc("GetWindowThreadProcessId")
	procIsWindowVisible          = moduser32.NewProc("IsWindowVisible")
	procPostMessageW             = moduser32.NewProc("PostMessageW")
	procIsIconic                 = moduser32.NewProc("IsIconic")
	procShowWindow               = moduser32.NewProc("ShowWindow")
)

func setForegroundWindow(hwnd uintptr) bool {
	ret, _, _ := procSetForegroundWindow.Call(hwnd)
	return ret != 0
}

func setWindowPos(hwnd, hWndInsertAfter uintptr, x, y, cx, cy int, flags uint32) bool {
	ret, _, _ := procSetWindowPos.Call(hwnd, hWndInsertAfter,
		uintptr(x), uintptr(y), uintptr(cx), uintptr(cy), uintptr(flags))
	return ret != 0
}

func isIconic(hwnd uintptr) bool {
	ret, _, _ := procIsIconic.Call(hwnd)
	return ret != 0
}

func showWindow(hwnd uintptr, cmdShow int) bool {
	ret, _, _ := procShowWindow.Call(hwnd, uintptr(cmdShow))
	return ret != 0
}

func (w *ProjectionWindow) SetOnExit(fn func(exitCode int, failed bool, cfg *ProjectionConfig)) {
	w.mu.Lock()
	w.onExit = fn
	w.mu.Unlock()
}

func (w *ProjectionWindow) notifyExit(exitCode int, failed bool) {
	select {
	case <-w.done:
		return
	default:
	}
	w.mu.Lock()
	fn := w.onExit
	close(w.done)
	w.mu.Unlock()
	if fn != nil {
		fn(exitCode, failed, &w.config)
	}
}
