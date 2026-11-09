package teboxcast

import (
	"encoding/binary"
	"net"
	"strings"
	"testing"
	"time"
)

// startFakeAudioServer 启动一个假独立音频服务端（对照上游 media.go handleAudio）：
// 校验握手行必须是 "TEBOX-AUDIO/1 <id>"，然后按 records 下发记录（只发 codec=16
// 与会被丢弃的噪音记录），写完保持连接片刻。
func startFakeAudioServer(t *testing.T, wantProto string, records [][]byte) (addr string, handshakeCh chan string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })
	handshakeCh = make(chan string, 16)
	handshakeCh = make(chan string, 16)
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go serveAudioConn(conn, wantProto, records, handshakeCh)
		}
	}()
	return ln.Addr().String(), handshakeCh
}

// serveAudioConn 处理一条音频连接：读握手行，可选校验后下发 records。
func serveAudioConn(conn net.Conn, wantProto string, records [][]byte, handshakeCh chan<- string) {
	defer conn.Close()
	buf := make([]byte, 256)
	n, err := conn.Read(buf)
	if err != nil {
		return
	}
	handshakeCh <- string(buf[:n])
	if wantProto != "" && !strings.HasPrefix(string(buf[:n]), wantProto+" ") {
		return // 握手不匹配：直接断开（上游行为）
	}
	for _, rec := range records {
		hdr := make([]byte, 4)
		binary.BigEndian.PutUint32(hdr, uint32(len(rec)))
		if _, err := conn.Write(hdr); err != nil {
			return
		}
		if _, err := conn.Write(rec); err != nil {
			return
		}
	}
	time.Sleep(300 * time.Millisecond)
}

// TestAudioClientHandshakeAndRouting 验证独立音频端点：握手行 TEBOX-AUDIO/1、
// codec=16 走 OnAudio、其他 codec 被跳过。
func TestAudioClientHandshakeAndRouting(t *testing.T) {
	aac := buildRecord(CodecAAC, 0, 42, []byte{0xff, 0xf1, 0x50, 0x80})
	noise := buildRecord(0x21, 0, 0, []byte{0x01, 0x02}) // 非 AAC codec，应被丢弃

	var got [][]byte
	addr, hsCh := startFakeAudioServer(t, AudioHandshakeProto, [][]byte{noise, aac, noise})

	c := NewAudioClient("127.0.0.1", portOf(addr), "inst-1")
	c.OnAudio = func(p []byte) { got = append(got, append([]byte(nil), p...)) }
	c.OnState = func(connected bool) {}
	c.Logf = func(f string, a ...any) { t.Logf(f, a...) }
	if err := c.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer c.Close()

	select {
	case hs := <-hsCh:
		if !strings.HasPrefix(hs, "TEBOX-AUDIO/1 inst-1\n") {
			t.Fatalf("bad handshake line: %q", hs)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no handshake received")
	}

	deadline := time.After(3 * time.Second)
	for len(got) < 1 {
		select {
		case <-deadline:
			t.Fatal("no AAC payload delivered")
		case <-time.After(20 * time.Millisecond):
		}
	}
	if len(got) != 1 || string(got[0]) != string(aac[frameHdrSize:]) {
		t.Fatalf("unexpected payloads: %v", got)
	}
}

// TestAudioClientBadHandshakeRejected 验证服务端拒绝错误握手时客户端重连
//（假服务端每个连接只应答一次握手校验失败，客户端应持续重试不崩溃）。
func TestAudioClientBadHandshakeRejected(t *testing.T) {
	addr, hsCh := startFakeAudioServer(t, "TEBOX-CAST/1", nil) // 服务端只认 cast 握手

	c := NewAudioClient("127.0.0.1", portOf(addr), "inst-1")
	c.Logf = func(f string, a ...any) { t.Logf(f, a...) }
	if err := c.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer c.Close()

	// 至少发过 2 次握手（首次被拒后 1s 重连重发，重连间隔 1s + 余量）
	n := 0
	deadline := time.After(6 * time.Second)
	for n < 2 {
		select {
		case <-hsCh:
			n++
		case <-deadline:
			t.Fatalf("expected >=2 handshake attempts, got %d", n)
		case <-time.After(50 * time.Millisecond):
		}
	}
}

// portOf 从 "127.0.0.1:port" 取端口。
func portOf(addr string) int {
	_, port, _ := net.SplitHostPort(addr)
	p := 0
	for _, ch := range port {
		p = p*10 + int(ch-'0')
	}
	return p
}
