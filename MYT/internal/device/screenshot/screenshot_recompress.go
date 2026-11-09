package screenshot

// screenshot_recompress.go — 截图重压缩（IPC 载荷优化）。
//
// 背景：三条抓图通道的原生输出都偏大且格式不统一——
//   - V4（tobox 8088）只支持 PNG（format=jpeg 服务端 400），480px 宽实测 ~200KB/张，
//     base64 后 ~300KB，99 台全变时 IPC 载荷可达 10~20MB/s；
//   - V3/公网（9082 /task=snap、网关透传）是 JPEG 但无缩放，1080p 原图数百 KB。
//
// 处理：在 storeImage 入口（抓图之后、进缓存/IPC 之前，三条通道唯一汇聚点）统一
// 重编码为 JPEG q75。PNG→JPEG 是无损转有损（无二次劣化）；JPEG→JPEG q75 有一代
// 画质损失，小图（低于阈值）不值得，直接跳过。缩放需要 golang.org/x/image，
// vendor 未收录，故本层只做格式/质量压缩，不做分辨率缩放（V4 分辨率由服务端
// width 参数控制，见 v4ShotWidth）。
//
// 透明度说明：JPEG 无 alpha 通道。Go 标准库编码带 alpha 的图时直接忽略 alpha，
// 透明像素按其 RGB 值落色。安卓截图（投屏抽帧 / adb screencap）画面整体不透明
// （alpha 恒为 255），实际不受影响。
//
// 确定性说明：相同输入字节经同一编码器输出逐字节一致，因此 storeImage 的
// 原始字节 diff 比较在重压缩后依然有效（同画面不误报变化）。

import (
	"bytes"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"log"
)

const (
	// recompressJPEGQuality JPEG 重编码质量。75 是照片类画面的通用平衡点：
	// 肉眼基本无损，体积约为同画面 PNG 的 1/5~1/8（200KB PNG → 25~40KB）。
	recompressJPEGQuality = 75
	// recompressJPEGSkipSize 已是 JPEG 且小于该阈值时跳过重编码：
	// 重编码有一代画质损失，而 16KB 以下的小图再压省不出多少字节（<5KB），
	// 得不偿失。
	recompressJPEGSkipSize = 16 << 10
)

// pngMagic / jpegMagic 图片魔数（与 v4host 侧的判定一致；输入格式按魔数判断，
// 不信任响应头）。
var (
	pngMagic  = []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}
	jpegMagic = []byte{0xFF, 0xD8, 0xFF}
)

// recompressImage 把抓到的图重压缩为小 JPEG，返回 (新字节, 新 Content-Type)。
//
// 输入按魔数判断（不信任响应头，服务端异常时可能给错误体/错误格式）：
//   - PNG → 解码后编码 JPEG q75（V4 主路径）
//   - JPEG：小于阈值或重编码不省字节时原样返回（避免一代损失白吃 + 体积反涨），
//     否则解码重编码 q75（V3/公网 1080p 大图路径）
//   - 其他/解码失败：原样返回、contentType 返回空串（调用方沿用原 Content-Type），
//     绝不因压缩失败丢弃一张能显示的截图
func recompressImage(data []byte) ([]byte, string) {
	switch {
	case bytes.HasPrefix(data, pngMagic):
		out, ok := reencode(data, png.Decode)
		if !ok {
			log.Printf("[screenshot] PNG→JPEG 重编码失败或无收益，保留原图: len=%d", len(data))
			return data, ""
		}
		return out, "image/jpeg"
	case bytes.HasPrefix(data, jpegMagic):
		if len(data) <= recompressJPEGSkipSize {
			return data, "image/jpeg"
		}
		out, ok := reencode(data, jpeg.Decode)
		if !ok {
			log.Printf("[screenshot] JPEG 重编码失败或无收益，保留原图: len=%d", len(data))
			return data, "image/jpeg"
		}
		return out, "image/jpeg"
	default:
		// 非图字节（理论到不了这里，storeImage 前各有魔数校验）：原样透传
		return data, ""
	}
}

// reencode 解码后以 JPEG q75 重编码；解码失败或新字节不小于原图时返回 ok=false
// （调用方保留原图，避免质量/体积双输）。
func reencode(data []byte, decode func(io.Reader) (image.Image, error)) ([]byte, bool) {
	img, err := decode(bytes.NewReader(data))
	if err != nil {
		return nil, false
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: recompressJPEGQuality}); err != nil {
		return nil, false
	}
	out := buf.Bytes()
	if len(out) >= len(data) {
		return nil, false
	}
	return out, true
}
