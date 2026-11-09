//go:build windows

package screencast

import (
	"log"
	"sort"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// arrangeMu 串行化批量排版调用（自动轮询与手动滑块可能并发触发，避免互相覆盖）。
var arrangeMu sync.Mutex

func doArrangePlayerWindows(scale float64, manual bool) {
	arrangeMu.Lock()
	defer arrangeMu.Unlock()
	var (
		u32               = windows.NewLazyDLL("user32.dll")
		pEnumWindows      = u32.NewProc("EnumWindows")
		pGetClassNameW    = u32.NewProc("GetClassNameW")
		pIsWindow         = u32.NewProc("IsWindow")
		pIsWindowVisible  = u32.NewProc("IsWindowVisible")
		pShowWindow       = u32.NewProc("ShowWindow")
		pSetWindowPos     = u32.NewProc("SetWindowPos")
		pGetWindowLong    = u32.NewProc("GetWindowLongPtrW")
		pIsZoomed         = u32.NewProc("IsZoomed")
		dwmapi            = windows.NewLazyDLL("dwmapi.dll")
		pDwmSetWindowAttr = dwmapi.NewProc("DwmSetWindowAttribute")
	)

	const (
		SW_SHOWNORMAL       = 1
		WS_OVERLAPPEDWINDOW = 0x00CF0000
	)

	var hwnds []uintptr
	var pids []uint32 // 与 hwnds 平行：每窗所属进程 PID，用于稳定排序
	cb := syscall.NewCallback(func(hwnd uintptr, lParam uintptr) uintptr {
		var cls [256]uint16
		pGetClassNameW.Call(hwnd, uintptr(unsafe.Pointer(&cls[0])), 255)
		name := syscall.UTF16ToString(cls[:])
		// 只排列矩阵投屏窗口（单台投屏已统一走 LgPlayerMatrix，不再产生 LgPlayerLegacy）
		if name == "LgPlayerMatrix" {
			// 跳过已全屏/最大化的窗口：不能把用户点全屏的窗口拽回网格（否则自动靠左）。
			// 全屏=已移除 WS_OVERLAPPEDWINDOW 样式，最大化=IsZoomed。
			if zr, _, _ := pIsZoomed.Call(hwnd); zr != 0 {
				return 1
			}
			if st, _, _ := pGetWindowLong.Call(hwnd, ^uintptr(15)); st&uintptr(WS_OVERLAPPEDWINDOW) == 0 {
				return 1
			}
			var winPID uint32
			procGetWindowThreadProcessId.Call(hwnd, uintptr(unsafe.Pointer(&winPID)))
			vis, _, _ := pIsWindowVisible.Call(hwnd)
			if vis == 0 {
				return 1 // 跳过隐藏窗口（真被控后台实例 -hidden）
			}
			hwnds = append(hwnds, hwnd)
			pids = append(pids, winPID)
		}
		return 1
	})
	pEnumWindows.Call(cb, 0)

	// EnumWindows 按 Z 序返回窗口，而 SetWindowPos(topmost→notopmost) 会改动 Z 序，
	// 导致同一窗口在不同轮排版中落进不同网格格（表现为"第一/第三个来回换位置"）。
	// 按 PID 稳定排序，让每台设备固定占用一个网格格，排版收敛后不再换位。
	if len(hwnds) > 1 {
		order := make([]int, len(hwnds))
		for i := range order {
			order[i] = i
		}
		sort.Slice(order, func(a, b int) bool { return pids[order[a]] < pids[order[b]] })
		newHwnds := make([]uintptr, len(hwnds))
		for oi, src := range order {
			newHwnds[oi] = hwnds[src]
		}
		hwnds = newHwnds
	}

	num := len(hwnds)
	// 单台跳过：保持 go_legacy 启动尺寸/位置，不拽回网格（手动滑块时不改单台）。
	if num < 2 {
		return
	}

	goodCount := 0
	for _, h := range hwnds {
		if ret, _, _ := pIsWindow.Call(h); ret != 0 {
			pShowWindow.Call(h, SW_SHOWNORMAL)
			goodCount++
		}
	}
	if goodCount < 1 {
		return
	}

	// 用已有窗口测量边框（sampleHwnd 取第一个），算网格
	// sampleHwnd 传 0：用 AdjustWindowRect 估算边框（统一标准），不实测个别窗口——
	// 实测会采样到用户拖大/异常的窗口，连累所有窗口按异常尺寸排。
	// sideW 用当前批量投屏的侧边栏宽度（批量启动时设置），winw 含 sideW 无黑边。
	// landscape 用当前会话方向（批量启动时 SetLandscape 设置）：横屏投屏排 1280x720 格。
	layout := ComputeGridLayout(goodCount, scale, 0, GetSideW(), GetLandscape())

	log.Printf("[arrange] count=%d scale=%.2f cols=%d rows=%d cell=%dx%d",
		goodCount, scale, layout.Cols, layout.Rows, int(layout.CellW), int(layout.CellH))

	positions := layout.LayoutPositions()

	const (
		hwndTopmost    = ^uintptr(0)
		hwndNotopmost  = ^uintptr(1)
		swpShowWindow  = 0x0040
		SWP_NOACTIVATE = 0x0010
	)
	const (
		DWMWA_NCRENDERING_POLICY       = 2
		DWMNCRP_DISABLED               = 2
		DWMWA_WINDOW_CORNER_PREFERENCE = 33
		DWMWCP_DONOTROUND              = 1
	)

	idx := 0
	for _, h := range hwnds {
		if ret, _, _ := pIsWindow.Call(h); ret != 0 {
			pos := positions[idx]
			// 保留 WS_THICKFRAME（可缩放边框）：去掉后系统不再响应缩放 hit-test。
			// 禁用 DWM NC 渲染外阴影 + 圆角设方角，进一步消除视觉间隔。
			ncrp := int32(DWMNCRP_DISABLED)
			pDwmSetWindowAttr.Call(h, DWMWA_NCRENDERING_POLICY,
				uintptr(unsafe.Pointer(&ncrp)), unsafe.Sizeof(ncrp))
			corner := int32(DWMWCP_DONOTROUND)
			pDwmSetWindowAttr.Call(h, DWMWA_WINDOW_CORNER_PREFERENCE,
				uintptr(unsafe.Pointer(&corner)), unsafe.Sizeof(corner))

			pSetWindowPos.Call(h, hwndTopmost, uintptr(pos.X), uintptr(pos.Y), uintptr(pos.W), uintptr(pos.H), swpShowWindow)
			pSetWindowPos.Call(h, hwndNotopmost, uintptr(pos.X), uintptr(pos.Y), uintptr(pos.W), uintptr(pos.H), swpShowWindow)
			idx++
			if idx >= len(positions) {
				break
			}
		}
	}
}
