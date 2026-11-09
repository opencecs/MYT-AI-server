// Package teboxcast 实现 tebox（QEMU Android 模拟器）投屏协议客户端。
//
// 自包含约束：本包只依赖标准库，不 import 播放器其他任何包，
// 与上层（解码渲染管线 / 控制事件消费方）仅通过回调解耦。
//
// 视频协议（TCP 9901，对照 tebox INTEGRATION.md 与 cast/tools/cast_view.c）：
//  1. 连接后发送握手行 "TEBOX-CAST/1 <实例id>\n"；
//  2. 持续接收帧记录（整数均大端）：
//     [u32 length][u8 codec][u8 flags][u64 pts_us][payload]
//     length 从 codec 起算（不含自身 4 字节）；flags 第 0 位=关键帧；
//     codec=0 为 H.264 Annex-B（关键帧带 SPS/PPS，连上即发关键帧、静止补帧）；
//     codec=16 为 AAC-LC ADTS 帧（每记录 1 帧，48kHz stereo，flags 恒 0）。
//  3. 断线（API 重启等）后每 1s 重连，重连必须重发握手行。
package teboxcast

import (
	"encoding/binary"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// 协议常量（与 cast_view.c / protocol.h 一致）
const (
	HandshakeProto = "TEBOX-CAST/1"
	// frameHdrSize 帧记录头长度：codec(1) + flags(1) + pts_us(8)
	frameHdrSize = 10
	// MaxFrameSize 单帧上限（cast_view.c 同款 32MB 保护）
	MaxFrameSize = 32 << 20
	// CodecH264 codec 字段取值
	CodecH264 = 0
	// CodecAAC AAC-LC ADTS 帧记录（音频），协议同 protocol.h CAST_CODEC_AAC
	CodecAAC = 16
	// reconnectDelay 断线重连间隔（文档建议 1–2s）
	reconnectDelay = time.Second
	dialTimeout    = 5 * time.Second
)

// Frame 一帧视频记录。
type Frame struct {
	Payload  []byte // H.264 Annex-B 数据（仅回调期间有效，需保留请自行拷贝）
	PTSUs    uint64 // 时间戳，微秒
	KeyFrame bool   // flags 第 0 位为 1
}

// CastClient tebox 投屏 TCP 客户端：连接 + 握手 + 帧解析 + 断线自动重连。
type CastClient struct {
	// OnFrame 每帧回调（读循环 goroutine 内串行调用）。
	OnFrame func(Frame)
	// OnAudio AAC 音频记录回调（读循环 goroutine 内串行调用；未设=零开销跳过）。
	// 与视频同一条 TCP，仅 codec=16 的记录走此回调。
	OnAudio func(payload []byte)
	// OnState 连接状态回调：true=已连接（握手完成），false=连接断开。
	OnState func(connected bool)
	// Logf 日志回调（可选，空=静默）。
	Logf func(format string, args ...any)

	host string
	port int
	id   string

	done      chan struct{}
	closeOnce sync.Once
	connMu    sync.Mutex
	conn      net.Conn
	running   atomic.Bool
}

// NewCastClient 创建 tebox 投屏客户端。
func NewCastClient(host string, port int, instanceID string) *CastClient {
	return &CastClient{
		host: host,
		port: port,
		id:   instanceID,
		done: make(chan struct{}),
	}
}

func (c *CastClient) logf(format string, args ...any) {
	if c.Logf != nil {
		c.Logf("[teboxcast] "+format+"\n", args...)
	}
}

// Start 启动连接循环（异步；连接失败不视为错误，由内部按 1s 周期重试）。
func (c *CastClient) Start() error {
	if !c.running.CompareAndSwap(false, true) {
		return fmt.Errorf("teboxcast: already started")
	}
	go c.loop()
	return nil
}

// Close 停止连接循环并关闭当前连接（幂等）。
func (c *CastClient) Close() {
	c.closeOnce.Do(func() { close(c.done) })
	c.connMu.Lock()
	if c.conn != nil {
		c.conn.Close()
		c.conn = nil
	}
	c.connMu.Unlock()
}

func (c *CastClient) loop() {
	defer c.running.Store(false)
	for {
		select {
		case <-c.done:
			return
		default:
		}

		conn, err := c.dialAndHandshake()
		if err != nil {
			c.logf("connect %s:%d failed: %v (retry in %s)", c.host, c.port, err, reconnectDelay)
			if !c.sleep(reconnectDelay) {
				return
			}
			continue
		}

		c.connMu.Lock()
		c.conn = conn
		c.connMu.Unlock()
		c.logf("connected %s:%d id=%s", c.host, c.port, c.id)
		if c.OnState != nil {
			c.OnState(true)
		}

		err = c.readLoop(conn)

		c.connMu.Lock()
		c.conn = nil
		c.connMu.Unlock()
		conn.Close()
		c.logf("video lost: %v (reconnecting)", err)
		if c.OnState != nil {
			c.OnState(false)
		}
		if !c.sleep(reconnectDelay) {
			return
		}
	}
}

// sleep 等待 d；期间被 Close 打断返回 false。
func (c *CastClient) sleep(d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-c.done:
		return false
	case <-t.C:
		return true
	}
}

// dialAndHandshake 建立 TCP 连接并发送握手行（重连必须重发，服务端按行路由实例）。
func (c *CastClient) dialAndHandshake() (net.Conn, error) {
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(c.host, fmt.Sprintf("%d", c.port)), dialTimeout)
	if err != nil {
		return nil, err
	}
	if tc, ok := conn.(*net.TCPConn); ok {
		tc.SetNoDelay(true) // 低延迟：帧数据立即发出
	}
	handshake := fmt.Sprintf("%s %s\n", HandshakeProto, c.id)
	if _, err := conn.Write([]byte(handshake)); err != nil {
		conn.Close()
		return nil, fmt.Errorf("handshake write: %w", err)
	}
	return conn, nil
}

// readLoop 持续解析帧记录并通过 OnFrame 吐出。
func (c *CastClient) readLoop(conn net.Conn) error {
	var lenBuf [4]byte
	for {
		if _, err := readFull(conn, lenBuf[:]); err != nil {
			return err
		}
		length := binary.BigEndian.Uint32(lenBuf[:])
		if length < frameHdrSize || length > MaxFrameSize {
			return fmt.Errorf("bad frame length %d", length)
		}
		body := make([]byte, length)
		if _, err := readFull(conn, body); err != nil {
			return err
		}
		if body[0] == CodecAAC {
			if c.OnAudio != nil {
				c.OnAudio(body[frameHdrSize:])
			}
			continue
		}
		if body[0] != CodecH264 {
			continue // 其他非 0 codec（HEVC/VP8/...），安全跳过
		}
		if c.OnFrame == nil {
			continue
		}
		c.OnFrame(Frame{
			Payload:  body[frameHdrSize:],
			PTSUs:    binary.BigEndian.Uint64(body[2:frameHdrSize]),
			KeyFrame: body[1]&0x01 != 0,
		})
	}
}

// readFull 读满 n 字节（TCP 粘包处理）。
func readFull(conn net.Conn, buf []byte) (int, error) {
	off := 0
	for off < len(buf) {
		n, err := conn.Read(buf[off:])
		if n > 0 {
			off += n
		}
		if err != nil {
			return off, err
		}
	}
	return off, nil
}
