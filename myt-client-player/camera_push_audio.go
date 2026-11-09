//go:build windows

package main

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// audioOnlyLoop 纯音频路子循环：不推视频，独立 TCP 连设备 + 握手 + 推音频。
// 生命周期由 handle 控制；h.stop 关闭即断开连接退出，收尾时 close(h.done)。
func (cp *CameraPusher) audioOnlyLoop(h *runHandle, micName string) {
	defer func() {
		h.cmd = nil
		close(h.done)
		cp.reapAudio(h)
	}()
	if micName == "" {
		return
	}
	remote, err := net.DialTimeout("tcp", net.JoinHostPort(cp.deviceIP, strconv.Itoa(cp.tcpPort)), 5*time.Second)
	if err != nil {
		logf("[mic] device connect failed: %v", err)
		return
	}
	defer remote.Close()
	if tcp, ok := remote.(*net.TCPConn); ok {
		tcp.SetNoDelay(true)
	}

	if err := cp.sendHandshake(remote); err != nil {
		logf("[mic] handshake failed: %v", err)
		return
	}

	// 设备在 hello 之后会沿连接主动发 RPC 请求（method 100/101/102 等），
	// 需要像 WindowsPusher 那样应答，音频注入通道才会被 arm。
	// 用单一 reader 协程解析 [4B BE len][payload] 帧并应答；injectAudio 只写不读。
	var writeMu sync.Mutex
	br := bufio.NewReaderSize(remote, 4096)
	go cp.serveDeviceRPC(br, remote, &writeMu, h.stop)

	cp.injectAudio(remote, &writeMu, micName, h.stop)
}

// serveDeviceRPC 持续读取设备发来的 TLV 帧，遇到请求帧(field1==1)按 method 应答。
// 应答格式复刻 WindowsPusher：把 field1 置 2、回显 seq 与 method、带 JSON 结果。
// 这是唯一的读协程，避免与握手响应读取竞争。
func (cp *CameraPusher) serveDeviceRPC(br *bufio.Reader, remote net.Conn, writeMu *sync.Mutex, stop <-chan struct{}) {
	for {
		hdr := make([]byte, 4)
		if _, err := io.ReadFull(br, hdr); err != nil {
			return // 连接关闭/出错，随路退出
		}
		n := binary.BigEndian.Uint32(hdr)
		if n == 0 || n > 65536 {
			logf("[mic] rpc bad frame len=%d, abort reader", n)
			return
		}
		payload := make([]byte, n)
		if _, err := io.ReadFull(br, payload); err != nil {
			return
		}
		field1, seq, method, ok := parseTLVFields(payload)
		if !ok {
			continue
		}
		// 仅对请求帧(field1==1)且已知 method 应答
		if field1 == 1 && (method == 100 || method == 101 || method == 102) {
			resp := buildTLVResponse(seq, method)
			writeMu.Lock()
			_, werr := remote.Write(resp)
			writeMu.Unlock()
			if werr != nil {
				return
			}
			logf("[mic] replied method=%d seq=%d", method, seq)
		}
	}
}

// parseTLVFields 解析 payload = magic(4) + 4×[len4][val4 LE]，返回 field1/seq/field3/field4。
// seq=第3个字段(偏移24)，method=第4个字段(偏移32)。
func parseTLVFields(p []byte) (field1, seq, method uint32, ok bool) {
	if len(p) < 40 || !bytes.Equal(p[:4], camMagic) {
		return 0, 0, 0, false
	}
	field1 = le32(p[8:12])
	seq = le32(p[24:28])
	method = le32(p[32:36])
	return field1, seq, method, true
}

// extractJSON 取 payload 中 data 段（偏移40 起，data 长度在偏移36）用于日志。
func extractJSON(p []byte) string {
	if len(p) < 40 {
		return ""
	}
	dl := int(le32(p[36:40]))
	if dl < 0 || 40+dl > len(p) {
		return ""
	}
	return string(p[40 : 40+dl])
}

// buildTLVResponse 构造对 method 请求的应答帧：[4B BE len] + magic + [2,1,seq,method] + JSON。
func buildTLVResponse(seq, method uint32) []byte {
	json := []byte(`{"code":200}`)
	if method == 100 {
		json = []byte(`{"code":200,"w":1280,"h":720,"rot":0}`)
	}
	payload := make([]byte, 0, 64+len(json))
	payload = append(payload, camMagic...)
	payload = appendTLVInt(payload, 2) // field1=2 → 应答
	payload = appendTLVInt(payload, 1)
	payload = appendTLVInt(payload, seq)
	payload = appendTLVInt(payload, method)
	payload = appendTLVData(payload, json)

	out := make([]byte, 4+len(payload))
	binary.BigEndian.PutUint32(out[:4], uint32(len(payload)))
	copy(out[4:], payload)
	return out
}

// reapAudio 在 audio-only 路结束后清理自己的 handle 登记（若仍是当前那一路）。
func (cp *CameraPusher) reapAudio(h *runHandle) {
	cp.mu.Lock()
	if cp.audio == h {
		cp.audio = nil
	}
	cp.mu.Unlock()
}

// sendHandshake 发送设备握手包
func (cp *CameraPusher) sendHandshake(conn net.Conn) error {
	hs := make([]byte, 0, 64)
	hs = append(hs, 0x89, 0x87, 0x67, 0x65) // inner magic
	hs = appendTLVInt(hs, 1)
	hs = appendTLVInt(hs, 1)
	hs = appendTLVInt(hs, 1)
	hs = appendTLVInt(hs, 2)
	hs = appendTLVData(hs, []byte("hello"))

	var hdr [4]byte
	binary.BigEndian.PutUint32(hdr[:], uint32(len(hs)))
	pkt := append([]byte{0x89, 0x87, 0x67, 0x65}, hdr[:]...)
	pkt = append(pkt, hs...)
	_, err := conn.Write(pkt)
	return err
}

// injectAudio 启动 ffmpeg 采集麦克风 PCM 并注入到 TCP 流。
// stop 关闭即停止采集并退出（由所属路 handle 提供）。
func (cp *CameraPusher) injectAudio(remote net.Conn, writeMu *sync.Mutex, micName string, stop <-chan struct{}) {
	ffmpegPath := cp.ffmpegExe()
	if _, err := os.Stat(ffmpegPath); err != nil {
		logf("[cam] no ffmpeg for audio: %v", err)
		return
	}

	args := []string{
		"-y", "-f", "dshow",
		"-i", "audio=" + micName,
		"-vn",
		"-acodec", "pcm_s16le",
		"-ar", "44100", "-ac", "1",
		"-f", "s16le", "pipe:1",
	}
	cmd := exec.Command(ffmpegPath, args...)
	cmd.Dir = filepath.Dir(ffmpegPath)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		logf("[cam] audio pipe failed: %v", err)
		return
	}
	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf
	if err := cmd.Start(); err != nil {
		logf("[cam] audio ffmpeg start failed: %v", err)
		return
	}
	defer cmd.Process.Kill()

	logf("[cam] audio injection started (mic=%s)", micName)

	var seq uint32
	chunk := make([]byte, 2048)
	for {
		select {
		case <-stop:
			return
		default:
		}
		n, err := io.ReadFull(stdout, chunk)
		if err != nil {
			stderrTail := strings.TrimSpace(stderrBuf.String())
			if len(stderrTail) > 200 {
				stderrTail = stderrTail[len(stderrTail)-200:]
			}
			logf("[cam] audio read: %v stderr=%s", err, stderrTail)
			return
		}
		if n > 0 {
			writeMu.Lock()
			cp.sendAudioFrame(remote, chunk[:n], &seq)
			writeMu.Unlock()
		}
	}
}
