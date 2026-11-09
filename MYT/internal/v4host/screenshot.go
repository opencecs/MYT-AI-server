package v4host

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// --- V4 实例截图（GET /android/:id/screenshot）---
//
// 实测契约（10.10.2.142:8088，tobox_player v0.8.1，2026-10 验证）：
//   - 成功：HTTP 200 + Content-Type: image/png + PNG 原始字节（实测 277818~280579 字节，
//     耗时 ~300ms）；附带 X-Screenshot-Source: cast（投屏抽帧）或 adb（降级 screencap）
//   - 实例未 running / 容器缺失 / guest 未就绪：HTTP 503 + JSON 错误体，
//     X-Screenshot-Error / X-Screenshot-Fallback 头写明原因
//   - 实例不存在：HTTP 404；未知后端：HTTP 400；format=jpeg：HTTP 400（仅支持 png）
//   - 单实例同时只允许 1 个进行中截图（超出 429），单次最长 5s（config screenshot.timeout）
//
// 注意 api.json 把 200 的 schema 声明成 ScreenshotRes(JSON)，与实际（二进制 PNG）不符，
// 以实测为准——因此这里按字节读，不走 unwrapEnvelope。
//
// 输出尺寸参数（width / height，2026-10 服务端新增，已实测 10.10.2.142:8088）：
//   - 语义 = **输出 PNG 的像素尺寸**，由服务端 ffmpeg 缩放，不是查询实例分辨率
//   - 只传一个时按源图比例缩放：width=360 → 360x800，height=300 → 136x300
//     （源图 720x1600）：**传入的那一维取精确值**，另一维按比例四舍五入
//     （height=1201 → 540x1201、height=999 → 450x999，高度原样保留，宽度取整）
//   - 同时传则强制该尺寸、不保持比例（可能变形）
//   - 合法范围 16~4096（整数）；越界/非数字一律 HTTP 400
//   - 仅 cast（投屏抽帧）路径生效；adb screencap 降级仍输出 guest 原生分辨率
//   - 不传 = 不缩放（输出 guest 原生分辨率）
//   - 体积收益显著：720 宽 266KB → 480 宽 162KB → 360 宽 105KB（PNG 无损，与像素数近线性）

const (
	// screenshotTimeout 单次截图超时：服务端上限 5s（config screenshot.timeout）+
	// 容器内 ffmpeg 收尾余量，留到 8s
	screenshotTimeout = 8 * time.Second
	// maxScreenshotSize 截图字节上限（1080x2400 PNG 实测约 300KB，16MB 防异常大包）
	maxScreenshotSize = 16 << 20
)

// pngMagic / jpegMagic 图片魔数（服务端异常时可能返回 JSON 错误体，必须校验魔数，
// 否则错误体会被当成图片 base64 后塞进前端 img src）
var (
	pngMagic  = []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}
	jpegMagic = []byte{0xFF, 0xD8, 0xFF}
)

// Screenshot 抓取实例当前画面（GET /android/:id/screenshot?backend=&width=）。
//
// 返回 PNG 原始字节与 Content-Type（服务端缺省时按魔数推断）。失败原因透出服务端
// message 与 X-Screenshot-Error，便于日志定位（503 多为实例未 running）。
//
// width > 0 时要求服务端按比例把输出缩放到该宽度（高度由源图比例决定）；传 0 表示
// 不缩放（guest 原生分辨率）。仅传 width 不传 height——服务端保持宽高比，
// 各 ROM 输出宽度一致、高度按各自比例，前端 object-contain 不裁切。
func (c *InstanceClient) Screenshot(ctx context.Context, ref InstanceRef, width int) ([]byte, string, error) {
	if ref.ID == "" {
		return nil, "", fmt.Errorf("实例 id 为空")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	reqCtx, cancel := context.WithTimeout(ctx, screenshotTimeout)
	defer cancel()

	path := "/android/" + url.PathEscape(ref.ID) + "/screenshot"
	q := url.Values{}
	if ref.Backend != "" {
		q.Set("backend", ref.Backend)
	}
	if width > 0 {
		q.Set("width", strconv.Itoa(width))
	}
	if encoded := q.Encode(); encoded != "" {
		path += "?" + encoded
	}
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return nil, "", fmt.Errorf("构造截图请求失败: %w", err)
	}
	req.SetBasicAuth(c.user, c.password)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("截图请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// 错误体是 JSON 外壳（几十字节），沿用 maxBodySize 上限读取
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxBodySize))
		return nil, "", screenshotError(resp, body)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxScreenshotSize))
	if err != nil {
		return nil, "", fmt.Errorf("读取截图响应失败: %w", err)
	}
	contentType := resp.Header.Get("Content-Type")
	if !isImageBytes(data) {
		return nil, "", fmt.Errorf("截图响应不是有效图片（HTTP 200，Content-Type=%s，%d 字节）",
			contentType, len(data))
	}
	if contentType == "" {
		contentType = http.DetectContentType(data)
	}
	return data, contentType, nil
}

// screenshotError 把非 200 响应翻译成可读错误（含服务端 message 与 X-Screenshot-Error）。
func screenshotError(resp *http.Response, body []byte) error {
	reason := envelopeMessage(body)
	if reason == "" {
		reason = strings.TrimSpace(string(body))
	}
	if detail := resp.Header.Get("X-Screenshot-Error"); detail != "" {
		if reason == "" {
			reason = detail
		} else {
			reason += "；" + detail
		}
	}
	switch resp.StatusCode {
	case http.StatusTooManyRequests:
		return fmt.Errorf("截图并发超限（HTTP 429，单实例同时只允许 1 个进行中）")
	case http.StatusServiceUnavailable:
		return fmt.Errorf("实例画面不可用（HTTP 503，通常为未 running 或 guest 未就绪）: %s", reason)
	case http.StatusNotFound:
		return fmt.Errorf("实例不存在（HTTP 404）: %s", reason)
	}
	return fmt.Errorf("截图失败（HTTP %d）: %s", resp.StatusCode, reason)
}

// isImageBytes 校验 PNG/JPEG 魔数
func isImageBytes(data []byte) bool {
	return bytes.HasPrefix(data, pngMagic) || bytes.HasPrefix(data, jpegMagic)
}
