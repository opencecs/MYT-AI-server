// Package teboxcast 实现 tebox（QEMU Android 模拟器）投屏协议客户端。
//
// 本文件：独立音频 TCP 客户端（上游 0360f5e 把音频从 cast 流拆出后的新协议）。
package teboxcast

import (
	"encoding/binary"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// 音频端点协议（对照上游 tebox commit 0360f5e src/service/api/cmd/tebox-api/media.go
// 的 handleAudio 与 src/service/README.md 端点表）：
//   - 连接后 5s 内发送握手行 "TEBOX-AUDIO/1 <实例id>\n"（media.go handshake:
//     strings.Fields 恰好 2 个字段，proto 必须精确匹配 "TEBOX-AUDIO/1"）；
//   - 之后仍是 cast 记录流 [u32 be length][u8 codec][u8 flags][u64 be pts][payload]，
//     length 从 codec 起算（不含自身），10 ≤ length ≤ 32MB（media.go relayRecords
//     的 recordHeader/uplinkMax/downlinkMax 约束，越界服务端直接断开）；
//   - 服务端下行只保留 codec=16（AAC-LC ADTS，48kHz stereo，每记录 1 帧），
//     其余 codec 静默丢弃；上行仅 codec=33（麦克风 PCM，本客户端不推）；
//   - 服务端对每个连接只做记录桥接，无 viewer 数量限制（16 viewer 上限与
//     4MB 落后踢除是 video 端点 tebox-cast 后端的缓冲策略）；
//   - 断开即结束，重连是客户端责任（重连需重发握手行）。
const (
	AudioHandshakeProto = "TEBOX-AUDIO/1"
)

// AudioClient tebox 独立音频 TCP 客户端：连接 + 握手 + 记录解析 + 断线自动重连。
// 仅 codec=16 的记录经 OnAudio 吐出（payload 即 ADTS 帧），与 CastClient 流内
// 音频共用同一回调签名，播放侧（tebox_audio.go）零改动。旧部署（cast 流内
// codec=16）路径不受影响：是否启用本客户端由 -audio-port 编排层决定。
type AudioClient struct {
	// OnAudio AAC 音频记录回调（读循环 goroutine 内串行调用；未设=零开销跳过）。
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

// NewAudioClient 创建 tebox 独立音频客户端。
func NewAudioClient(host string, port int, instanceID string) *AudioClient {
	return &AudioClient{
		host: host,
		port: port,
		id:   instanceID,
		done: make(chan struct{}),
	}
}

func (c *AudioClient) logf(format string, args ...any) {
	if c.Logf != nil {
		c.Logf("[teboxcast:audio] "+format+"\n", args...)
	}
}

// Start 启动连接循环（异步；连接失败不视为错误，由内部按 1s 周期重试）。
func (c *AudioClient) Start() error {
	if !c.running.CompareAndSwap(false, true) {
		return fmt.Errorf("teboxcast: audio already started")
	}
	go c.loop()
	return nil
}

// Close 停止连接循环并关闭当前连接（幂等）。
func (c *AudioClient) Close() {
	c.closeOnce.Do(func() { close(c.done) })
	c.connMu.Lock()
	if c.conn != nil {
		c.conn.Close()
		c.conn = nil
	}
	c.connMu.Unlock()
}

func (c *AudioClient) loop() {
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
		c.logf("audio lost: %v (reconnecting)", err)
		if c.OnState != nil {
			c.OnState(false)
		}
		if !c.sleep(reconnectDelay) {
			return
		}
	}
}

// sleep 等待 d；期间被 Close 打断返回 false。
func (c *AudioClient) sleep(d time.Duration) bool {
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
func (c *AudioClient) dialAndHandshake() (net.Conn, error) {
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(c.host, fmt.Sprintf("%d", c.port)), dialTimeout)
	if err != nil {
		return nil, err
	}
	if tc, ok := conn.(*net.TCPConn); ok {
		tc.SetNoDelay(true) // 低延迟：音频帧立即发出
	}
	handshake := fmt.Sprintf("%s %s\n", AudioHandshakeProto, c.id)
	if _, err := conn.Write([]byte(handshake)); err != nil {
		conn.Close()
		return nil, fmt.Errorf("handshake write: %w", err)
	}
	return conn, nil
}

// readLoop 持续解析记录流，仅 codec=16（AAC）经 OnAudio 吐出。
// 与 CastClient.readLoop 的帧解析一致（同一记录格式），差异仅握手行与
// 是否有视频帧——独立音频端点无 codec=0，无需 OnFrame。
func (c *AudioClient) readLoop(conn net.Conn) error {
	var lenBuf [4]byte
	for {
		if _, err := readFull(conn, lenBuf[:]); err != nil {
			return err
		}
		length := binary.BigEndian.Uint32(lenBuf[:])
		if length < frameHdrSize || length > MaxFrameSize {
			return fmt.Errorf("bad record length %d", length)
		}
		body := make([]byte, length)
		if _, err := readFull(conn, body); err != nil {
			return err
		}
		if body[0] != CodecAAC {
			continue // 服务端已过滤，防御性跳过其他 codec
		}
		if c.OnAudio == nil {
			continue
		}
		c.OnAudio(body[frameHdrSize:])
	}
}
