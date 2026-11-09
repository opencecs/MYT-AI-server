//go:build linux

package screencast

import (
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

// ProjectionWindow 投屏窗口（Linux 平台，调用 go_legacy；对齐 window_darwin.go /
// window_windows.go 模式）。
type ProjectionWindow struct {
	config      ProjectionConfig
	onExit      func(exitCode int, failed bool, cfg *ProjectionConfig)
	cmd         *exec.Cmd
	done        chan struct{}
	mu          sync.Mutex
	startedAt   time.Time
	controlPort int // 播放器本地控制 HTTP 端口（0 = 未分配）
}

var (
	playerPath     string
	playerFound    bool
	playerInitOnce sync.Once
)

func NewProjectionWindow(cfg ProjectionConfig) (*ProjectionWindow, error) {
	findPlayer()
	if !playerFound {
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
	if !playerFound {
		w.notifyExit(-1, true)
		return
	}

	// 预取本地回环可用端口给播放器控制接口（对照 window_windows.go reserveControlPort）
	w.mu.Lock()
	w.controlPort = reserveControlPort()
	w.mu.Unlock()

	args := w.buildGoLegacyArgs()

	w.mu.Lock()
	w.cmd = exec.Command(playerPath, args...)
	w.cmd.Dir = filepath.Dir(playerPath)
	// 强制 GDK 走 X11 后端：COSMIC 等会话默认 GDK_BACKEND=wayland,x11，播放器
	// GTK 选 Wayland 后端会拿不到 X11 XID（渲染器依赖 gdk_x11_window_get_xid）。
	// 追加在 os.Environ() 之后覆盖继承值（append 后者胜出）。
	w.cmd.Env = append(os.Environ(), "GDK_BACKEND=x11")

	// stderr 重定向到 logs/go_legacy.log（与 Windows/macOS 一致，供排查投屏问题）
	if logDir := filepath.Join(filepath.Dir(playerPath), "logs"); os.MkdirAll(logDir, 0o755) == nil {
		if f, err := os.OpenFile(filepath.Join(logDir, "go_legacy.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
			w.cmd.Stderr = f
		}
	}
	w.mu.Unlock()

	log.Printf("[screencast] 启动 %s %v", playerPath, args)

	if err := w.cmd.Start(); err != nil {
		log.Printf("[screencast] 播放器启动失败: %v", err)
		w.notifyExit(-1, true)
		return
	}
	w.startedAt = time.Now()

	go func() {
		err := w.cmd.Wait()
		elapsed := time.Since(w.startedAt)
		exitCode := -1
		if w.cmd.ProcessState != nil {
			exitCode = w.cmd.ProcessState.ExitCode()
		}
		failed := elapsed < 5*time.Second && exitCode != ExitCodeRotate
		log.Printf("[screencast] 播放器退出: elapsed=%v exitCode=%d err=%v failed=%v",
			elapsed.Round(time.Millisecond), exitCode, err, failed)
		w.notifyExit(exitCode, failed)
	}()
}

// reserveControlPort 预取一个本地回环可用端口（Listen→Close，回环可接受微小竞态）。
// 失败返回 0（不追加 -control-port）。
func reserveControlPort() int {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	return port
}

// buildGoLegacyArgs 构建 go_legacy 的命令行参数（RTSP 模式子集，对照
// window_darwin.go buildGoLegacyArgs；Linux MVP1 不含 tebox/WebURL/矩阵扇出）。
func (w *ProjectionWindow) buildGoLegacyArgs() []string {
	args := []string{
		"-ip", w.config.DeviceIP,
		"-vport", fmt.Sprintf("%d", w.config.VideoPort),
		"-cport", fmt.Sprintf("%d", w.config.ControlPort),
		"-rtsp_tcp",
		"-nosidebar", // Linux MVP1 无侧边栏
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
	// 窗口尺寸：WinW>0 表示定尺寸投屏，传给 go_legacy 固定窗口大小
	if w.config.WinW > 0 {
		args = append(args, "-winw", fmt.Sprintf("%d", w.config.WinW))
		args = append(args, "-winh", fmt.Sprintf("%d", w.config.WinH))
	}
	// 窗口位置：哨兵 WinPosUnset = 未设（居中）。0 是合法位置必须保留。
	if w.config.WinX != WinPosUnset {
		args = append(args, "-winx", fmt.Sprintf("%d", w.config.WinX))
	}
	if w.config.WinY != WinPosUnset {
		args = append(args, "-winy", fmt.Sprintf("%d", w.config.WinY))
	}
	if w.config.NotifyPort > 0 {
		args = append(args, "-notifyport", fmt.Sprintf("%d", w.config.NotifyPort))
	}
	// 本地控制接口（screenshot_window / close_screencast 工具用；0=不追加）
	if w.controlPort > 0 {
		args = append(args, "-control-port", fmt.Sprintf("%d", w.controlPort))
	}
	return args
}

// PID 播放器进程 PID（0 = 进程未启动/已退出）。
func (w *ProjectionWindow) PID() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.cmd != nil && w.cmd.Process != nil {
		return w.cmd.Process.Pid
	}
	return 0
}

// ControlPort 播放器本地控制 HTTP 端口（0 = 未启用）。
func (w *ProjectionWindow) ControlPort() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.controlPort
}

func (w *ProjectionWindow) Stop() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.cmd != nil && w.cmd.Process != nil {
		w.cmd.Process.Kill()
	}
}

// UpdateTitle 实时更新投屏窗口标题（不重启投屏）。
// Linux 无辅助进程可依赖：进程内窗口标题由 go_legacy 启动时的 -title 定死，
// 运行时改名需播放器实现 X11 协议（MVP1 未做），此处仅记录生效值。
func (w *ProjectionWindow) UpdateTitle(title string) {
	w.mu.Lock()
	w.config.Title = title
	w.mu.Unlock()
	log.Printf("[screencast] Linux UpdateTitle: %q（运行时改名需播放器侧支持，下次启动生效）", title)
}

func (w *ProjectionWindow) SetOnExit(fn func(exitCode int, failed bool, cfg *ProjectionConfig)) {
	w.mu.Lock()
	w.onExit = fn
	w.mu.Unlock()
}

// BringToFront 将投屏窗口置顶显示（Linux 走 wmctrl _NET_ACTIVE_WINDOW；未安装时静默）。
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
	if _, err := exec.LookPath("wmctrl"); err != nil {
		return
	}
	// wmctrl 按 PID 激活窗口（-i 按 id；按 pid 需 -p 变体：用 xdotool 兜底）
	if _, err := exec.LookPath("xdotool"); err == nil {
		_ = exec.Command("xdotool", "search", "--pid", fmt.Sprintf("%d", pid), "windowactivate").Run()
	}
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
