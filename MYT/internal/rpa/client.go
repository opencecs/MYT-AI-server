package rpa

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"log"
	"time"

	adrpa "cnb.cool/openmyt/myt-sdk-golang/rpa"
)

// VLMMaxWidth VLM 截图最大宽边：540px 是 1080p 的一半
// 缩放到 540×1200 + JPEG-85 ≈ 39KB base64，适合大模型视觉理解
const VLMMaxWidth = 540

const defaultTimeout = 30 * time.Second

// Client RPA 客户端，封装 TCP 连接和 16 个方法
type Client struct {
	cli adrpa.RPA
	ip  string
}

// NewClient 创建 RPA 客户端并建立 TCP 连接
func NewClient(ip string, port int) (*Client, error) {
	cli, err := adrpa.New(ip, port)
	if err != nil {
		return nil, fmt.Errorf("RPA连接失败 %s:%d: %w", ip, port, err)
	}
	log.Printf("[rpa] 连接成功: %s:%d", ip, port)
	return &Client{cli: cli, ip: ip}, nil
}

// Close 关闭 TCP 连接
func (c *Client) Close() {
	c.cli.Close()
	log.Printf("[rpa] 连接关闭: %s", c.ip)
}

func (c *Client) ctx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), defaultTimeout)
}

// Screenshot 截图，返回 base64 编码的 PNG 图片
func (c *Client) Screenshot(quality int) (string, error) {
	if quality <= 0 {
		quality = 50
	}
	ctx, cancel := c.ctx()
	defer cancel()

	data, err := c.cli.TakeCaptureCompress(ctx, 0, quality)
	if err != nil {
		return "", fmt.Errorf("RPA截图失败: %w", err)
	}
	return base64.StdEncoding.EncodeToString(data), nil
}

// ScreenshotRaw 截图，返回原始 PNG 二进制
func (c *Client) ScreenshotRaw(quality int) ([]byte, error) {
	if quality <= 0 {
		quality = 50
	}
	ctx, cancel := c.ctx()
	defer cancel()

	data, err := c.cli.TakeCaptureCompress(ctx, 0, quality)
	if err != nil {
		return nil, fmt.Errorf("RPA截图失败: %w", err)
	}
	return data, nil
}

// ScreenshotForVLM 截图并缩放到 VLM 友好尺寸（最长边 540px, JPEG-85）
// 返回 data:image/jpeg;base64,... 格式，约 30-50KB，适合大模型视觉理解
// 自动调整 JPEG 质量/尺寸以确保 base64 不超过 50KB
func (c *Client) ScreenshotForVLM(quality int) (string, error) {
	raw, err := c.ScreenshotRaw(quality)
	if err != nil {
		return "", err
	}

	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		log.Printf("[rpa] PNG解码失败，返回原始截图: %v", err)
		return "data:image/png;base64," + base64.StdEncoding.EncodeToString(raw), nil
	}

	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()

	// 计算缩放比例（最长边缩到 VLMMaxWidth）
	scale := float64(VLMMaxWidth) / float64(w)
	if h > w {
		scale = float64(VLMMaxWidth) / float64(h)
	}
	targetW, targetH := w, h
	if scale < 1.0 {
		targetW = int(float64(w) * scale)
		targetH = int(float64(h) * scale)
	}

	resized := resizeImage(img, targetW, targetH)
	const maxBase64Len = 50000

	// 逐步降低 JPEG 质量直到 base64 不超限
	for q := 85; q >= 30; q -= 15 {
		var buf bytes.Buffer
		if err := jpeg.Encode(&buf, resized, &jpeg.Options{Quality: q}); err != nil {
			break
		}
		b64 := base64.StdEncoding.EncodeToString(buf.Bytes())
		if len(b64) <= maxBase64Len {
			log.Printf("[rpa] VLM截图: %s %dx%d→%dx%d q=%d (%.0fKB→%.0fKB)",
				c.ip, w, h, targetW, targetH, q,
				float64(len(raw))/1024, float64(buf.Len())/1024)
			return "data:image/jpeg;base64," + b64, nil
		}
	}

	// 降级：缩放到 360px
	scale2 := 360.0 / float64(w)
	if h > w {
		scale2 = 360.0 / float64(h)
	}
	smallW, smallH := int(float64(w)*scale2), int(float64(h)*scale2)
	small := resizeImage(img, smallW, smallH)
	var buf bytes.Buffer
	jpeg.Encode(&buf, small, &jpeg.Options{Quality: 60})

	log.Printf("[rpa] VLM截图降级: %s %dx%d→%dx%d (%.0fKB)",
		c.ip, w, h, smallW, smallH, float64(buf.Len())/1024)
	return "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(buf.Bytes()), nil
}

// resizeImage 最近邻缩放（快，无需外部依赖）
func resizeImage(img image.Image, newW, newH int) image.Image {
	src := img.Bounds()
	scaleX := float64(src.Dx()) / float64(newW)
	scaleY := float64(src.Dy()) / float64(newH)
	dst := image.NewRGBA(image.Rect(0, 0, newW, newH))
	for y := 0; y < newH; y++ {
		for x := 0; x < newW; x++ {
			sx := int(float64(x) * scaleX)
			sy := int(float64(y) * scaleY)
			if sx >= src.Dx() {
				sx = src.Dx() - 1
			}
			if sy >= src.Dy() {
				sy = src.Dy() - 1
			}
			dst.Set(x, y, img.At(sx+src.Min.X, sy+src.Min.Y))
		}
	}
	return dst
}

// Tap 点击坐标
func (c *Client) Tap(x, y int) error {
	ctx, cancel := c.ctx()
	defer cancel()

	if err := c.cli.TouchClick(ctx, 0, x, y); err != nil {
		return fmt.Errorf("RPA点击失败(%d,%d): %w", x, y, err)
	}
	log.Printf("[rpa] 点击: %s (%d,%d)", c.ip, x, y)
	return nil
}

// Swipe 滑动
func (c *Client) Swipe(x0, y0, x1, y1 int, duration int64) error {
	if duration <= 0 {
		duration = 300
	}
	ctx, cancel := c.ctx()
	defer cancel()

	if err := c.cli.Swipe(ctx, 0, x0, y0, x1, y1, duration, false); err != nil {
		return fmt.Errorf("RPA滑动失败(%d,%d→%d,%d): %w", x0, y0, x1, y1, err)
	}
	log.Printf("[rpa] 滑动: %s (%d,%d)→(%d,%d)", c.ip, x0, y0, x1, y1)
	return nil
}

// InputText 输入文本
func (c *Client) InputText(text string) error {
	if text == "" {
		return fmt.Errorf("输入文本不能为空")
	}
	ctx, cancel := c.ctx()
	defer cancel()

	if err := c.cli.SendText(ctx, text); err != nil {
		return fmt.Errorf("RPA输入失败: %w", err)
	}
	log.Printf("[rpa] 输入: %s %q", c.ip, text)
	return nil
}

// KeyPress 按键
func (c *Client) KeyPress(code int) error {
	ctx, cancel := c.ctx()
	defer cancel()

	if err := c.cli.KeyPress(ctx, code); err != nil {
		return fmt.Errorf("RPA按键失败(code=%d): %w", code, err)
	}
	log.Printf("[rpa] 按键: %s code=%d", c.ip, code)
	return nil
}

// OpenApp 打开应用
func (c *Client) OpenApp(pkg string) error {
	ctx, cancel := c.ctx()
	defer cancel()

	if err := c.cli.OpenApp(ctx, pkg); err != nil {
		return fmt.Errorf("RPA打开应用失败(%s): %w", pkg, err)
	}
	log.Printf("[rpa] 打开应用: %s %s", c.ip, pkg)
	return nil
}

// StopApp 停止应用
func (c *Client) StopApp(pkg string) error {
	ctx, cancel := c.ctx()
	defer cancel()

	if err := c.cli.StopApp(ctx, pkg); err != nil {
		return fmt.Errorf("RPA停止应用失败(%s): %w", pkg, err)
	}
	log.Printf("[rpa] 停止应用: %s %s", c.ip, pkg)
	return nil
}

// Shell 执行 Shell 命令
func (c *Client) Shell(cmdline string) (string, error) {
	ctx, cancel := c.ctx()
	defer cancel()

	output, err := c.cli.ExecCmd(ctx, cmdline, true)
	if err != nil {
		return "", fmt.Errorf("RPA Shell失败: %w", err)
	}
	log.Printf("[rpa] Shell: %s %q → %d字符", c.ip, cmdline, len(output))
	return output, nil
}

// DumpUI 获取 UI 节点树 XML
func (c *Client) DumpUI() (string, error) {
	ctx, cancel := c.ctx()
	defer cancel()

	xml, err := c.cli.DumpNodeXml(ctx, true)
	if err != nil {
		return "", fmt.Errorf("RPA DumpUI失败: %w", err)
	}
	log.Printf("[rpa] DumpUI: %s %d字符", c.ip, len(xml))
	return xml, nil
}

// GetRotation 获取屏幕旋转角度
func (c *Client) GetRotation() (int, error) {
	ctx, cancel := c.ctx()
	defer cancel()

	rotate, err := c.cli.GetDisplayRotate(ctx)
	if err != nil {
		return 0, fmt.Errorf("RPA获取旋转失败: %w", err)
	}
	return rotate, nil
}

// TouchDown 触摸按下
func (c *Client) TouchDown(touchID, x, y int) error {
	ctx, cancel := c.ctx()
	defer cancel()
	return c.cli.TouchDown(ctx, touchID, x, y)
}

// TouchUp 触摸抬起
func (c *Client) TouchUp(touchID, x, y int) error {
	ctx, cancel := c.ctx()
	defer cancel()
	return c.cli.TouchUp(ctx, touchID, x, y)
}

// TouchMove 触摸移动
func (c *Client) TouchMove(touchID, x, y int) error {
	ctx, cancel := c.ctx()
	defer cancel()
	return c.cli.TouchMove(ctx, touchID, x, y)
}

// CheckLive 检查连接是否存活
func (c *Client) CheckLive() error {
	ctx, cancel := c.ctx()
	defer cancel()
	return c.cli.CheckLive(ctx)
}

// IsScreenOn 截图判断屏幕是否亮着（非全黑）
// 返回 (ready, size, error): ready=true 表示设备已开机可操作
func (c *Client) IsScreenOn() (bool, int, error) {
	ctx, cancel := c.ctx()
	defer cancel()

	data, err := c.cli.TakeCaptureCompress(ctx, 0, 10)
	if err != nil {
		return false, 0, fmt.Errorf("截图失败(设备可能未开机): %w", err)
	}

	size := len(data)
	if size < 100 {
		return false, size, nil
	}

	// 采样检测是否全黑：每隔若干字节检查 RGB 值
	// PNG 解码太重，用 JPEG 的原始字节采样即可
	nonBlack := 0
	step := max(size/50, 1)
	for i := step; i < size-step; i += step {
		if data[i] > 8 { // 阈值8，避免JPEG压缩噪点
			nonBlack++
		}
	}

	ready := nonBlack > 3 // 至少3个采样点非黑
	if ready {
		log.Printf("[rpa] 设备已开机: %s (截图%d字节, %d个非黑采样)", c.ip, size, nonBlack)
	} else {
		log.Printf("[rpa] 设备未开机(黑屏): %s (截图%d字节, %d个非黑采样)", c.ip, size, nonBlack)
	}
	return ready, size, nil
}
