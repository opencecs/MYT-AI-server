package updater

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// downloadClient 带 30 分钟整体超时，防止服务端无响应时 goroutine 永久挂起
var downloadClient = &http.Client{
	Timeout: 5 * time.Minute,
}

// ProgressCallback 下载进度回调，percent 范围 0-100；无 Content-Length 时 percent 为 -1
type ProgressCallback func(percent int, bytesDownloaded, bytesTotal int64)

// Downloader 下载服务
type Downloader struct{}

// NewDownloader 创建下载器
func NewDownloader() *Downloader {
	return &Downloader{}
}

// Download 下载文件，支持断点续传和进度回调
func (d *Downloader) Download(url, dest string, progressCb ProgressCallback) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return fmt.Errorf("创建目录失败: %w", err)
	}

	// 检查本地已下载大小，用于断点续传
	var offset int64
	if info, err := os.Stat(dest); err == nil {
		offset = info.Size()
	}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return fmt.Errorf("创建请求失败: %w", err)
	}
	if offset > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
	}

	resp, err := downloadClient.Do(req)
	if err != nil {
		return fmt.Errorf("下载请求失败: %w", err)
	}
	defer resp.Body.Close()

	supported := resp.StatusCode == http.StatusPartialContent
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		return fmt.Errorf("下载失败: HTTP %d", resp.StatusCode)
	}

	// 服务端不支持 Range 则从头下载
	var file *os.File
	if supported && offset > 0 {
		file, err = os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			return fmt.Errorf("打开文件失败: %w", err)
		}
	} else {
		offset = 0
		file, err = os.Create(dest)
		if err != nil {
			return fmt.Errorf("创建文件失败: %w", err)
		}
	}
	defer file.Close()

	totalSize := resp.ContentLength
	if supported && totalSize >= 0 {
		totalSize += offset
	}

	buf := make([]byte, 32*1024)
	var written int64
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, writeErr := file.Write(buf[:n]); writeErr != nil {
				return fmt.Errorf("写入文件失败: %w", writeErr)
			}
			written += int64(n)

			if progressCb != nil {
				if totalSize > 0 {
					total := offset + written
					pct := int(total * 100 / totalSize)
					if pct > 100 {
						pct = 100
					}
					progressCb(pct, total, totalSize)
				} else {
					// 服务端未返回 Content-Length，用已下载字节数反馈进度
					progressCb(-1, offset+written, 0)
				}
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return fmt.Errorf("下载读取失败: %w", readErr)
		}
	}

	// 确保最终进度为 100%
	if progressCb != nil {
		if totalSize > 0 {
			progressCb(100, offset+written, totalSize)
		} else {
			progressCb(100, offset+written, 0)
		}
	}

	return nil
}
