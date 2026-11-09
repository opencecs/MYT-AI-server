//go:build windows

package main

// camera_push_screen.go — 伪源推流（扫码功能）：
// screenDevice = 播放器解码帧（完整设备画面，不受窗口遮挡/最小化影响，captureRendererFrame 抓取）；
// screenDesktop = 电脑桌面（GDI BitBlt 抓屏）。
//
// 一条管线：帧泵以 15fps 把 BGRA 帧写进 ffmpeg stdin（-f rawvideo），
// 编码输出/握手/RPC 与摄像头路完全共用（见 camera_push_encoder.go）。
// 帧尺寸归一化在 Go 侧完成（缩放 + 非比例补黑边居中），内置 ffmpeg 无 pad 滤镜负担；
// QR 对旋转免疫，旋转帧不做转正。

import (
	"bufio"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

// rawPushW/rawPushH 推流目标尺寸：与 dshow 路的 1280x720 一致
//（容器相机管线按 720p 配置，RPC100 也按此口径回报）。
const (
	rawPushW = 1280
	rawPushH = 720
)

// screenPushSize 返回伪源的推流尺寸（编码参数与帧泵共同约定）。
func screenPushSize(src string) (int, int) {
	return rawPushW, rawPushH
}

// srcLabel 伪源的可读名（日志用）。
func srcLabel(src string) string {
	switch src {
	case camSrcDevice:
		return "screenDevice"
	case camSrcDesktop:
		return "screenDesktop"
	case camSrcWindow:
		return "screenWindow"
	case camSrcWindowRect:
		return "screenWindowRect"
	default:
		return "cam"
	}
}

// rawvideoFFmpegArgs 构造 rawvideo 管线输入 + 编码参数。
// 与 dshow 路共用编码段（baseline/ultrafast/zerolatency/色彩模式/GOP），
// 差异：输入为 BGRA 管线、15fps 固定帧率、尺寸已知（stderr 只做错误日志）。
func rawvideoFFmpegArgs(w, h, bitrate int, chromaMode string) []string {
	args := []string{
		"-hide_banner", "-loglevel", "info", "-nostats",
		"-f", "rawvideo",
		"-pixel_format", "bgra",
		"-video_size", fmt.Sprintf("%dx%d", w, h),
		"-framerate", "15",
		"-i", "pipe:0",
		"-an",
		"-c:v", "libx264",
		"-profile:v", "baseline",
		"-preset", "ultrafast", "-tune", "zerolatency",
	}
	switch chromaMode {
	case "full":
		args = append(args, "-pix_fmt", "yuv420p", "-color_range", "pc",
			"-colorspace", "smpte170m", "-color_primaries", "smpte170m", "-color_trc", "smpte170m")
	default:
		// legacy/limited：伪源数值即 PC 全 range，保持直通 + 剥 VUI（同 dshow legacy）
		args = append(args, "-vf", "format=yuv420p", "-pix_fmt", "yuv420p")
	}
	args = append(args,
		"-g", "30",
		"-bf", "0",
		"-flags", "+low_delay",
		"-x264-params", "repeat-headers=1:slices=1:sliced_threads=0:threads=2",
		"-b:v", fmt.Sprintf("%dk", bitrate),
	)
	if chromaMode == "limited" {
		lut := "lut=y='val':u='clip((val-128)*224/255+128,16,240)':v='clip((val-128)*224/255+128,16,240)'"
		args = append(args, "-vf", lut)
	}
	args = append(args, "-f", "h264", "pipe:1")
	return args
}

// startRawvideoFFmpeg 启动 rawvideo ffmpeg：stdin 供帧泵写 BGRA，stdout 由
// videoLoopEncoder 读（Annex-B），stderr 仅记错误（尺寸已知不解析分辨率）。
func startRawvideoFFmpeg(cp *CameraPusher, ffPath string, args []string, h *runHandle) error {
	cmd := exec.Command(ffPath, args...)
	cmd.Dir = filepath.Dir(ffPath)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	h.cmd = cmd
	h.stdout = stdout
	h.stdin = stdin
	go cp.watchRawStderr(stderr)
	go func() { <-h.stop; cmd.Process.Kill() }()
	return nil
}

// startDshowFFmpeg dshow 路（现状行为抽平）：stdout 给主循环、stderr 解析分辨率。
func startDshowFFmpeg(cp *CameraPusher, ffPath string, args []string, e *camEncoder, h *runHandle) error {
	cmd := exec.Command(ffPath, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		logf("[enc] ffmpeg start failed: %v", err)
		return err
	}
	h.cmd = cmd
	h.stdout = stdout
	go e.watchStderr(stderr)
	go func() { <-h.stop; cmd.Process.Kill() }()
	return nil
}

// watchRawStderr rawvideo 路 stderr 消费：只记错误行。必须持续读到 EOF
//（Windows 管道缓冲 ~64KB，不消费会卡死 ffmpeg，见 watchStderr 注释）。
func (cp *CameraPusher) watchRawStderr(r io.Reader) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if strings.Contains(line, "Error") || strings.Contains(line, "error") {
			logf("[enc] rawvideo stderr: %s", line)
		}
	}
}

// screenFramePump 伪源帧泵：15fps 抓帧 → 归一化 → BGRA 写 ffmpeg stdin。
// 写入随管道自然背压（rawvideo 消费速度远快于 15fps 输入，正常不积压）；
// 停止信号到达即退出，ffmpeg 由 runHandle 的 stop goroutine Kill。
func (cp *CameraPusher) screenFramePump(stop <-chan struct{}, stdin io.Writer, src string, w, h int) {
	logf("[cam] screen pump started src=%s %dx%d", srcLabel(src), w, h)
	ticker := time.NewTicker(time.Second / 15)
	defer ticker.Stop()
	if stdin == nil {
		logf("[cam] screen pump: no stdin")
		return
	}
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
		}
		bgra, fw, fh, _ := cp.grabScreenFrame(src)
		if bgra == nil || fw <= 0 || fh <= 0 {
			continue
		}
		out := normalizeBGRA(bgra, int(fw), int(fh), w, h)
		if out == nil {
			continue
		}
		if _, err := stdin.Write(out); err != nil {
			logf("[cam] stdin write: %v", err)
			return
		}
	}
}

// grabScreenFrame 按源抓一帧 BGRA。
// screenDevice：captureRendererFrame（软解 BGRA 帧或硬解纹理回读，d3d11_frame.go）。
// screenDesktop：GDI BitBlt 抓桌面。
// screenWindow：WGC CreateForWindow 抓被遮挡窗口（camera_push_wgc.go；自动选
// 桌面 z 序中盖住播放器的最近窗口，找不到返回 0 → 停帧）。
func (cp *CameraPusher) grabScreenFrame(src string) (bgra []byte, w, h int32, rotated bool) {
	if src == camSrcDevice {
		return cp.app.captureRendererFrame()
	}
	if src == camSrcWindow {
		hwnd := cp.app.PlayerHwnd()
		if hwnd == 0 {
			return nil, 0, 0, false
		}
		target := findOccludedWindow(hwnd)
		if target == 0 {
			return nil, 0, 0, false
		}
		b, bw, bh := wgcGrabFrame(target)
		return b, bw, bh, false
	}
	b, bw, bh := captureDesktopBGRA()
	return b, bw, bh, false
}

// normalizeBGRA 把任意尺寸 BGRA 帧缩放 + 非比例补黑边居中到 dstW x dstH
//（BGRA 顶行在前，与 DIB top-down 一致）。缩放用最近邻——QR 码条形锐度优先
// 于平滑度；15fps 下性能开销可忽略。
func normalizeBGRA(src []byte, sw, sh, dstW, dstH int) []byte {
	if sw <= 0 || sh <= 0 || len(src) < sw*sh*4 {
		return nil
	}
	dst := make([]byte, dstW*dstH*4) // 零值 = 黑（alpha 编码忽略）
	scaleW := float64(dstW) / float64(sw)
	scaleH := float64(dstH) / float64(sh)
	scale := scaleW
	if scaleH < scale {
		scale = scaleH
	}
	dw := int(float64(sw) * scale)
	dh := int(float64(sh) * scale)
	if dw > dstW {
		dw = dstW
	}
	if dh > dstH {
		dh = dstH
	}
	offX := (dstW - dw) / 2
	offY := (dstH - dh) / 2
	for y := 0; y < dh; y++ {
		sy := int(float64(y) / scale)
		if sy >= sh {
			sy = sh - 1
		}
		srcRow := src[sy*sw*4:]
		dstRow := dst[(y+offY)*dstW*4:]
		for x := 0; x < dw; x++ {
			sx := int(float64(x) / scale)
			if sx >= sw {
				sx = sw - 1
			}
			copy(dstRow[(x+offX)*4:(x+offX)*4+4], srcRow[sx*4:sx*4+4])
		}
	}
	return dst
}

// captureDesktopBGRA GDI 抓整个虚拟桌面首屏（BitBlt 到 DIB section，BGRA 顶行在前）。
// WebView2/D3D 窗口在 BitBlt 下可能得到黑屏（已知 GDI 局限）——TODO: 如黑屏
// 占比过高可改 Windows.Graphics.Capture 或 DWMCapture 输出，留待实测决定。
func captureDesktopBGRA() ([]byte, int32, int32) {
	hdc, _, _ := procGetDC.Call(0)
	if hdc == 0 {
		return nil, 0, 0
	}
	defer procReleaseDC.Call(0, hdc)
	w, _, _ := procGetSystemMetrics.Call(SM_CXSCREEN)
	h, _, _ := procGetSystemMetrics.Call(SM_CYSCREEN)
	if w == 0 || h == 0 {
		return nil, 0, 0
	}
	w32, h32 := int32(w), int32(h)
	size := int(w32) * int(h32) * 4
	memDC, _, _ := procCreateCompatibleDC.Call(hdc)
	if memDC == 0 {
		return nil, 0, 0
	}
	defer procDeleteDC.Call(memDC)
	bmi := bitmapInfoHeader{
		Size:        uint32(unsafe.Sizeof(bitmapInfoHeader{})),
		Width:       w32,
		Height:      -h32, // top-down
		Planes:      1,
		BitCount:    32,
		Compression: 0,
		SizeImage:   uint32(size),
	}
	var pBits unsafe.Pointer
	hBmp, _, _ := procCreateDIBSection.Call(hdc, uintptr(unsafe.Pointer(&bmi)), 0,
		uintptr(unsafe.Pointer(&pBits)), 0, 0)
	if hBmp == 0 || pBits == nil {
		return nil, 0, 0
	}
	defer procDeleteObject.Call(hBmp)
	old, _, _ := procSelectObject.Call(memDC, hBmp)
	defer procSelectObject.Call(memDC, old)
	// SRCCOPY=0x00CC0020；不带 CAPTUREBLT（避免光标层闪烁，对扫码无影响）
	ret, _, _ := procBitBlt.Call(memDC, 0, 0, uintptr(w32), uintptr(h32), hdc, 0, 0, 0x00CC0020)
	if ret == 0 {
		return nil, 0, 0
	}
	buf := make([]byte, size)
	copy(buf, unsafe.Slice((*byte)(pBits), size))
	return buf, w32, h32
}
