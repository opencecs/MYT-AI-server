//go:build windows

package screencast

import (
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

// ProjectionWindow 投屏窗口（Windows 平台，调用 go_legacy.exe）
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
		return nil, fmt.Errorf("找不到投屏播放器 (go_legacy.exe)")
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

	// 预取本地回环可用端口给播放器控制接口（Listen→Close，回环可接受微小竞态；
	// 分配失败 controlPort=0 → 不追加 -control-port，工具侧降级 Win32 截屏/报错）
	w.mu.Lock()
	w.controlPort = reserveControlPort()
	w.mu.Unlock()

	args := w.buildArgs()

	w.mu.Lock()
	w.cmd = exec.Command(playerPath, args...)
	w.cmd.Dir = filepath.Dir(playerPath)
	w.cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000000}

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

// reserveControlPort 预取一个本地回环可用端口（Listen→Close，回环可接受
// 微小竞态）。失败返回 0（不追加 -control-port）。
func reserveControlPort() int {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	return port
}

// --- 后续方法已按职责拆分到：---
//   - window_windows_args.go    （buildArgs）
//   - window_windows_control.go （Stop/UpdateTitle/BringToFront/SetOnExit/notifyExit + win32 变量）
//   - window_windows_find.go    （findPlayer/findWindowByPID/setWindowText）
//   - window_windows_arrange.go （reveal 系列 + ArrangePlayerWindows + doArrangePlayerWindows）
