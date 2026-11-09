//go:build windows

package main

// camera_push_wgc.go — 扫码伪源第三源 screenWindow（被遮挡的窗口）：
// d3d11render.dll 的 WGC 导出（D3D11R_WgcInit/GrabFrame/Close）经 purego 绑定；
// CreateForWindow 抓 DWM 合成前内容，被投屏播放器盖住也能拿到完整画面。
//
// 旧 dll（无 Wgc* 导出）时 procWgc* 全 0，wgcAvailable() 返回 false，UI 隐藏该项
// （side_api.js 的 SCREEN_WINDOW_VAL 常量仍保留，旧播放器收到该值按 camSrcCam 回落）。
//
// HWND 选择：自动模式——枚举桌面 z 序中「与播放器窗口相交、可见、非本进程」的
// 顶层窗口，取 z 序最高（最贴近盖住播放器语义）者；找不到返回 0，帧泵停帧。

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"unsafe"

	"github.com/ebitengine/purego"
	"golang.org/x/sys/windows"
)

var (
	wgcOnce    sync.Once
	wgcProcs   wgcProcsT
	wgcLoadErr error
)

type wgcProcsT struct {
	init       uintptr
	grab       uintptr
	close      uintptr
	initMon    uintptr // D3D11R_WgcInitMonitor（新 dll，screenWindowRect 源用）
	frameSize  uintptr // D3D11R_WgcFrameSize
	grabRect   uintptr // D3D11R_WgcGrabFrameRect
}

// loadWgcProcs 惰性解析 dll 的 Wgc* 导出（复用 d3d11_renderer.go 已加载的库）。
// 导出缺失（旧 dll）不报错，仅置 proc=0。
func loadWgcProcs() {
	wgcOnce.Do(func() {
		if err := loadD3D11Render(); err != nil {
			wgcLoadErr = err
			return
		}
		lib, err := findLoadedD3D11Render()
		if err != nil {
			wgcLoadErr = err
			return
		}
		for _, e := range []struct {
			name string
			dst  *uintptr
		}{
			{"D3D11R_WgcInit", &wgcProcs.init},
			{"D3D11R_WgcGrabFrame", &wgcProcs.grab},
			{"D3D11R_WgcClose", &wgcProcs.close},
			// 新 dll 导出（缺失时 wgcLoadErr 非空，screenWindowRect 源不可用）
			{"D3D11R_WgcInitMonitor", &wgcProcs.initMon},
			{"D3D11R_WgcFrameSize", &wgcProcs.frameSize},
			{"D3D11R_WgcGrabFrameRect", &wgcProcs.grabRect},
		} {
			addr, err := windows.GetProcAddress(lib, e.name)
			if err != nil {
				wgcLoadErr = fmt.Errorf("wgc: %s: %w", e.name, err)
				return
			}
			*e.dst = addr
		}
	})
}

// findLoadedD3D11Render 定位 d3d11render.dll 句柄（与 d3d11_renderer.go 的
// 提取路径一致：exe 同目录 → temp）。
func findLoadedD3D11Render() (windows.Handle, error) {
	exePath, _ := os.Executable()
	candidates := []string{filepath.Join(filepath.Dir(exePath), "d3d11render.dll"),
		filepath.Join(os.TempDir(), "d3d11render.dll")}
	var lastErr error
	for _, dllPath := range candidates {
		h, err := windows.LoadLibrary(dllPath)
		if err == nil {
			return h, nil
		}
		lastErr = err
	}
	return 0, lastErr
}

// wgcAvailable WGC 捕获是否可用（dll 有 Wgc* 导出且加载成功）。
func wgcAvailable() bool {
	loadWgcProcs()
	return wgcLoadErr == nil
}

// wgcGrabFrame 对 hwnd 建立（或复用）capture session 并取最新 BGRA 帧。
// 返回 BGRA 顶行在前紧凑帧；无新帧/失败返回 nil（调用方跳过本拍）。
// 当前仅支持一个活跃 session（dll 静态单例），伪源同一时刻只有一个源在跑。
func wgcGrabFrame(hwnd uintptr) (bgra []byte, w, h int32) {
	loadWgcProcs()
	if wgcProcs.init == 0 || wgcProcs.grab == 0 {
		return nil, 0, 0
	}
	wgc.mu.Lock()
	defer wgc.mu.Unlock()
	if wgc.hwnd != hwnd {
		// 换窗口（或首次/上一次 init 失败重试）：先关旧 session
		if wgc.hwnd != 0 || wgc.inited {
			purego.SyscallN(wgcProcs.close)
		}
		ret, _, _ := purego.SyscallN(wgcProcs.init, hwnd)
		wgc.inited = true // init 已被调用（无论成败），下次必须先 close 防叠加
		if int32(ret) != 0 {
			logf("[wgc] init hwnd=%x failed: %d", hwnd, int32(ret))
			wgc.hwnd = 0
			return nil, 0, 0
		}
		wgc.hwnd = hwnd
	}
	// 先试探尺寸：grab 需要调用方提供缓冲。用一次小缓冲探测会失败，
	// 改为直接传 4K 上限缓冲（32MB）——WGC 帧最大也就屏尺寸，够用。
	const maxBuf = 3840 * 2160 * 4
	if cap(wgc.buf) < maxBuf {
		wgc.buf = make([]byte, maxBuf)
	}
	var fw, fh int32
	ret, _, _ := purego.SyscallN(wgcProcs.grab,
		uintptr(unsafe.Pointer(&wgc.buf[0])), uintptr(maxBuf),
		uintptr(unsafe.Pointer(&fw)), uintptr(unsafe.Pointer(&fh)))
	switch int32(ret) {
	case 0:
		if fw <= 0 || fh <= 0 || int(fw)*int(fh)*4 > maxBuf {
			return nil, 0, 0
		}
		out := make([]byte, int(fw)*int(fh)*4)
		copy(out, wgc.buf[:len(out)])
		return out, fw, fh
	case -1: // 暂无新帧
		return nil, 0, 0
	case -3: // 缓冲不足（超大屏）：报告失败，不重试
		logf("[wgc] grab: buffer too small")
		return nil, 0, 0
	default:
		return nil, 0, 0
	}
}

// wgcShutdownPlayer 关闭 wgc session（推流停止时调用，避免占用）。
func wgcShutdownPlayer() {
	loadWgcProcs()
	if wgcProcs.close == 0 {
		return
	}
	wgc.mu.Lock()
	defer wgc.mu.Unlock()
	if wgc.hwnd != 0 || wgc.inited {
		purego.SyscallN(wgcProcs.close)
		wgc.hwnd = 0
		wgc.inited = false
	}
}

// ─── screenWindowRect 源的 monitor 模式（camera_push_winrect.go 调用）───

// wgcInitMonitor 对 hmon 建立 monitor capture session（CreateForMonitor）。
// 返回 dll 错误码（0 成功）。与 wgcGrabFrame 共用 dll 静态单例 session，
// 由 wgc.mu 串行化（winrect 会话独占使用，不与 screenWindow 混跑）。
func wgcInitMonitor(hmon uintptr) int32 {
	loadWgcProcs()
	if wgcProcs.initMon == 0 {
		return -100
	}
	wgc.mu.Lock()
	defer wgc.mu.Unlock()
	if wgc.hwnd != 0 || wgc.inited {
		purego.SyscallN(wgcProcs.close)
	}
	ret, _, _ := purego.SyscallN(wgcProcs.initMon, hmon)
	wgc.inited = true
	if int32(ret) != 0 {
		wgc.hwnd = 0
		return int32(ret)
	}
	wgc.hwnd = 1 // 占位非 0：标识 monitor session 已建（≠ screenWindow 的 hwnd）
	return 0
}

// wgcGrabFrameRect 取最新帧并按帧内像素矩形裁剪/缩放到 dstW x dstH。
// outBuf 容量须 ≥ dstW*dstH*4；返回 dll 错误码（0 成功，-1 暂无新帧）。
func wgcGrabFrameRect(outBuf []byte, dstW, dstH, srcX, srcY, srcW, srcH int) int32 {
	loadWgcProcs()
	if wgcProcs.grabRect == 0 {
		return -100
	}
	wgc.mu.Lock()
	defer wgc.mu.Unlock()
	ret, _, _ := purego.SyscallN(wgcProcs.grabRect,
		uintptr(unsafe.Pointer(&outBuf[0])), uintptr(len(outBuf)),
		uintptr(dstW), uintptr(dstH),
		uintptr(srcX), uintptr(srcY), uintptr(srcW), uintptr(srcH))
	return int32(ret)
}

// wgcMonitorFrameSize 查询当前 session 采集尺寸（监视器模式=显示器物理像素）。
func wgcMonitorFrameSize() (int32, int32) {
	loadWgcProcs()
	if wgcProcs.frameSize == 0 {
		return 0, 0
	}
	wgc.mu.Lock()
	defer wgc.mu.Unlock()
	var fw, fh int32
	ret, _, _ := purego.SyscallN(wgcProcs.frameSize,
		uintptr(unsafe.Pointer(&fw)), uintptr(unsafe.Pointer(&fh)))
	if int32(ret) != 0 {
		return 0, 0
	}
	return fw, fh
}

type wgcState struct {
	mu     sync.Mutex
	hwnd   uintptr // 当前 session 的目标窗口（0=init 失败或未建）
	inited bool    // dll 侧是否建过 session（init 失败后也须 close 复位）
	buf    []byte
}

var wgc wgcState

// findOccludedWindow 自动选「被投屏窗口盖住」的目标：
// 枚举顶层窗口，取与播放器窗口矩形相交、可见、非本进程、有标题的
// 顶层窗口中 z 序最高者（EnumWindows 自顶向下遍历，第一个命中即最高）。
// 找不到返回 0（帧泵停帧并提示）。
func findOccludedWindow(playerHwnd uintptr) uintptr {
	var playerRect tagRECT
	if r, _, _ := procGetWindowRect.Call(playerHwnd, uintptr(unsafe.Pointer(&playerRect))); r == 0 {
		return 0
	}
	selfPID := windows.GetCurrentProcessId()

	type hit struct{ hwnd uintptr }
	var found hit
	cb := syscall.NewCallback(func(hwnd uintptr, lParam uintptr) uintptr {
		if hwnd == playerHwnd {
			return 1 // 继续枚举
		}
		if vis, _, _ := procIsWindowVisible.Call(hwnd); vis == 0 {
			return 1
		}
		// 排除工具窗/无标题窗（二维码通常在正常应用窗口里）
		len_, _, _ := procGetWindowTextLengthW.Call(hwnd)
		if len_ == 0 {
			return 1
		}
		// 排除本进程（播放器自己的子窗口/WebView）
		var pid uint32
		procGetWindowThreadProcessId.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
		if pid == selfPID {
			return 1
		}
		var rc tagRECT
		if r, _, _ := procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&rc))); r == 0 {
			return 1
		}
		// 矩形相交判定
		if rc.Right <= playerRect.Left || rc.Left >= playerRect.Right ||
			rc.Bottom <= playerRect.Top || rc.Bottom >= playerRect.Bottom {
			return 1
		}
		// 排除 cloaked（UWP 挂起等）——简单用 DWM 宽度>0 已隐含，这里不引入 dwmapi
		found.hwnd = hwnd
		return 0 // 停止枚举（z 序最高者已找到）
	})
	procEnumWindows.Call(cb, 0)
	return found.hwnd
}

// 窗口标题长度（wgcFilter 用）
var procGetWindowTextLengthW = user32.NewProc("GetWindowTextLengthW")
var procGetWindowThreadProcessId = user32.NewProc("GetWindowThreadProcessId")
