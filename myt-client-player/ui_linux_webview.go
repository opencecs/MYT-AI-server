//go:build linux

package main

// ui_linux_webview.go — WebKitGTK 侧栏面板与消息桥（W4/W5）。
//
// Go→JS：sideEvalJS（webkit_web_view_evaluate_javascript）推 updateState(stateJSON())。
// JS→Go：script-message-received → linuxGoWebMessage → app.dispatchMessage。
// MessageHost 方法实装语义对照：ui_windows_toggles.go（panel/fullscreen）、
// ui_windows_upload.go（upload）、ui_darwin_api.go:59-176（iploc/sms/contact 的
// 设备 HTTP API 同语义移植）、ui_darwin_arrange.go:106（broadcastPrimary）。

/*
#cgo pkg-config: gtk+-3.0 webkit2gtk-4.1

#include <gtk/gtk.h>
#include <webkit2/webkit2.h>

// 跨文件 helper 的本文件副本：cgo preamble 是独立编译单元，static 函数不跨单元
// 链接——前向声明能过编译但链接期 undefined。函数体逐字自 ui_linux.go 复制
//（ui_linux.go 原定义保留，两处 static 各自单元自洽）；依赖闭包核查过：5 个函数
// 只用 GTK/GDK/webkit2gtk API（本文件 include 已覆盖），无对其他 static helper 的引用。
static void myResize(GtkWidget* win, int w, int h) {
    gtk_window_resize(GTK_WINDOW(win), w, h);
}

static int myIsFullscreen(GtkWidget* win) {
    return (gdk_window_get_state(gtk_widget_get_window(win)) & GDK_WINDOW_STATE_FULLSCREEN) ? 1 : 0;
}
static void myFullscreen(GtkWidget* win) { gtk_window_fullscreen(GTK_WINDOW(win)); }
static void myUnfullscreen(GtkWidget* win) { gtk_window_unfullscreen(GTK_WINDOW(win)); }
static void sideEvalJS(GtkWidget* wv, const char* js) {
    webkit_web_view_evaluate_javascript(WEBKIT_WEB_VIEW(wv), js, -1, NULL, NULL, NULL, NULL, NULL);
}

// Go 导出回调（本文件尾部 //export；先声明后引用纪律）。
extern void linuxGoSideLoaded(long long uiId);

// panelResize 侧栏宽度切换（GtkFixed 内绝对定位重排，对照 TogglePanel）。
static void panelResize(GtkWidget* fixed, GtkWidget* video, GtkWidget* nav,
                        GtkWidget* side, GtkWidget* wv, int winW, int winH,
                        int navH, int sideW) {
    gtk_widget_set_size_request(video, winW - sideW, winH - navH);
    gtk_widget_set_size_request(nav, winW - sideW, navH);
    gtk_fixed_move(GTK_FIXED(fixed), video, 0, 0);
    gtk_fixed_move(GTK_FIXED(fixed), nav, 0, winH - navH);
    if (side != NULL && wv != NULL) {
        gtk_widget_set_size_request(wv, sideW, winH);
        gtk_fixed_move(GTK_FIXED(fixed), wv, winW - sideW, 0);
    }
}
*/
import "C"

import (
	"net"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"unsafe"
)

// ─── 消息桥（W5）───────────────────────────────────────────────────────────

// linuxGoWebMessage JS→Go 消息入口（script-message-received 回调，GTK 主线程）。
//
//export linuxGoWebMessage
func linuxGoWebMessage(p C.longlong, msg *C.char) {
	ui := lookupUI(p)
	if ui == nil {
		return
	}
	raw := C.GoString(msg)
	logf("[sidebar] raw=%s\n", raw)
	ui.app.dispatchMessage(raw, ui)
}

// linuxGoSideLoaded 面板加载完成 → 补推状态。
//
//export linuxGoSideLoaded
func linuxGoSideLoaded(p C.longlong) {
	if ui := lookupUI(p); ui != nil {
		ui.syncStateToPanel()
	}
}

// syncStateToPanel Go→JS 状态推送（对照 ui_windows_msg.go:188-197 doSyncStateToPanel）。
func (ui *LinuxUI) syncStateToPanel() {
	if ui.webView == nil {
		return
	}
	js := "if(window.updateState)updateState(" + ui.app.stateJSON() + ")"
	cs := C.CString(js)
	defer C.free(unsafe.Pointer(cs))
	C.sideEvalJS(ui.webView, cs)
}

// evalPanelJS 通用 JS 注入（iploc/sms/contact/upload 错误回推用）。
func (ui *LinuxUI) evalPanelJS(js string) {
	if ui.webView == nil {
		return
	}
	cs := C.CString(js)
	defer C.free(unsafe.Pointer(cs))
	C.sideEvalJS(ui.webView, cs)
}

// ─── MessageHost（W5 实装；语义来源见文件头注释）──────────────────────────

// ClipboardCopy 调设备 API 复制屏幕选中文字到本地剪贴板（对照 DarwinUI.copyOrCutSelection）。
func (ui *LinuxUI) ClipboardCopy() {
	apiAddr := ui.app.DeviceApiAddr()
	if apiAddr == "" {
		return
	}
	if text := ui.app.copySelection(apiAddr); text != "" {
		linuxWriteClipboard(text)
	}
}

// ClipboardCut 调设备 API 剪切屏幕选中文字（同上对照）。
func (ui *LinuxUI) ClipboardCut() {
	apiAddr := ui.app.DeviceApiAddr()
	if apiAddr == "" {
		return
	}
	if text := ui.app.cutSelection(apiAddr); text != "" {
		linuxWriteClipboard(text)
	}
}

func (ui *LinuxUI) ToggleTopmost() { ui.app.ToggleTopmost() }

// ToggleRotate 旋转 + 窗口宽高互换（对照 ui_darwin_layout.go:45-57：视频区宽高互换，
// 保持 nav 高度），并同步面板。
func (ui *LinuxUI) ToggleRotate() {
	ui.app.ToggleRotate()
	ui.mu.Lock()
	videoW := ui.baseW - ui.sideW
	videoH := ui.baseH - linuxNavBarH
	ui.baseW = videoH + ui.sideW
	ui.baseH = videoW + linuxNavBarH
	w, h := ui.baseW, ui.baseH
	ui.mu.Unlock()
	if ui.gtkWin != nil {
		C.myResize(ui.gtkWin, C.int(w), C.int(h))
	}
	ui.syncStateToPanel()
}

// TogglePanel 侧栏展开/收起（对照 ui_windows_toggles.go:168-200：sideW 切换 +
// 窗口宽度跟随 + 布局重排）。
func (ui *LinuxUI) TogglePanel(expand bool) {
	ui.mu.Lock()
	videoW := ui.baseW - ui.sideW
	ui.sideW = int(sideExpandedDefault)
	if !expand {
		ui.sideW = int(sideCollapsedDefault)
	}
	if ui.webView != nil {
		ui.baseW = videoW + ui.sideW
	} else {
		ui.sideW = 0
	}
	w, h := ui.baseW, ui.baseH
	fixed, video, nav, side, wv := ui.gtkWin, ui.videoArea, ui.navArea, ui.sideArea, ui.webView
	ui.mu.Unlock()
	if fixed == nil {
		return
	}
	// 窗口与布局重排（GtkFixed 内绝对定位）
	if ui.gtkWin != nil && video != nil {
		C.myResize(fixed, C.int(w), C.int(h))
		C.panelResize(C.gtk_widget_get_parent(video), video, nav, side, wv,
			C.int(w), C.int(h), C.int(linuxNavBarH), C.int(ui.sideW))
	}
	ui.syncStateToPanel()
}

// ToggleFullscreen 全屏切换（对照 darwin cToggleFullscreen）。
func (ui *LinuxUI) ToggleFullscreen() {
	if ui.gtkWin == nil {
		return
	}
	if C.myIsFullscreen(ui.gtkWin) != 0 {
		C.myUnfullscreen(ui.gtkWin)
	} else {
		C.myFullscreen(ui.gtkWin)
	}
}

// SyncState 状态推送（dispatchMessage 的 panel/speaker_toggle 等命令触发）。
func (ui *LinuxUI) SyncState() { ui.syncStateToPanel() }

// ReadClipboardText xclip 读取（MVP1 已有）。
func (ui *LinuxUI) ReadClipboardText() string {
	out, err := exec.Command("xclip", "-selection", "clipboard", "-o").Output()
	if err != nil {
		return ""
	}
	return string(out)
}

func (ui *LinuxUI) ArrangeWindows() {} // 批量排列：NoSidebar 模式不触发，保持空（方案 §6）

// BroadcastPrimary 主控广播（照 ui_darwin_arrange.go:106-131 同语义：ps 扫描 go_legacy
// 进程向各 -vport 端口 UDP 通知；POSIX 命令 Linux 可用）。
func (ui *LinuxUI) BroadcastPrimary(addr string) { broadcastPrimaryLinux(addr) }

// ListCameras/ListMics/CameraPush* 对齐 darwin 空桩（摄像头推流不在 Linux MVP2 范围）。
func (ui *LinuxUI) ListCameras() []string               { return nil }
func (ui *LinuxUI) ListMics() []string                  { return nil }
func (ui *LinuxUI) StartCameraPush(string, string, string) bool { return false }
func (ui *LinuxUI) StopCameraPush()                     {}
func (ui *LinuxUI) SetSpeakerOff(bool)                  {}
func (ui *LinuxUI) IsSpeakerOff() bool                  { return false }
func (ui *LinuxUI) CameraPushRunning() bool             { return false }

// broadcastPrimaryLinux 主控广播实现（照 ui_darwin_arrange.go:106-131 同语义：
// 扫描本机 go_legacy 进程的 -vport 端口，向各端口 UDP 通知 primary 地址）。
func broadcastPrimaryLinux(addr string) {
	if addr == "" {
		return
	}
	out, err := exec.Command("sh", "-c", "ps -eo pid,args | grep go_legacy | grep -v grep").Output()
	if err != nil {
		return
	}
	vportRe := regexp.MustCompile(`-vport\s+(\d+)`)
	count := 0
	for _, line := range strings.Split(string(out), "\n") {
		m := vportRe.FindStringSubmatch(line)
		if len(m) < 2 {
			continue
		}
		vp, err := strconv.Atoi(m[1])
		if err != nil || vp <= 0 {
			continue
		}
		conn, err := net.DialUDP("udp", nil, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: vp})
		if err != nil {
			continue
		}
		_, _ = conn.Write([]byte("primary:" + addr))
		_ = conn.Close()
		count++
	}
	logf("[linux] broadcast primary=%s to %d players\n", addr, count)
}
