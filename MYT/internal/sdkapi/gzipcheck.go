package sdkapi

import (
	"compress/gzip"
	"fmt"
	"io"
	"os"
)

// verifyGzipReader 流式校验 gzip 流完整性（读到 trailer 的 CRC32/ISIZE 才算通过），
// 不把内容载入内存（备份包可能上 GB）。任何中途损坏（crc error / unexpected EOF）都会报错。
func verifyGzipReader(r io.Reader) error {
	zr, err := gzip.NewReader(r)
	if err != nil {
		return fmt.Errorf("不是有效的 gzip 数据: %w", err)
	}
	if _, err := io.Copy(io.Discard, zr); err != nil {
		return err
	}
	return zr.Close()
}

// VerifyGzipFile 校验本地 gzip 文件完整性（流式读取，不载入内存）。
func VerifyGzipFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("打开文件失败: %w", err)
	}
	defer f.Close()
	if err := verifyGzipReader(f); err != nil {
		return fmt.Errorf("gzip 校验失败: %w", err)
	}
	return nil
}
