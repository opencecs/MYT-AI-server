package adbterm

import (
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"
)

// dialTimeout ADB 建连超时（局域网直连，实测毫秒级）
const dialTimeout = 8 * time.Second

// shellDestinations OPEN 目标候选（按优先级尝试，全部在 ADB 通道内协商）：
//  1. shell,v2,pty  —— v2 协议 + PTY（stdout/stderr 分离、退出码、窗口大小）
//  2. shell,v2,raw  —— v2 协议无 PTY（老 adbd 不接受 pty 关键字时）
//  3. shell:       —— 原生 raw PTY（无退出码，仅输出）
var shellDestinations = []string{"shell,v2,pty:", "shell,v2,raw:", "shell:"}

// Session 一个 ADB shell 会话（交互式 PTY）
type Session struct {
	conn     net.Conn
	localID  uint32
	remoteID uint32
	v2       bool // 是否 shell_v2 协议（决定 WRTE 载荷是否带 5 字节消息头）
	onData   func(stream byte, data []byte)
	onExit   func(code int)

	writeMu   sync.Mutex
	closeOnce sync.Once
	exitOnce  sync.Once
	closed    chan struct{}
}

// DialShell 连接 adb 端口并打开交互式 shell。
// onData：stream = shStdout(1) / shStderr(2)；onExit：shell 退出码。
func DialShell(addr string, cols, rows int, onData func(stream byte, data []byte), onExit func(code int)) (*Session, error) {
	conn, err := net.DialTimeout("tcp", addr, dialTimeout)
	if err != nil {
		return nil, fmt.Errorf("连接 ADB 端口失败（%s）: %w", addr, err)
	}
	s := &Session{
		conn:   conn,
		onData: onData,
		onExit: onExit,
		closed: make(chan struct{}),
	}
	if err := s.handshake(); err != nil {
		conn.Close()
		return nil, err
	}
	go s.readLoop()
	return s, nil
}

// handshake 完成 CNXN 协商 + OPEN shell 并等待 OKAY
func (s *Session) handshake() error {
	if err := writePacket(s.conn, cmdCNXN, adbVersion, maxPayload,
		cnxnPayload("shell_v2,cmd,stat,ls_v2")); err != nil {
		return fmt.Errorf("ADB 握手失败: %w", err)
	}
	// 等 CNXN：AUTH 表示设备要求签名认证（容器 adbd 实测无需认证）
	s.conn.SetReadDeadline(time.Now().Add(dialTimeout))
	for {
		p, err := readPacket(s.conn)
		if err != nil {
			return fmt.Errorf("ADB 握手无响应: %w", err)
		}
		if p.cmd == cmdAUTH {
			return errors.New("设备要求 ADB 认证（该容器未开放免认证 adb）")
		}
		if p.cmd == cmdCNXN {
			break
		}
	}

	// 逐个候选目标尝试 OPEN，直到设备回 OKAY
	var lastErr error
	for _, dest := range shellDestinations {
		s.localID = 1
		payload := append([]byte(dest), 0)
		if err := writePacket(s.conn, cmdOPEN, s.localID, 0, payload); err != nil {
			return fmt.Errorf("ADB 打开 shell 失败: %w", err)
		}
		s.conn.SetReadDeadline(time.Now().Add(dialTimeout))
		opened := false
		for {
			p, err := readPacket(s.conn)
			if err != nil {
				lastErr = fmt.Errorf("ADB 打开 shell 无响应: %w", err)
				break
			}
			if p.cmd == cmdOKAY {
				s.remoteID = p.arg0
				s.v2 = strings.Contains(dest, "v2")
				opened = true
				break
			}
			if p.cmd == cmdCLSE {
				lastErr = fmt.Errorf("设备拒绝 shell 目标 %q", strings.TrimSuffix(dest, ":"))
				break
			}
		}
		if opened {
			s.conn.SetReadDeadline(time.Time{}) // 清除握手期读超时（交互会话常驻）
			return nil
		}
	}
	if lastErr == nil {
		lastErr = errors.New("ADB 打开 shell 失败")
	}
	return lastErr
}

// readLoop 持续读包：输出回调 / 退出回调 / 流控 ACK
func (s *Session) readLoop() {
	defer s.finish(0)
	for {
		p, err := readPacket(s.conn)
		if err != nil {
			return
		}
		switch p.cmd {
		case cmdWRTE:
			// 流控：arg1 非 0 表示对端期待 ACK
			if p.arg1 != 0 {
				s.writeMu.Lock()
				_ = writePacket(s.conn, cmdOKAY, p.arg1, p.arg0, nil)
				s.writeMu.Unlock()
			}
			if !s.v2 {
				if len(p.data) > 0 {
					s.onData(shStdout, p.data)
				}
				continue
			}
			// 一个 WRTE 可打包多条 shell_v2 消息（设备会把回显/输出合并发送），
			// 必须循环解析——只取第一条会丢掉换行与后续输出（表现为「不换行」）
			s.handleShellPayload(p.data)
		case cmdCLSE:
			s.finish(0)
			return
		}
	}
}

// handleShellPayload 解析一个 WRTE 载荷中的全部 shell_v2 消息
func (s *Session) handleShellPayload(data []byte) {
	for len(data) >= 5 {
		stream, payload, ok := parseShellMessage(data)
		if !ok {
			return
		}
		switch stream {
		case shStdout, shStderr:
			if len(payload) > 0 {
				s.onData(stream, payload)
			}
		case shExit:
			code := 0
			if len(payload) > 0 {
				code = int(payload[0])
			}
			s.finish(code)
		}
		data = data[5+len(payload):]
	}
}

// Write 发送标准输入（xterm 的按键数据）
func (s *Session) Write(data []byte) error {
	if len(data) == 0 {
		return nil
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	select {
	case <-s.closed:
		return errors.New("会话已关闭")
	default:
	}
	if s.v2 {
		return writePacket(s.conn, cmdWRTE, s.localID, s.remoteID, shellMessage(shStdin, data))
	}
	return writePacket(s.conn, cmdWRTE, s.localID, s.remoteID, data)
}

// Resize 同步 PTY 窗口大小（仅 v2 协议支持，raw 模式忽略）
func (s *Session) Resize(cols, rows int) error {
	if !s.v2 || cols <= 0 || rows <= 0 {
		return nil
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	select {
	case <-s.closed:
		return nil
	default:
	}
	return writePacket(s.conn, cmdWRTE, s.localID, s.remoteID,
		shellMessage(shWindowSize, windowSize(rows, cols)))
}

// Close 关闭会话（通知设备不再输入并断开连接）
func (s *Session) Close() error {
	s.closeOnce.Do(func() {
		s.writeMu.Lock()
		if s.v2 {
			_ = writePacket(s.conn, cmdWRTE, s.localID, s.remoteID, shellMessage(shCloseStdin, nil))
		}
		_ = writePacket(s.conn, cmdCLSE, s.localID, s.remoteID, nil)
		s.writeMu.Unlock()
		close(s.closed)
		_ = s.conn.Close()
	})
	return nil
}

// finish 结束会话（退出回调只触发一次）
func (s *Session) finish(code int) {
	s.exitOnce.Do(func() {
		if s.onExit != nil {
			s.onExit(code)
		}
	})
}
