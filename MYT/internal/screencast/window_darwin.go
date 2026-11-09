//go:build darwin

package screencast

import (
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// playerKind 区分投屏播放器类型
type playerKind int

const (
	playerNone     playerKind = iota
	playerGoLegacy            // go_legacy（纯 Go + purego）
	playerLegacy              // player（C++ 版）
)

// playerInfo 缓存找到的播放器路径和类型
var (
	playerInfo     playerKind
	playerPath     string
	playerInitOnce sync.Once

	// dataDir 是主程序注入的数据目录（macOS: ~/Library/Application Support/...）。
	// go_legacy 由 ExtractGoLegacy 提取到此目录，findPlayer 优先在此搜索。
	dataDir string
)

// SetDataDir 注入数据目录（由 setup.go 在启动时调用）。
// macOS 上 .app/Contents/MacOS/ 是只读的（签名后不可写），
// go_legacy 和 dylib 必须提取到可写的 dataDir。
func SetDataDir(dir string) { dataDir = dir }

// ProjectionWindow 投屏窗口（macOS 平台，调用 go_legacy 或 player 进程）
type ProjectionWindow struct {
	config ProjectionConfig
	onExit func(exitCode int, failed bool, cfg *ProjectionConfig)
	cmd    *exec.Cmd
	done   chan struct{}
	mu     sync.Mutex
}

func NewProjectionWindow(cfg ProjectionConfig) (*ProjectionWindow, error) {
	findPlayer()
	if playerInfo == playerNone {
		return nil, fmt.Errorf("找不到投屏播放器 (go_legacy)，请确认 embedded/go_legacy 已编译")
	}

	w := &ProjectionWindow{
		config: cfg,
		done:   make(chan struct{}),
	}
	go w.run()
	return w, nil
}

func (w *ProjectionWindow) run() {
	if playerInfo == playerNone {
		w.notifyExit(true)
		return
	}

	var args []string
	if playerInfo == playerGoLegacy {
		args = w.buildGoLegacyArgs()
	} else {
		args = w.buildLegacyArgs()
	}

	w.mu.Lock()
	w.cmd = exec.Command(playerPath, args...)
	w.cmd.Dir = filepath.Dir(playerPath)

	// stderr 重定向到 logs/go_legacy.log（与 Windows 一致，供排查投屏问题）
	if logDir := filepath.Join(filepath.Dir(playerPath), "logs"); os.MkdirAll(logDir, 0o755) == nil {
		if f, err := os.OpenFile(filepath.Join(logDir, "go_legacy.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
			w.cmd.Stderr = f
		}
	}
	w.mu.Unlock()

	log.Printf("[screencast] 启动 %s %v", playerPath, args)

	if err := w.cmd.Start(); err != nil {
		log.Printf("[screencast] 播放器启动失败: %v", err)
		w.notifyExit(true)
		return
	}

	go func() {
		w.cmd.Wait()
		log.Printf("[screencast] 播放器已退出")
		w.notifyExit(true)
	}()
}

// buildGoLegacyArgs 构建 go_legacy 的命令行参数
func (w *ProjectionWindow) buildGoLegacyArgs() []string {
	args := []string{
		"-ip", w.config.DeviceIP,
		"-vport", fmt.Sprintf("%d", w.config.VideoPort),
		"-cport", fmt.Sprintf("%d", w.config.ControlPort),
		"-rtsp_tcp",
		"-playarg", "imagelevel#1#platform#1#heartbeat#5",
	}
	// 容器安装上下文：侧边栏上传 APK 安装走 SDK /android/exec（与客户端 UploadToCloud 同路径）
	if w.config.ContainerName != "" {
		args = append(args, "-container", w.config.ContainerName)
	}
	if w.config.SdkHost != "" {
		args = append(args, "-sdkhost", w.config.SdkHost)
	}
	if w.config.SdkPassword != "" {
		args = append(args, "-sdkpwd", w.config.SdkPassword)
	}
	// V2 部署模式：主机无 8000 SDK，安装命令改走 81 /and_api/v1/shell（播放器 sdkExec 分支）
	if w.config.V2Mode {
		args = append(args, "-v2mode")
	}
	if w.config.ApiPort > 0 {
		args = append(args, "-apiport", fmt.Sprintf("%d", w.config.ApiPort))
	}
	if w.config.Title != "" {
		args = append(args, "-title", w.config.Title)
	}
	if w.config.CtrlList != "" {
		args = append(args, "-list", w.config.CtrlList)
	}
	if w.config.AllCtrlList != "" {
		args = append(args, "-alllist", w.config.AllCtrlList)
	}
	if w.config.SelfAddr != "" {
		args = append(args, "-self", w.config.SelfAddr)
	}
	if w.config.Controlled {
		args = append(args, "-controlled")
	}
	if w.config.PrimaryAddr != "" {
		args = append(args, "-primary", w.config.PrimaryAddr)
	}
	if w.config.Scale > 0 {
		args = append(args, "-scale", fmt.Sprintf("%.2f", w.config.Scale))
	}
	if w.config.Landscape {
		args = append(args, "-landscape")
	}
	return args
}

// buildLegacyArgs 构建 player 的命令行参数
func (w *ProjectionWindow) buildLegacyArgs() []string {
	args := []string{
		"-ip", w.config.DeviceIP,
		"-vport", fmt.Sprintf("%d", w.config.VideoPort),
		"-cport", fmt.Sprintf("%d", w.config.ControlPort),
	}
	if w.config.ApiPort > 0 {
		args = append(args, "-apiport", fmt.Sprintf("%d", w.config.ApiPort))
	}
	if w.config.Title != "" {
		args = append(args, "-term", w.config.Title)
	}
	return args
}

// PID macOS 侧未跟踪播放器进程句柄，恒返 0。
func (w *ProjectionWindow) PID() int { return 0 }

// ControlPort macOS 侧未接入播放器控制接口，恒返 0。
func (w *ProjectionWindow) ControlPort() int { return 0 }

func (w *ProjectionWindow) Stop() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.cmd != nil && w.cmd.Process != nil {
		w.cmd.Process.Kill()
	}
}

// UpdateTitle 实时更新投屏窗口标题（不重启投屏）
// macOS 通过 osascript 调用 AppleScript 设置窗口标题
// 需要辅助功能权限：系统设置 → 隐私与安全性 → 辅助功能
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

	// 通过 osascript 设置进程窗口标题
	script := fmt.Sprintf(
		`tell application "System Events" to tell process id %d to set name of every window whose name is not "" to "%s"`,
		pid, title,
	)
	if err := exec.Command("osascript", "-e", script).Run(); err != nil {
		log.Printf("[screencast] 更新窗口标题失败（可能未授权辅助功能权限）: %v", err)
	}

	w.mu.Lock()
	w.config.Title = title
	w.mu.Unlock()
}

func (w *ProjectionWindow) SetOnExit(fn func(exitCode int, failed bool, cfg *ProjectionConfig)) {
	w.mu.Lock()
	w.onExit = fn
	w.mu.Unlock()
}

// BringToFront 将投屏窗口置顶显示（macOS 通过 osascript 激活进程窗口）
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

	script := fmt.Sprintf(`tell application "System Events" to set frontmost of process id %d to true`, pid)
	if err := exec.Command("osascript", "-e", script).Run(); err != nil {
		log.Printf("[screencast] BringToFront 失败: %v", err)
	}
}

func (w *ProjectionWindow) notifyExit(failed bool) {
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
		fn(-1, failed, &w.config)
	}
}

// ArrangePlayerWindows 通过 UDP 通知所有 go_legacy 进程自己移动窗口到网格位置。
func ArrangePlayerWindows(scale float64, manual bool) {
	// 用 ps 找到所有 go_legacy 进程的 vport
	out, err := exec.Command("sh", "-c", "ps -eo args | grep go_legacy | grep -v grep").Output()
	if err != nil || len(out) == 0 {
		return
	}

	vportRe := regexp.MustCompile(`-vport\s+(\d+)`)
	var vports []int
	for _, line := range strings.Split(string(out), "\n") {
		m := vportRe.FindStringSubmatch(line)
		if len(m) < 2 {
			continue
		}
		vport, err := strconv.Atoi(m[1])
		if err != nil || vport <= 0 {
			continue
		}
		vports = append(vports, vport)
	}
	if len(vports) < 2 {
		return
	}

	// 按 vport 排序，保证排列顺序与 Windows 一致
	sort.Ints(vports)

	// 通过 UDP 广播通知所有 go_legacy 进程执行排列
	const arrangeUdpBase = 49000
	for _, vport := range vports {
		port := arrangeUdpBase + vport%1000
		addr, err := net.ResolveUDPAddr("udp", "127.0.0.1:"+strconv.Itoa(port))
		if err != nil {
			continue
		}
		conn, err := net.DialUDP("udp", nil, addr)
		if err != nil {
			continue
		}
		conn.Write([]byte("arrange\n"))
		conn.Close()
	}
}

// findDarwinPlayerPIDs 通过 ps 命令查找 go_legacy 进程 PID（按 vport 排序）。
func findDarwinPlayerPIDs() []int {
	out, err := exec.Command("sh", "-c", "ps -eo pid,args | grep go_legacy | grep -v grep").Output()
	if err != nil {
		log.Printf("[screencast] ps 查找 go_legacy 失败: %v", err)
		return nil
	}

	type pidVport struct {
		pid   int
		vport int
	}
	var entries []pidVport
	vportRe := regexp.MustCompile(`-vport\s+(\d+)`)
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
		m := vportRe.FindStringSubmatch(line)
		vport := 0
		if len(m) >= 2 {
			vport, _ = strconv.Atoi(m[1])
		}
		entries = append(entries, pidVport{pid, vport})
	}
	// 按 vport 排序
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].vport < entries[j].vport
	})
	pids := make([]int, len(entries))
	for i, e := range entries {
		pids[i] = e.pid
	}
	return pids
}

func darwinScreenSize() (int, int) {
	script := `tell application "Finder" to get bounds of window of desktop`
	out, err := exec.Command("osascript", "-e", script).Output()
	if err != nil {
		log.Printf("[screencast] 获取 macOS 屏幕尺寸失败: %v", err)
		return 0, 0
	}
	parts := strings.Split(strings.TrimSpace(string(out)), ",")
	if len(parts) < 4 {
		return 0, 0
	}
	right, errRight := strconv.Atoi(strings.TrimSpace(parts[2]))
	bottom, errBottom := strconv.Atoi(strings.TrimSpace(parts[3]))
	if errRight != nil || errBottom != nil {
		return 0, 0
	}
	return right, bottom
}

func arrangeScaledLayout(count, areaW, areaH, singleClientW, singleClientH, extraW, extraH int, scale float64) (int, int, int) {
	if singleClientW <= 0 || singleClientH <= 0 {
		singleClientW, singleClientH = 464, 780
	}
	if scale <= 0 {
		scale = 1.0
	}
	if scale < 0.6 {
		scale = 0.6
	}
	if scale > 1.0 {
		scale = 1.0
	}
	singleClientW = int(float64(singleClientW) * scale)
	singleClientH = int(float64(singleClientH) * scale)

	aspect := float64(singleClientW) / float64(singleClientH)
	const gap = 4
	bestW, bestH, bestCols := 0, 0, 1
	bestArea := 0
	for cols := 1; cols <= count; cols++ {
		rows := (count + cols - 1) / cols
		cellW := (areaW - gap*(cols-1)) / cols
		cellH := (areaH - gap*(rows-1)) / rows
		if cellW <= 0 || cellH <= 0 {
			continue
		}

		clientW := cellW - extraW
		clientH := cellH - extraH
		if clientW <= 0 || clientH <= 0 {
			continue
		}

		cw, ch := clientW, clientH
		if float64(cw)/float64(ch) > aspect {
			cw = int(float64(ch) * aspect)
		} else {
			ch = int(float64(cw) / aspect)
		}
		if cw > singleClientW {
			cw = singleClientW
			ch = int(float64(cw) / aspect)
		}
		if ch > singleClientH {
			ch = singleClientH
			cw = int(float64(ch) * aspect)
		}
		if cw <= 0 || ch <= 0 {
			continue
		}

		area := cw * ch
		if area > bestArea {
			bestArea = area
			bestW = cw + extraW
			bestH = ch + extraH
			bestCols = cols
		}
	}
	return bestW, bestH, bestCols
}

// CheckAccessibilityPermission 检查辅助功能权限状态并提示用户
func CheckAccessibilityPermission() bool {
	// 尝试执行一个简单的 osascript 命令来检测权限
	script := `tell application "System Events" to get name of every process`
	err := exec.Command("osascript", "-e", script).Run()
	return err == nil
}

// OpenAccessibilitySettings 打开系统辅助功能设置页面
func OpenAccessibilitySettings() {
	exec.Command("open", "x-apple.systempreferences:com.apple.preference.security?Privacy_Accessibility").Run()
}

// findPlayer 查找可用的投屏播放器（结果缓存，只搜索一次）
// 优先使用 go_legacy，其次 player
func findPlayer() {
	playerInitOnce.Do(func() {
		var candidates []struct {
			kind playerKind
			name string
		}

		// 优先搜索数据目录（ExtractGoLegacy 提取到此，确保用新版）
		if dataDir != "" {
			candidates = append(candidates,
				struct {
					kind playerKind
					name string
				}{playerGoLegacy, filepath.Join(dataDir, "go_legacy")},
			)
		}

		if exe, err := os.Executable(); err == nil {
			dir := filepath.Dir(exe)
			log.Printf("[screencast] 搜索主程序同目录: exe=%s dir=%s", exe, dir)
			candidates = append(candidates,
				struct {
					kind playerKind
					name string
				}{playerGoLegacy, filepath.Join(dir, "go_legacy")},
				struct {
					kind playerKind
					name string
				}{playerLegacy, filepath.Join(dir, "player", "player")},
				struct {
					kind playerKind
					name string
				}{playerLegacy, filepath.Join(dir, "player")},
			)
		}
		if wd, err := os.Getwd(); err == nil {
			candidates = append(candidates,
				struct {
					kind playerKind
					name string
				}{playerGoLegacy, filepath.Join(wd, "bin", "go_legacy")},
				struct {
					kind playerKind
					name string
				}{playerLegacy, filepath.Join(wd, "bin", "player", "player")},
				struct {
					kind playerKind
					name string
				}{playerLegacy, filepath.Join(wd, "bin", "player")},
			)
		}

		for _, c := range candidates {
			if _, err := os.Stat(c.name); err == nil {
				log.Printf("[screencast] 找到播放器: %s (类型=%d)", c.name, c.kind)
				playerPath = c.name
				playerInfo = c.kind
				return
			}
		}
		log.Printf("[screencast] 所有候选路径均未找到投屏播放器")
	})
}
