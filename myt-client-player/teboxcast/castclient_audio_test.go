package teboxcast

import (
	"encoding/binary"
	"net"
	"strconv"
	"testing"
	"time"
)

// startFakeServer 启动一个假 cast 服务端：接受握手后按 records 写入帧记录，
// 返回一个通知通道（写完关闭）。
func startFakeServer(t *testing.T, records [][]byte) (addr string, done <-chan struct{}) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })
	ch := make(chan struct{})
	go func() {
		defer close(ch)
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		// 读握手行
		buf := make([]byte, 64)
		n, _ := conn.Read(buf)
		if n == 0 {
			return
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
		// 保持连接一会儿，让客户端读完
		time.Sleep(200 * time.Millisecond)
	}()
	return ln.Addr().String(), ch
}

// buildRecord 构造一条帧记录：codec + flags + pts + payload。
func buildRecord(codec, flags byte, pts uint64, payload []byte) []byte {
	rec := make([]byte, frameHdrSize+len(payload))
	rec[0] = codec
	rec[1] = flags
	binary.BigEndian.PutUint64(rec[2:], pts)
	copy(rec[frameHdrSize:], payload)
	return rec
}

// TestReadLoopAudioRouting 验证 codec=16 走 OnAudio、codec=0 走 OnFrame、
// 其他非 0 codec 安全跳过。
func TestReadLoopAudioRouting(t *testing.T) {
	var (
		frames      []Frame
		audioFrames [][]byte
	)
	recH264 := buildRecord(CodecH264, 0x01, 100, []byte{0x00, 0x00, 0x00, 0x01, 0x65})
	recAAC := buildRecord(CodecAAC, 0, 200, []byte{0xFF, 0xF1, 0x50, 0x80, 0x01, 0xFC, 0xFC})
	recVP8 := buildRecord(2, 0, 300, []byte{0x01, 0x02})

	addr, done := startFakeServer(t, [][]byte{recH264, recAAC, recVP8, recAAC})
	host, portStr, _ := net.SplitHostPort(addr)
	port, _ := strconv.Atoi(portStr)

	c := NewCastClient(host, port, "test-inst")
	c.Logf = t.Logf
	c.OnFrame = func(f Frame) { frames = append(frames, f) }
	c.OnAudio = func(p []byte) { cp := make([]byte, len(p)); copy(cp, p); audioFrames = append(audioFrames, cp) }
	if err := c.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer c.Close()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for server to finish")
	}
	time.Sleep(100 * time.Millisecond)

	if len(frames) != 1 {
		t.Fatalf("OnFrame calls = %d, want 1", len(frames))
	}
	if !frames[0].KeyFrame || frames[0].PTSUs != 100 {
		t.Fatalf("frame = %+v, want keyframe pts=100", frames[0])
	}
	if len(audioFrames) != 2 {
		t.Fatalf("OnAudio calls = %d, want 2", len(audioFrames))
	}
	for i, a := range audioFrames {
		want := recAAC[frameHdrSize:]
		if len(a) != len(want) {
			t.Fatalf("audio[%d] len = %d, want %d", i, len(a), len(want))
		}
		for j := range want {
			if a[j] != want[j] {
				t.Fatalf("audio[%d][%d] = %#x, want %#x", i, j, a[j], want[j])
			}
		}
	}
}

// TestReadLoopNoAudioCallback 验证未设 OnAudio 时 codec=16 记录被零开销跳过，
// 不影响视频路径。
func TestReadLoopNoAudioCallback(t *testing.T) {
	var frameCount int
	recH264 := buildRecord(CodecH264, 0x01, 100, []byte{0x00, 0x00, 0x00, 0x01, 0x65})
	recAAC := buildRecord(CodecAAC, 0, 200, make([]byte, 32))

	addr, done := startFakeServer(t, [][]byte{recAAC, recH264, recAAC})
	host, portStr, _ := net.SplitHostPort(addr)
	port, _ := strconv.Atoi(portStr)

	c := NewCastClient(host, port, "test-inst")
	c.OnFrame = func(f Frame) { frameCount++ }
	if err := c.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer c.Close()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for server to finish")
	}
	time.Sleep(100 * time.Millisecond)

	if frameCount != 1 {
		t.Fatalf("OnFrame calls = %d, want 1 (audio records must be skipped)", frameCount)
	}
}
