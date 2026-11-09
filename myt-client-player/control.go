package main

import (
	"encoding/binary"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"
)

const ctrlVersion = "LGCtrlV01"

// ControlChannel manages UDP/TCP control connection to remote device.
type ControlChannel struct {
	mu      sync.Mutex
	useTCP  bool
	conn    net.Conn
	udpConn *net.UDPConn
	// primaryAddr is resolved address for primary target (WriteTo).
	primaryAddr *net.UDPAddr
	targets     []net.Addr
	primary     string
	msgCh       chan []byte
	done        chan struct{}
	closeOnce   sync.Once // prevents double-close panic on done

	wg sync.WaitGroup // tracks sendLoop and tcpReadLoop goroutines

	// OnDisconnect is called when the TCP connection is lost.
	OnDisconnect func()
}

// NewControlChannel creates a new control channel.
func NewControlChannel(useTCP bool) *ControlChannel {
	return &ControlChannel{
		useTCP: useTCP,
		msgCh:  make(chan []byte, 64),
		done:   make(chan struct{}),
	}
}

// Connect establishes the control connection.
func (c *ControlChannel) Connect(host string, port int) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))
	c.primary = addr

	proto := "UDP"
	if c.useTCP {
		proto = "TCP"
	}
	logf("[ctrl] connecting to %s (%s)...\n", addr, proto)

	if c.useTCP {
		conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
		if err != nil {
			return fmt.Errorf("ctrl tcp connect: %w", err)
		}
		// TCP_NODELAY: 禁用 Nagle 算法，小包（鼠标/键盘事件）立即发送，降低延迟
		if tc, ok := conn.(*net.TCPConn); ok {
			tc.SetNoDelay(true)
		}
		c.conn = conn

		// TCP 模式也需要 UDP socket 来给被控设备发控制消息
		udpConn, err := net.ListenUDP("udp", nil)
		if err != nil {
			logf("[ctrl] tcp mode: udp listen failed (batch control disabled): %v\n", err)
		} else {
			c.udpConn = udpConn
		}

		// TCP handshake: [length_byte]["LGCtrlV01\0"]
		id := []byte(ctrlVersion)
		handshake := make([]byte, 0, 2+len(id))
		handshake = append(handshake, byte(1+len(id)+1)) // length
		handshake = append(handshake, id...)
		handshake = append(handshake, 0) // null terminator
		if _, err := conn.Write(handshake); err != nil {
			conn.Close()
			return fmt.Errorf("ctrl tcp handshake: %w", err)
		}
		logf("[ctrl] TCP connected and handshake sent\n")
	} else {
		udpAddr, err := net.ResolveUDPAddr("udp", addr)
		if err != nil {
			return fmt.Errorf("ctrl resolve udp: %w", err)
		}
		// Use unconnected UDP socket so WriteTo works for batch targets.
		// DialUDP creates a connected socket; on Windows, WriteTo on a
		// connected UDP socket always returns an error.
		conn, err := net.ListenUDP("udp", nil)
		if err != nil {
			return fmt.Errorf("ctrl udp listen: %w", err)
		}
		c.udpConn = conn
		c.primaryAddr = udpAddr
		logf("[ctrl] UDP socket ready to %s\n", addr)
	}

	// Add primary target
	if c.useTCP && c.conn != nil {
		c.targets = []net.Addr{c.conn.RemoteAddr()}
	} else if c.primaryAddr != nil {
		c.targets = []net.Addr{c.primaryAddr}
	}

	c.wg.Add(1)
	go c.sendLoop()

	// For TCP, start a read loop to detect disconnection.
	// The device doesn't send data on the control channel, so any read
	// activity means the connection is still alive. If the remote closes
	// the socket, the read will return EOF or an error.
	if c.useTCP && c.conn != nil {
		c.wg.Add(1)
		go c.tcpReadLoop(c.conn)
	}

	return nil
}

// SetTargetList parses "ip:port#ip:port" and adds extra targets (UDP only).
func (c *ControlChannel) SetTargetList(list string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Keep primary target
	var primary []net.Addr
	if len(c.targets) > 0 {
		primary = []net.Addr{c.targets[0]}
	}
	c.targets = primary

	logf("[ctrl] SetTargetList: list=%q, primary=%s, useTCP=%v, existing targets=%d\n", list, c.primary, c.useTCP, len(c.targets))

	for _, item := range strings.Split(list, "#") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if item == c.primary {
			logf("[ctrl] SetTargetList: skipping self (matches primary): %s\n", item)
			continue
		}
		addr, err := net.ResolveUDPAddr("udp", item)
		if err != nil {
			logf("[ctrl] batch target resolve failed: %s: %v\n", item, err)
			continue
		}
		c.targets = append(c.targets, addr)
		logf("[ctrl] batch target added: %s (resolved to %s)\n", item, addr)
	}
	logf("[ctrl] target list: %d targets total\n", len(c.targets))
	for i, t := range c.targets {
		logf("[ctrl]   target[%d]: %s (%T)\n", i, t, t)
	}
}

// Send enqueues a message for sending.
func (c *ControlChannel) Send(msg []byte) {
	// UDP/TCP 模式（原有逻辑）
	select {
	case c.msgCh <- msg:
	default:
		// queue full, drop — return pooled buffer if applicable
		returnPooledMsg(msg)
	}
}

// TargetCount 返回当前扇出目标数量（含主控自身目标）。
// 群控主控窗口 > 1；单台投屏 = 1；未连接 = 0。
func (c *ControlChannel) TargetCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.targets)
}

// Close shuts down the control channel.
func (c *ControlChannel) Close() {
	c.closeOnce.Do(func() {
		close(c.done)
	})

	// Wait for goroutines to exit before closing connections
	// (ensures no goroutine is using conn/udpConn after Close returns)
	c.wg.Wait()

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn != nil {
		c.conn.Close()
	}
	if c.udpConn != nil {
		c.udpConn.Close()
	}
}

func (c *ControlChannel) sendLoop() {
	defer c.wg.Done()
	for {
		select {
		case <-c.done:
			// Drain remaining messages to return pooled buffers
			for {
				select {
				case msg := <-c.msgCh:
					returnPooledMsg(msg)
				default:
					return
				}
			}
		case msg := <-c.msgCh:
			// Quick check: skip if already closed
			select {
			case <-c.done:
				returnPooledMsg(msg)
				return
			default:
			}
			c.sendMsg(msg)
		}
	}
}

// tcpReadLoop monitors the TCP connection for disconnection.
// The device normally doesn't send data on the control channel,
// so any read error (including EOF) indicates the connection is lost.
func (c *ControlChannel) tcpReadLoop(conn net.Conn) {
	defer c.wg.Done()
	buf := make([]byte, 1024)
	for {
		select {
		case <-c.done:
			return
		default:
		}
		if conn == nil {
			return
		}
		// Set a read deadline so we can check the done channel periodically
		conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		_, err := conn.Read(buf)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue // deadline expired, check done channel
			}
			// Connection lost (EOF, reset, etc.)
			select {
			case <-c.done:
				return // intentional close
			default:
			}
			logf("[ctrl] tcp connection lost: %v\n", err)
			if c.OnDisconnect != nil {
				c.OnDisconnect()
			}
			return
		}
	}
}

func (c *ControlChannel) sendMsg(msg []byte) {
	// For TCP, only send the wire-length bytes — pooled buffers may be larger
	// (e.g. BuildKeyCmd uses pool20 but wire length is 12). Sending the full
	// buffer would inject padding bytes into the TCP stream, corrupting the
	// next message's framing.
	wireLen := int(binary.BigEndian.Uint16(msg[0:]))
	if wireLen <= 0 || wireLen > len(msg) {
		wireLen = len(msg)
	}
	wireBuf := msg[:wireLen]

	if !c.useTCP {
		// UDP: copy targets under lock, then send without holding lock.
		// UDP uses the wire-length bytes; pooled buffers may be larger.
		// Sending the full buffer would break Android key commands.
		c.mu.Lock()
		udpConn := c.udpConn
		targets := make([]net.Addr, len(c.targets))
		copy(targets, c.targets)
		c.mu.Unlock()

		if udpConn != nil {
			for _, t := range targets {
				udpConn.SetWriteDeadline(time.Now().Add(5 * time.Millisecond))
				if _, err := udpConn.WriteTo(wireBuf, t); err != nil {
					if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
						continue
					}
					logf("[ctrl] udp write error: %v\n", err)
				}
			}
			udpConn.SetWriteDeadline(time.Time{})
		}
	} else {
		// TCP: send only wire-length bytes.
		c.mu.Lock()
		conn := c.conn
		targets := make([]net.Addr, len(c.targets))
		copy(targets, c.targets)
		udpConn := c.udpConn
		c.mu.Unlock()

		if conn != nil {
			if _, err := conn.Write(wireBuf); err != nil {
				logf("[ctrl] tcp write error: %v\n", err)
			}
		}
		// TCP 模式下，额外通过 UDP 发送给被控设备。
		if udpConn != nil && len(targets) > 1 {
			for i, t := range targets[1:] {
				udpConn.SetWriteDeadline(time.Now().Add(5 * time.Millisecond))
				n, err := udpConn.WriteTo(wireBuf, t)
				if err != nil {
					if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
						logf("[ctrl] tcp+udp batch target[%d] %s: write timeout\n", i, t)
						continue
					}
					logf("[ctrl] tcp+udp batch target[%d] %s: write error: %v\n", i, t, err)
				} else {
					logf("[ctrl] tcp+udp batch target[%d] %s: sent %d bytes\n", i, t, n)
				}
			}
			udpConn.SetWriteDeadline(time.Time{})
		} else if udpConn == nil && len(targets) > 1 {
			logf("[ctrl] tcp+udp: udpConn is nil, cannot send to %d batch targets\n", len(targets)-1)
		}
	}

	// Return pooled buffers after sending (conn.Write copies data, safe to return)
	returnPooledMsg(msg)
}

// returnPooledMsg returns a message buffer to the appropriate pool based on length.
func returnPooledMsg(msg []byte) {
	switch len(msg) {
	case 20:
		pool20.Put(&msg)
	case 32:
		pool32.Put(&msg)
	case 66:
		pool66.Put(&msg)
	}
}
