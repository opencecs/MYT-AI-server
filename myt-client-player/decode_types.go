package main

import (
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"
)

const (
	PixelFormatBGRA = 1
)

// DecodeResult holds the decoded frame dimensions.
type DecodeResult struct {
	Width       int32
	Height      int32
	BGRA        uintptr
	Size        int
	IsTexture   bool    // true = GPU texture path, false = BGRA CPU buffer
	SubIdx      int     // D3D11VA texture array index (valid when IsTexture=true; macOS always 0)
	FrameHandle uintptr // 零拷贝路径：独立 AVFrame 句柄，UnrefFrame 按此释放
}

// VideoFrame represents one decoded video frame.
type VideoFrame struct {
	Width      int32
	Height     int32
	Format     uint32 // PixelFormatBGRA = 1
	BGRA       DecodeResult
	Data       []byte
	PTS        time.Duration
	releaseRef func() // called after the frame is consumed by the renderer
}

// InputHandler forwards input events to the App.
type InputHandler interface {
	OnMouseEvent(msg []byte)
	OnKeyboardEvent(msg []byte)
}

// inputScaler translates mouse coordinates from window space to source space.
type inputScaler interface {
	ScaleMouseCoords(mx, my int32) (sx, sy uint16, srcW, srcH int32, ok bool)
	SourceSize() (int32, int32)
	LBGeo() *letterboxGeo
	IsRotated() bool
}

// frameRenderer receives decoded video frames and renders them.
type frameRenderer interface {
	SendFrame(frame *VideoFrame)
	SetOverlay(text string)
}

// letterboxGeo holds the letterbox geometry for aspect-ratio preservation.
type letterboxGeo struct {
	drawW int32
	drawH int32
	offX  int32
	offY  int32
	winW  int32
	winH  int32
}

var framePool = sync.Pool{New: func() any { return new(VideoFrame) }}

// CopyBGRA copies decoded BGRA data from the C buffer into dst.
func CopyBGRA(dst []byte, res DecodeResult) {
	if res.BGRA == 0 || res.Size == 0 {
		return
	}
	n := min(len(dst), res.Size)
	src := unsafe.Slice((*byte)(unsafe.Pointer(res.BGRA)), n)
	copy(dst, src)
}

// greenDiagCount 限制绿闪诊断日志只打印前 5 帧
var greenDiagCount atomic.Int32

// renderTexLogCount 限制 RenderSlot 调用日志只打印前 5 次
var renderTexLogCount atomic.Int32


// badFrameLogCount 限制 UV=0 坏帧日志只打印前 5 次（+每 100 次）
var badFrameLogCount atomic.Int32

// greenDiagWrite 绿闪诊断独立日志（prod 版 logf 为空，此函数直接写文件，用于线上定位绿闪）
func greenDiagWrite(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	os.MkdirAll("logs", 0o755)
	f, err := os.OpenFile("logs/green_diag.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	f.WriteString(msg + "\n")
}

// decodeLogOnce ensures first-frame log prints only once.
var decodeLogOnce atomic.Bool
