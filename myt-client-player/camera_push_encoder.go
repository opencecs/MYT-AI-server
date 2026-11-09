//go:build windows

package main

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// camEncoder 自研推流器（推流唯一路径，WindowsPusher 已退役）。
// 数据流：ffmpeg(dshow MJPEG 1280x720 采集 → full-range 数值直通 → libx264)
//
//	→ Annex-B stdout → AU 拆分 → TLV 组帧 → 设备 camera TCP 口
//
// 协议时序按真实抓包复刻（2026-08-28 假设备 5MB dump）：
//
//	hello(seq=1, 等 ack 'f' 5s) → ffmpeg 启动 → SPS/PPS 消息(seq=2) →
//	设备 RPC100 应答(真实分辨率)+补发 SPS/PPS(seq=3) → 帧消息(seq≥4)
//
// 色彩（WP 复刻结论）：WP 的 sws_getContext(dst=yuv420p) 因 yuvj420p→yuv420p
// 同布局走直通、不压缩 range（高亮直铺到 255，直方图实证），输出 = full 数值
// + yuv420p 编码 + 无色彩 VUI；legacy 模式用 format=yuv420p 复刻同行为，
// 并在 Go 侧剥掉 x264 传播的 full-range VUI（stripSpsVui）。
type camEncoder struct {
	cp      *CameraPusher
	conn    net.Conn
	writeMu sync.Mutex
	seq     int32 // 我方消息 seq 计数器（hello 占 1，SPS/PPS 起从 2）

	helloCh chan struct{} // hello ack 校验通过后 close
	sizeCh  chan struct{} // 从 stderr 解析到摄像头真实分辨率后 close

	mu     sync.Mutex
	spsPPS []byte // 缓存 Annex-B SPS+PPS（RPC100 应答时补发）
	camW   int32
	camH   int32

	// chromaMode 色彩模式，默认 legacy（= WindowsPusher 真实复刻，全量对齐基准）：
	// legacy  = 1280x720 + full-range 数值直通（不压缩）+ 剥掉色彩 VUI——
	//           抓包实证的 WP 原始输出（旧"limited 压缩"结论有误）；
	// full    = full-range 直通 + full VUI（读 VUI 的镜像按 full 解释）；
	// limited = 混合流（Y full + UV 压 16-240）——个别镜像的逃生门。
	// 可经 %TEMP%/myt-cam/chroma_<ip>_<port>.txt 按坑位覆盖，见 loadChromaMode。
	chromaMode string
}

// loadChromaMode 读色彩模式覆盖，优先级：
// %TEMP%/myt-cam/chroma_<ip>_<port>.txt（按坑位） > chroma_<ip>.txt > chroma.txt > 默认 legacy。
// 必须带端口：一台宿主机多个坑位共享同一 IP，仅按 IP 区分不了坑位。
func (e *camEncoder) loadChromaMode(deviceIP string, tcpPort int) {
	e.chromaMode = "legacy"
	base := filepath.Join(os.TempDir(), "myt-cam")
	port := strconv.Itoa(tcpPort)
	for _, name := range []string{
		"chroma_" + deviceIP + "_" + port + ".txt",
		"chroma_" + deviceIP + ".txt",
		"chroma.txt",
	} {
		if b, err := os.ReadFile(filepath.Join(base, name)); err == nil {
			m := strings.TrimSpace(string(b))
			if m == "legacy" || m == "full" || m == "limited" {
				e.chromaMode = m
				break
			}
		}
	}
	logf("[enc] chroma mode=%s (device %s:%s)", e.chromaMode, deviceIP, port)
}

// encoderFFmpegArgs 构造 dshow 采集 + libx264 编码参数。
// 编码参数对齐 WindowsPusher（baseline/ultrafast/zerolatency/ABR/LOW_DELAY），
// 差异：repeat-headers + slices=1 + 按 chromaMode 选择色彩模式
// （legacy=WP 复刻 full数值+无VUI 为默认；full/limited 见 loadChromaMode）。
// 固定请求 -video_size 1280x720（WP 时代即此分辨率，容器相机管线按 720p 配置）；
// 不能传 -framerate：720p MJPEG 档只有 25/30fps，传 10 会 I/O error 秒退
// （实测踩坑；WP 内嵌 ffmpeg 3.4 对帧率协商宽松所以当年没炸）。
// 真实分辨率从 stderr 解析后经 RPC100 回报设备（WP 同款行为）。
// chromaMode=limited 时经 lut 滤镜把色度压到 16-240（混合流）。
func (e *camEncoder) encoderFFmpegArgs(camName string, bitrate int) []string {
	in := "video=0"
	if camName != "" {
		in = "video=" + camName
	}
	args := []string{
		"-hide_banner", "-loglevel", "info", "-nostats",
		"-f", "dshow",
		"-vcodec", "mjpeg",
		"-video_size", "1280x720",
		"-i", in,
		"-an",
		"-c:v", "libx264",
		"-profile:v", "baseline",
		"-preset", "ultrafast", "-tune", "zerolatency",
	}
	// 色彩模式（ffmpeg 7.1 的 colorspace 枚举名是 smpte170m，bt601 别名不被接受）：
	switch e.chromaMode {
	case "full":
		// full-range 直通 + full VUI
		args = append(args, "-pix_fmt", "yuv420p", "-color_range", "pc",
			"-colorspace", "smpte170m", "-color_primaries", "smpte170m", "-color_trc", "smpte170m")
	case "limited":
		// 混合流：亮度 full 直通 + 色度压到 16-240，标 full VUI
		args = append(args, "-pix_fmt", "yuv420p", "-color_range", "pc",
			"-colorspace", "smpte170m", "-color_primaries", "smpte170m", "-color_trc", "smpte170m")
	default:
		// legacy（WP 复刻）：数值保持 full 直通——format 滤镜做同布局 yuvj420p→yuv420p
		// 转换不做 range 压缩（与 WP 的 sws_getContext(dst=yuv420p) 实测行为一致，
		// 高亮直铺到 255）；x264 写入的 full-range VUI 由 sendSPSPPS 在 Go 侧剥掉，
		// 最终流与 WP 逐字节同构
		args = append(args, "-vf", "format=yuv420p", "-pix_fmt", "yuv420p")
	}
	args = append(args,
		"-g", "30", // GOP≈1s（按 30fps 原生帧率；25/24fps 摄像头也适用）
		"-bf", "0",
		"-flags", "+low_delay",
		// repeat-headers: 每个 IDR 前重发 SPS/PPS（对齐原程序）
		// slices=1 + sliced_threads=0: 每帧仅 1 个 slice NAL（ultrafast 默认
		// sliced_threads=1 会把一帧切成 3 个 slice NAL 且覆盖 slices 值）
		// threads=2: 显式限制 x264 frame-level 线程数。不设则 threads=auto
		// （12 核开到 18 线程），x264 流水线里每帧要等前面 threads-1 帧编码完
		// 才出流，33ms/帧 × 17 帧缓冲 ≈ 640ms 恒定滞后（实测）；threads=2
		// 时流水线深度仅 1 帧（33ms）。单线程编码仅耗 0.23 核，吞吐余量充足。
		// zerolatency 里 sliced_threads 关不掉这个滞后——threads 是 ffmpeg
		// 层传给 x264 的参数，sliced_threads 只影响 x264 内部。也不能改回
		// sliced_threads=1 来"用多线程"：auSplitter 会把 slice NAL 当独立帧，
		// 多 slice 会成残帧。
		"-x264-params", "repeat-headers=1:slices=1:sliced_threads=0:threads=2",
		"-b:v", fmt.Sprintf("%dk", bitrate),
	)
	// 混合流（chromaMode=limited）：亮度保持 full 直通，色度压到 16-240。
	// -vf 是输出选项，须位于 "-f h264 pipe:1" 之前。
	if e.chromaMode == "limited" {
		lut := "lut=y='val':u='clip((val-128)*224/255+128,16,240)':v='clip((val-128)*224/255+128,16,240)'"
		args = append(args, "-vf", lut)
	}
	args = append(args, "-f", "h264", "pipe:1")
	return args
}

// videoLoopEncoder 自研 ffmpeg 推流路：直连设备（无代理）。
func (cp *CameraPusher) videoLoopEncoder(h *runHandle, target camTarget, intentional *bool) {
	addr := net.JoinHostPort(cp.deviceIP, strconv.Itoa(cp.tcpPort))
	logf("[enc] connecting to %s (src=%s cam=%s mic=%s)", addr, srcLabel(target.src), target.cam, target.mic)
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		logf("[enc] device connect failed: %v", err)
		return
	}
	if tcp, ok := conn.(*net.TCPConn); ok {
		tcp.SetNoDelay(true)
	}
	e := &camEncoder{cp: cp, conn: conn, seq: 1}
	e.helloCh = make(chan struct{})
	e.sizeCh = make(chan struct{})
	e.loadChromaMode(cp.deviceIP, cp.tcpPort)
	go e.serveDevice(h.stop)

	// hello（[1,1,seq=1,2] + "hello"）+ 等 ack（首字节 'f'）
	if err := cp.sendHandshake(conn); err != nil {
		logf("[enc] handshake failed: %v", err)
		conn.Close()
		return
	}
	select {
	case <-e.helloCh:
	case <-time.After(5 * time.Second):
		logf("[enc] hello ack timeout")
		conn.Close()
		return
	}

	ffPath := cp.ffmpegExe()
	if _, err := os.Stat(ffPath); err != nil {
		logf("[enc] no ffmpeg for video: %v", err)
		conn.Close()
		return
	}
	bitrate := 2000
	if target.src == camSrcWindowRect {
		// screenWindowRect：按窗口矩形裁剪桌面后归一化到 1280x720 推流。
		// ⚠️ 不能按窗口矩形定格分辨率：设备端相机注入管线固定按 1280x720 建
		// ImageReader/HAL 流（native_startTcpPush 1280x720），非标准分辨率
		// （实测 796x940）解码帧进不了相机队列，readCameraStream 一次都不触发
		// → 相机应用无画面（2026-11-02 设备 logcat 实证）。故与 screenDevice
		// 等伪源同口径：RPC100 回报 1280x720，帧泵归一化补边。
		// ffmpeg rawvideo 的 -s 恒 1280x720；窗口 resize 只影响裁剪矩形（帧泵
		// 自适应），仍保留 restartVideo 以重定格裁剪会话（<1s 闪断可接受）。
		if winRectStart(cp.app.PlayerHwnd()) == nil {
			logf("[enc] winrect session start failed")
			conn.Close()
			return
		}
		e.mu.Lock()
		e.camW, e.camH = rawPushW, rawPushH
		e.mu.Unlock()
		close(e.sizeCh)
		bitrate = 3500 // 保 QR 码锐度（伪源画面复杂，2000k 会糊掉二维码）
		args := rawvideoFFmpegArgs(rawPushW, rawPushH, bitrate, e.chromaMode)
		if err := startRawvideoFFmpeg(cp, ffPath, args, h); err != nil {
			logf("[enc] winrect ffmpeg start failed: %v", err)
			conn.Close()
			return
		}
		wrCh := make(chan struct{}, 1)
		go cp.winRectFramePump(h.stop, h.stdin, wrCh)
		go cp.watchWinRectResize(h.stop, h, wrCh)
	} else if isPseudoSrc(target.src) {
		// 伪源：rawvideo 管线。尺寸已知，先置 camW/camH 再启动 ffmpeg，
		// RPC100 无需等 stderr 解析即可应答真实分辨率。
		w, hh := screenPushSize(target.src)
		e.mu.Lock()
		e.camW, e.camH = int32(w), int32(hh)
		e.mu.Unlock()
		close(e.sizeCh)
		bitrate = 3500 // 保 QR 码锐度（伪源画面复杂，2000k 会糊掉二维码）
		args := rawvideoFFmpegArgs(w, hh, bitrate, e.chromaMode)
		if err := startRawvideoFFmpeg(cp, ffPath, args, h); err != nil {
			logf("[enc] rawvideo ffmpeg start failed: %v", err)
			conn.Close()
			return
		}
		// 直接传 stdin 管道：不读 cp.video（stop 通道由 runHandle 统一管理），
		// 帧泵不再无锁读共享字段，消除竞态。
		go cp.screenFramePump(h.stop, h.stdin, target.src, w, hh)
	} else if err := startDshowFFmpeg(cp, ffPath, e.encoderFFmpegArgs(target.cam, bitrate), e, h); err != nil {
		conn.Close()
		return
	}
	logf("[enc] ffmpeg PID=%d src=%s bitrate=%dk", h.cmd.Process.Pid, srcLabel(target.src), bitrate)
	if target.mic != "" {
		go cp.injectAudio(conn, &e.writeMu, target.mic, h.stop)
	}

	// 主循环：Annex-B → AU 拆分 → TLV 发送（h.stdout 由两种启动分支填充）
	sp := &auSplitter{}
	buf := make([]byte, 1<<16)
	for {
		n, rerr := h.stdout.Read(buf)
		if n > 0 {
			sp.feed(buf[:n], e.sendSPSPPS, e.sendFrame)
		}
		if rerr != nil {
			if rerr != io.EOF {
				logf("[enc] ffmpeg stdout read: %v", rerr)
			}
			sp.flush(e.sendSPSPPS, e.sendFrame) // EOF：发出残留的最后一个 NAL
			break
		}
	}
	h.cmd.Wait()
	conn.Close()
	select {
	case <-h.stop:
		*intentional = true
	default:
		logf("[enc] ffmpeg exited unexpectedly")
	}
}

// watchWinRectResize screenWindowRect 的 resize 联动：帧泵向 wrCh 发请求时，
// 停掉本路并用两步 apply 重启（ffmpeg -s 启动定格，尺寸变化必须重启 ffmpeg）。
// 两步走是因为 apply 对「desired 未变」会早退——先 apply 空 src（仅停），
// 再 apply 完整目标（重起，videoLoopEncoder 入口会重新 winRectStart 定格新尺寸）。
func (cp *CameraPusher) watchWinRectResize(stop <-chan struct{}, h *runHandle, wrCh <-chan struct{}) {
	select {
	case <-stop:
		return
	case <-wrCh:
	}
	// 确认仍是 winrect 源在跑（防与停止/切源竞争）
	cp.mu.Lock()
	same := cp.video == h && cp.desired.src == camSrcWindowRect
	var target camTarget
	if same {
		target = cp.desired
	}
	cp.mu.Unlock()
	if !same {
		return
	}
	w, hh := 0, 0
	if s := wrSessionPtr.Load(); s != nil {
		w, hh = int(s.outW), int(s.outH)
	}
	logf("[enc] winrect resize detected (now %dx%d) — restarting video loop", w, hh)
	// 先停帧泵会话（旧输出尺寸已失效），再 intentional 停本路
	winRectStop()
	h.requestStop()
	go func() {
		waitDone(h.done, 3*time.Second)
		cp.apply(camTarget{mic: target.mic}) // 仅停（清 active，绕开早退）
		cp.apply(target)                     // 以新尺寸重起
	}()
}

// serveDevice 设备消息唯一读协程：hello ack 校验 + RPC 请求应答。
// 报文：[4B BE len][payload]，payload = magic + 4×TLVInt + TLVData。
// int1=2 是设备对我方同步消息的应答；int1=1 是设备主动 RPC 请求。
func (e *camEncoder) serveDevice(stopCh <-chan struct{}) {
	br := bufio.NewReaderSize(e.conn, 4096)
	for {
		select {
		case <-stopCh:
			return
		default:
		}
		hdr := make([]byte, 4)
		if _, err := io.ReadFull(br, hdr); err != nil {
			return
		}
		n := binary.BigEndian.Uint32(hdr)
		if n == 0 {
			continue // 空心跳
		}
		if n > 65536 {
			logf("[enc] rpc bad frame len=%d, abort reader", n)
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
		switch {
		case field1 == 2 && method == 2:
			// hello ack：只校验 data 首字节 'f'（原程序同款逻辑）
			if j := extractJSON(payload); j != "" && j[0] == 'f' {
				logf("[enc] hello ack ok")
				close(e.helloCh)
			} else {
				logf("[enc] hello ack invalid: %q", j)
			}
		case field1 == 2 && method == 1:
			// SPS/PPS 消息的设备应答（原程序以 f2=1 同步等待此应答，设备必回）
			logf("[enc] sps/pps device ack seq=%d", seq)
		case field1 == 1:
			e.handleRPC(seq, method)
		}
	}
}

// handleRPC 应答设备 RPC 请求。
// 100=开摄像头：应答摄像头真实分辨率（rot 恒 0）+ 补发缓存 SPS/PPS
// （原程序行为，抓包中 seq=3 的 SPS/PPS 即来源于此）。
// 101/102：应答 {"code":200}。注：原程序 102 落空串分支不应答，
// 我方保留应答以 arm 设备音频注入通道（纯音频路既有修复，风险低）。
func (e *camEncoder) handleRPC(seq, method uint32) {
	logf("[enc] device rpc method=%d seq=%d", method, seq)
	switch method {
	case 100:
		e.waitForSize(2 * time.Second)
		e.mu.Lock()
		w, hh := e.camW, e.camH
		cached := make([]byte, len(e.spsPPS))
		copy(cached, e.spsPPS)
		e.mu.Unlock()
		if w <= 0 || hh <= 0 {
			w, hh = 1280, 720 // dshow 未回报时的兜底（请求值）
		}
		e.writeRPCReply(seq, method, fmt.Sprintf(`{"code":200,"w":%d,"h":%d,"rot":0}`, w, hh))
		if len(cached) > 8 {
			e.sendSPSPPS(cached)
		}
	case 101, 102:
		e.writeRPCReply(seq, method, `{"code":200}`)
	}
}

func (e *camEncoder) waitForSize(dur time.Duration) {
	select {
	case <-e.sizeCh:
	case <-time.After(dur):
	}
}

// writeRPCReply 构造 RPC 应答：[4B BE len] + magic + [2,1,seq,method] + JSON。
func (e *camEncoder) writeRPCReply(seq uint32, method uint32, jsonStr string) {
	payload := make([]byte, 0, 64+len(jsonStr))
	payload = append(payload, camMagic...)
	payload = appendTLVInt(payload, 2) // int1=2 → 应答
	payload = appendTLVInt(payload, 1)
	payload = appendTLVInt(payload, seq)
	payload = appendTLVInt(payload, method)
	payload = appendTLVData(payload, []byte(jsonStr))
	out := make([]byte, 4+len(payload))
	binary.BigEndian.PutUint32(out[:4], uint32(len(payload)))
	copy(out[4:], payload)
	e.writeMu.Lock()
	_, err := e.conn.Write(out)
	e.writeMu.Unlock()
	if err != nil {
		logf("[enc] rpc reply write: %v", err)
	}
}

// watchStderr 从 ffmpeg stderr 解析摄像头真实分辨率（Stream #0:0 行的 WxH），
// 供 RPC100 应答使用（原程序 w/h = codecpar 真实分辨率，非 --width）。
//
// 解析到分辨率后必须持续消费到 EOF，不能提前 return：Windows 管道缓冲
// 约 64KB，info 级 stderr 流量虽小但长会话必满，管道一满 ffmpeg 写入
// 阻塞、整个推流卡死。提前 return 还会丢失 ffmpeg 崩溃时的错误原因。
// 已加 -nostats 消掉 -stats 的 \r 进度行流量，stderr 流量降到接近零。
var streamSizeRe = regexp.MustCompile(`Stream #\d+:\d+[^0-9]*?(\d{3,4})x(\d{3,4})`)

func (e *camEncoder) watchStderr(r io.Reader) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	sizeFound := false
	for sc.Scan() {
		line := sc.Text()
		if !sizeFound {
			if m := streamSizeRe.FindStringSubmatch(line); m != nil {
				w, _ := strconv.Atoi(m[1])
				hh, _ := strconv.Atoi(m[2])
				e.mu.Lock()
				e.camW, e.camH = int32(w), int32(hh)
				e.mu.Unlock()
				logf("[enc] camera real size %dx%d", w, hh)
				close(e.sizeCh)
				sizeFound = true
			}
		}
		if strings.Contains(line, "Error") || strings.Contains(line, "error") {
			logf("[enc] ffmpeg stderr: %s", line)
		}
	}
}

// sendMessage 发一条我方数据消息：[4B BE len] + magic + [1, f2, seq, f4] + TLVData(inner)。
// f2=0（数据消息，不等应答），f4=1。
func (e *camEncoder) sendMessage(f2, f4 uint32, inner []byte) {
	seq := uint32(atomic.AddInt32(&e.seq, 1))
	payload := make([]byte, 0, 44+len(inner))
	payload = append(payload, camMagic...)
	payload = appendTLVInt(payload, 1)
	payload = appendTLVInt(payload, f2)
	payload = appendTLVInt(payload, seq)
	payload = appendTLVInt(payload, f4)
	payload = appendTLVData(payload, inner)
	out := make([]byte, 4+len(payload))
	binary.BigEndian.PutUint32(out[:4], uint32(len(payload)))
	copy(out[4:], payload)
	e.writeMu.Lock()
	_, err := e.conn.Write(out)
	e.writeMu.Unlock()
	if err != nil {
		logf("[enc] send seq=%d failed: %v", seq, err)
	}
}

// sendInner 构造 inner = magic + [1, kind] + blockLen(LE) + block 并发送。
// kind: 1=SPS/PPS 消息, 0=帧消息（与抓包一致）。
// 外层 f2 恒 0——WP 真实抓包（2026-08-28 假设备 5MB dump）实证 SPS/PPS 外层也是
// [1,0,seq,1] 异步，RE 文档 §3.5 的 f2=1 推断有误；设备对两种都接受。
func (e *camEncoder) sendInner(kind uint32, block []byte) {
	inner := make([]byte, 0, 24+len(block))
	inner = append(inner, camMagic...)
	inner = appendTLVInt(inner, 1)
	inner = appendTLVInt(inner, kind)
	var bl [4]byte
	binary.LittleEndian.PutUint32(bl[:], uint32(len(block)))
	inner = append(inner, bl[:]...)
	inner = append(inner, block...)
	e.sendMessage(0, 1, inner)
}

func (e *camEncoder) sendSPSPPS(block []byte) {
	if e.chromaMode == "legacy" {
		// WP 复刻：剥掉 ffmpeg 7.1 wrapper 从 yuvj 输入传播进来的 full-range VUI，
		// 使 SPS 与 WP 的 x264 输出一致（无任何色彩 VUI）
		if nb := stripSpsVuiBlock(block); len(nb) != 0 && !bytes.Equal(nb, block) {
			block = nb
		}
	}
	e.mu.Lock()
	if len(e.spsPPS) == 0 {
		e.spsPPS = append([]byte(nil), block...)
	}
	e.mu.Unlock()
	e.sendInner(1, block)
}

// stripSpsVuiBlock 把 SPS/PPS block 里 SPS NAL 的色彩 VUI 剥掉（其余原样）。
func stripSpsVuiBlock(block []byte) []byte {
	if len(block) < 8 {
		return block
	}
	sc := 3 // 000001
	if block[2] != 1 {
		if len(block) < 9 || block[3] != 1 {
			return block
		}
		sc = 4 // 00000001
	}
	if block[sc]&0x1f != 7 {
		return block
	}
	pos, _ := findStartCode(block, sc+1)
	if pos < 0 {
		return block
	}
	orig := block[sc+1 : pos]
	newSPS := stripSpsVui(orig)
	if len(newSPS) == 0 || bytes.Equal(newSPS, orig) {
		return block
	}
	out := make([]byte, 0, len(block)+8)
	out = append(out, block[:sc+1]...)
	out = append(out, newSPS...)
	out = append(out, block[pos:]...)
	return out
}

func (e *camEncoder) sendFrame(block []byte) {
	e.sendInner(0, block)
}
