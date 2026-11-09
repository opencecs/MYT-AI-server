package sdkapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// 同名下载单飞：同一目标文件的并发下载只放行一个，第二个直接报错，
// 避免双路下载同时写同一文件产生拼接坏包。
var (
	downloadLocksMu sync.Mutex
	downloadLocks   = map[string]*sync.Mutex{}
)

func tryLockDownloadPath(absPath string) (unlock func(), ok bool) {
	downloadLocksMu.Lock()
	mu, exists := downloadLocks[absPath]
	if !exists {
		mu = &sync.Mutex{}
		downloadLocks[absPath] = mu
	}
	downloadLocksMu.Unlock()
	if !mu.TryLock() {
		return nil, false
	}
	return mu.Unlock, true
}

// BackupInfo 表示设备上的备份文件信息
type BackupInfo struct {
	Name       string `json:"name"`
	Size       string `json:"size"`
	Mtimestamp int64  `json:"mtimestamp"`
}

// ListBackups 获取设备上的备份文件列表
func (c *Client) ListBackups() ([]BackupInfo, error) {
	body, err := c.get("/backup")
	if err != nil {
		return nil, err
	}
	resp, err := parseAPIResponse(body)
	if err != nil {
		return nil, err
	}
	var backups []BackupInfo
	if err := parseListData(resp.Data, &backups); err != nil {
		return nil, fmt.Errorf("解析备份列表失败: %w", err)
	}
	return backups, nil
}

// DeleteBackup 删除设备上的指定备份文件
// DELETE /backup?name=<filename>（name 为 query 参数；设备端无 /backup/{filename} 路由，
// 拼在路径里会 404 —— 与 /android/backup/model?name= 同款模式，见 API 规范"云机备份"tag）
func (c *Client) DeleteBackup(filename string) error {
	body, err := c.deleteWithQuery("/backup?name=" + url.QueryEscape(filename))
	if err != nil {
		return err
	}
	return c.parseMutationResponse(body)
}

// DownloadBackup 下载设备备份文件到本地（无进度回调）
// GET /backup/download?name=xxx
func (c *Client) DownloadBackup(filename, localDir string, deleteAfterDownload bool) (string, error) {
	return c.DownloadBackupWithProgress(filename, localDir, deleteAfterDownload, nil)
}

// DownloadBackupWithProgress 下载设备备份文件到本地，支持进度回调
// deleteAfterDownload=true 时通知设备下载完成后删除备份文件
func (c *Client) DownloadBackupWithProgress(filename, localDir string, deleteAfterDownload bool, onProgress func(downloaded, total int64)) (string, error) {
	reqURL := c.buildURL("/backup/download?name=" + url.QueryEscape(filename) + "&delete=" + strconv.FormatBool(deleteAfterDownload))
	log.Printf("[DownloadBackup] GET %s", reqURL)

	// 可取消 + 30 分钟超时：client ctx 由调用方 SetContext 注入（CancelTaskOp 可中断在途 HTTP）
	ctx, cancel := context.WithTimeout(c.requestContext(), 30*time.Minute)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return "", err
	}
	c.setAuth(req)

	client := &http.Client{Timeout: 30 * time.Minute, Transport: exportTransport}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("下载备份失败: %s", friendlyNetError(err))
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("下载备份失败: HTTP %d, %s", resp.StatusCode, string(body))
	}

	// 检查是否返回了 JSON 错误而非文件内容
	contentType := resp.Header.Get("Content-Type")
	if strings.HasPrefix(contentType, "application/json") {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		var apiResp APIResponse
		if err := json.Unmarshal(body, &apiResp); err == nil && !isSuccessCode(apiResp.Code) {
			return "", fmt.Errorf("下载备份失败: %s", apiResp.ErrorMsg())
		}
		return "", fmt.Errorf("下载备份失败: 服务器返回了JSON而非文件")
	}

	if err := os.MkdirAll(localDir, 0755); err != nil {
		return "", fmt.Errorf("创建本地目录失败: %w", err)
	}

	localPath := filepath.Join(localDir, filename)
	partPath := localPath + ".part"

	// 同名单飞：同一目标文件同时只允许一个下载在写盘
	unlock, ok := tryLockDownloadPath(localPath)
	if !ok {
		return "", fmt.Errorf("该备份正在下载中，请勿重复操作: %s", filename)
	}
	defer unlock()

	// 原子写盘：先写 .part，全部校验通过后再 rename 覆盖正式名，
	// 失败/中断删除 .part，正式名文件要么完整要么保持旧版
	f, err := os.Create(partPath)
	if err != nil {
		return "", fmt.Errorf("创建本地文件失败: %w", err)
	}

	total := resp.ContentLength
	var written int64
	buf := make([]byte, 64*1024)
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := f.Write(buf[:n]); werr != nil {
				f.Close()
				os.Remove(partPath)
				return "", fmt.Errorf("写入备份文件失败: %w", werr)
			}
			written += int64(n)
			if onProgress != nil && total > 0 {
				onProgress(written, total)
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			f.Close()
			os.Remove(partPath)
			return "", fmt.Errorf("下载备份失败: %w", readErr)
		}
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(partPath)
		return "", fmt.Errorf("落盘备份文件失败: %w", err)
	}
	f.Close()

	// 完整性校验 1：Content-Length 对账（响应头有值时）
	if total > 0 && written != total {
		os.Remove(partPath)
		return "", fmt.Errorf("下载不完整: 期望 %d 字节，实际 %d 字节", total, written)
	}

	// 完整性校验 2：gzip 流式校验（防设备端孤儿打包进程写出的拼接坏包落盘）
	if strings.HasSuffix(strings.ToLower(filename), ".gz") {
		if err := VerifyGzipFile(partPath); err != nil {
			os.Remove(partPath)
			return "", fmt.Errorf("下载的备份包损坏: %w", err)
		}
	}

	if err := os.Rename(partPath, localPath); err != nil {
		os.Remove(partPath)
		return "", fmt.Errorf("保存备份文件失败: %w", err)
	}

	log.Printf("[DownloadBackup] 完成: %s (%d bytes)", localPath, written)
	return localPath, nil
}
