//go:build darwin

package main

import (
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const darwinNavBarH = 48
const darwinSideCollapsedW = 52
const darwinSideExpandedW = 300

// ─── Layout ────────────────────────────────────────────────────────────

func (ui *DarwinUI) layoutChildren() {
	ui.mu.Lock()
	w, h := ui.baseW, ui.baseH
	sw := ui.sideW
	fs := ui.fullscreen
	ui.mu.Unlock()

	if fs {
		cLayoutViews(w, h, 0, 0)
		return
	}
	cLayoutViews(w, h, sw, darwinNavBarH)
}

func (ui *DarwinUI) doToggleFullscreen() {
	ui.mu.Lock()
	ui.fullscreen = !ui.fullscreen
	ui.mu.Unlock()

	cToggleFullscreen()
	ui.layoutChildren()
	ui.syncSidebarState()
}

func (ui *DarwinUI) doToggleRotate() {
	ui.app.ToggleRotate()

	ui.mu.Lock()
	// Nav bar always at bottom: videoW = baseW - sideW, videoH = baseH - navBarH
	videoW := ui.baseW - ui.sideW
	videoH := ui.baseH - darwinNavBarH
	ui.baseW = videoH + ui.sideW
	ui.baseH = videoW + darwinNavBarH
	ui.mu.Unlock()

	ui.fitWindowToVideo()
	ui.syncSidebarState()
}

func (ui *DarwinUI) doTogglePanel(expand bool) {
	ui.mu.Lock()
	videoW := ui.baseW - ui.sideW
	videoH := ui.baseH - darwinNavBarH
	ui.expanded = expand
	if expand {
		ui.sideW = darwinSideExpandedW
	} else {
		ui.sideW = darwinSideCollapsedW
	}
	ui.baseW = videoW + ui.sideW
	ui.baseH = videoH + darwinNavBarH
	minW := ui.sideW + 200
	minH := darwinNavBarH + 200
	ui.mu.Unlock()

	if expand {
		cShowSidebarView(1)
	} else {
		cShowSidebarView(0)
	}

	ui.fitWindowToVideo()
	ui.layoutChildren()
	cSetMinWindowSize(minW, minH)

	if expand {
		ui.syncSidebarState()
	}
}

func (ui *DarwinUI) fitWindowToVideo() {
	scaler := ui.app.activeScaler()
	if scaler == nil {
		return
	}
	srcW, srcH := scaler.SourceSize()
	if srcW <= 0 || srcH <= 0 {
		return
	}

	effW, effH := srcW, srcH
	rotated := ui.app.Rotated.Load()
	if rotated && srcW >= srcH {
		effW, effH = srcH, srcW
	}

	ui.mu.Lock()
	sdlW := ui.baseW - ui.sideW
	sdlH := int((int32(sdlW)*effH + effW/2) / effW)
	ui.baseH = int(sdlH) + darwinNavBarH
	ui.mu.Unlock()
	if ren := ui.app.MetalRen.Load(); ren != nil {
		ren.UpdateSize(int32(sdlW), int32(sdlH))
	}

	ratio := float32(effW) / float32(effH)
	cSetAspectRatio(ratio)
	cResizeWindow(ui.baseW, ui.baseH)
	ui.layoutChildren()
}

func (ui *DarwinUI) onFirstFrame() {
	defer func() {
		if r := recover(); r != nil {
			logf("[onFirstFrame] recovered: %v\n", r)
		}
	}()
	ui.fitWindowToVideo()
	cHideConnOverlay()
}

func (ui *DarwinUI) onScreenshot() {
	ui.app.screenshotMu.Lock()
	path := ui.app.screenshotPath
	ui.app.screenshotMu.Unlock()
	if path == "" {
		return
	}
	name := shortenPath(path)
	cShowToast("截图已保存！", name, "打开文件夹")
	go func() {
		time.Sleep(4 * time.Second)
		cHideToast()
	}()
}

func (ui *DarwinUI) doArrangeWindows() {
	ui.doArrangeWindowsImpl(true)
}

// doArrangeWindowsFromBroadcast 收到广播通知时调用，不再广播给其他进程
func (ui *DarwinUI) doArrangeWindowsFromBroadcast() {
	ui.doArrangeWindowsImpl(false)
}

func (ui *DarwinUI) doArrangeWindowsImpl(broadcast bool) {
	pids := findDarwinPlayerPIDs()
	logf("[arrange] found %d player windows\n", len(pids))
	if len(pids) < 2 {
		return
	}

	screenW, screenH := getScreenSize()
	logf("[arrange] screen size: %dx%d\n", screenW, screenH)
	if screenW <= 0 || screenH <= 0 {
		return
	}

	const (
		singleClientW = 464
		singleClientH = 780
		gap           = 4
		marginTop     = 30
		marginSide    = 8
	)

	areaW := screenW - marginSide*2
	areaH := screenH - marginTop
	startX := marginSide
	startY := marginTop

	winW, winH, cols := arrangeScaledLayout(len(pids), areaW, areaH, singleClientW, singleClientH, 0, 0, matrixScale)
	if winW <= 0 || winH <= 0 || cols <= 0 {
		logf("[arrange] layout returned zero: winW=%d winH=%d cols=%d\n", winW, winH, cols)
		return
	}

	rows := (len(pids) + cols - 1) / cols
	gridW := cols*winW + (cols-1)*gap
	gridH := rows*winH + (rows-1)*gap
	originX := startX + (areaW-gridW)/2
	originY := startY + (areaH-gridH)/2
	if originX < startX {
		originX = startX
	}
	if originY < startY {
		originY = startY
	}

	// 找到自己 PID 在列表中的索引，只移动自己的窗口
	myPID := os.Getpid()
	myIdx := -1
	for i, pid := range pids {
		if pid == myPID {
			myIdx = i
			break
		}
	}
	if myIdx < 0 {
		logf("[arrange] my pid=%d not in list %v\n", myPID, pids)
		return
	}

	x := originX + (myIdx%cols)*(winW+gap)
	y := originY + (myIdx/cols)*(winH+gap)
	logf("[arrange] self pid=%d idx=%d -> (%d,%d)\n", myPID, myIdx, x, y)
	cSetWindowPosition(x, y)

	// 只有手动点击排列时才广播给其他进程，收到广播通知的不再广播
	if broadcast {
		broadcastArrange()
	}
}

func findDarwinPlayerPIDs() []int {
	out, err := exec.Command("sh", "-c", "ps -eo pid,args | grep go_legacy | grep -v grep").Output()
	if err != nil {
		logf("[arrange] ps lookup failed: %v\n", err)
		return nil
	}
	type pidVport struct {
		pid   int
		vport int
	}
	vportRe := regexp.MustCompile(`-vport\s+(\d+)`)
	var entries []pidVport
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil || pid <= 0 {
			continue
		}
		vport := 0
		if m := vportRe.FindStringSubmatch(line); len(m) >= 2 {
			vport, _ = strconv.Atoi(m[1])
		}
		entries = append(entries, pidVport{pid, vport})
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].vport < entries[j].vport
	})
	pids := make([]int, len(entries))
	for i, e := range entries {
		pids[i] = e.pid
	}
	return pids
}

func getScreenSize() (int, int) {
	out, err := exec.Command("osascript", "-e",
		`tell application "Finder" to get bounds of window of desktop`).Output()
	if err != nil {
		logf("[arrange] getScreenSize failed: %v\n", err)
		return 0, 0
	}
	parts := strings.Split(strings.TrimSpace(string(out)), ",")
	if len(parts) < 4 {
		logf("[arrange] getScreenSize unexpected output: %q\n", string(out))
		return 0, 0
	}
	w, _ := strconv.Atoi(strings.TrimSpace(parts[2]))
	h, _ := strconv.Atoi(strings.TrimSpace(parts[3]))
	return w, h
}

func shortenPath(path string) string {
	parts := strings.Split(path, "/")
	if len(parts) > 3 {
		return "..." + strings.Join(parts[len(parts)-2:], "/")
	}
	return path
}
