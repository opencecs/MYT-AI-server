package container

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/moyunteng/myt/internal/mytos"
	"github.com/moyunteng/myt/internal/util"
)

// --- 文件上传/下载类 API ---

// Download 下载文件
func (c *Client) Download(filePath string) ([]byte, error) {
	rawURL := c.buildURL("/download?path=" + filePath)
	req, err := http.NewRequest("GET", rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}
	c.setAuth(req)
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("下载失败: %s", util.FriendlyNetError(err))
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

// DownloadToFile 流式下载文件到本地路径（避免大文件撑爆内存）
func (c *Client) DownloadToFile(filePath, savePath string) error {
	reqURL := c.buildURL("/download?path=" + filePath)
	longClient := &http.Client{Timeout: 30 * time.Minute}
	req, err := http.NewRequest("GET", reqURL, nil)
	if err != nil {
		return fmt.Errorf("创建请求失败: %w", err)
	}
	c.setAuth(req)
	resp, err := longClient.Do(req)
	if err != nil {
		return fmt.Errorf("下载失败: %s", util.FriendlyNetError(err))
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("下载失败: HTTP %d", resp.StatusCode)
	}

	f, err := os.Create(savePath)
	if err != nil {
		return fmt.Errorf("创建本地文件失败: %w", err)
	}
	defer f.Close()

	if _, err := io.Copy(f, resp.Body); err != nil {
		return fmt.Errorf("写入文件失败: %w", err)
	}
	return nil
}

// ListFiles 获取云机指定目录下的文件列表
// API: GET /files?list={path}
func (c *Client) ListFiles(dirPath string) ([]mytos.CloudFileEntry, error) {
	rawURL := c.buildURL("/files?list=" + url.QueryEscape(dirPath))
	log.Printf("[container] GET %s", rawURL)

	req, err := http.NewRequest("GET", rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}
	c.setAuth(req)
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("获取文件列表失败: %s", util.FriendlyNetError(err))
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}

	var result struct {
		Code  int                   `json:"code"`
		Files []mytos.CloudFileEntry `json:"files"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("解析文件列表失败: %w", err)
	}
	if result.Code == 202 {
		return nil, fmt.Errorf("该路径不可访问，请检查云机存储状态（FUSE挂载可能失效）")
	}
	if result.Code != 200 {
		return nil, fmt.Errorf("获取文件列表失败: code=%d", result.Code)
	}
	return result.Files, nil
}

// CloudFileEntry 已上移至 mytos 父包作为共享 DTO，模拟器与容器共用。
// 本子包不再定义独立的 CloudFileEntry，避免两端数据结构差异时出现类型不一致。

// UploadFile 上传文件到云机（multipart POST 到 /upload），返回设备端文件路径
func (c *Client) UploadFile(filePath string) (string, error) {
	return c.UploadFileWithProgress(filePath, nil)
}

// progressReader 包装 io.Reader，在读取时回调上传进度
type progressReader struct {
	r        io.Reader
	total    int64
	uploaded int64
	lastEmit int64
	onProg   func(uploaded, total int64)
}

func (pr *progressReader) Read(p []byte) (int, error) {
	n, err := pr.r.Read(p)
	if n > 0 && pr.onProg != nil {
		pr.uploaded += int64(n)
		threshold := pr.total / 100
		if threshold < 512*1024 {
			threshold = 512 * 1024
		}
		// 读到 EOF 时终发一次，保证 uploaded==total 的回调可靠到达（调用方据此切换"安装中"阶段）
		if pr.uploaded-pr.lastEmit >= threshold || err == io.EOF {
			pr.lastEmit = pr.uploaded
			pr.onProg(pr.uploaded, pr.total)
		}
	}
	return n, err
}

// UploadFileWithProgress 上传文件到云机（带进度回调）
func (c *Client) UploadFileWithProgress(filePath string, onProgress func(uploaded, total int64)) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("打开文件失败: %w", err)
	}
	defer file.Close()

	fi, err := file.Stat()
	if err != nil {
		return "", fmt.Errorf("获取文件信息失败: %w", err)
	}

	pr, pw := io.Pipe()
	writer := multipart.NewWriter(pw)

	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[PANIC] upload goroutine recovered: %v", r)
			}
		}()
		defer pw.Close()
		defer writer.Close()

		part, partErr := writer.CreateFormFile("file", filepath.Base(filePath))
		if partErr != nil {
			pw.CloseWithError(partErr)
			return
		}
		progReader := &progressReader{r: file, total: fi.Size(), onProg: onProgress}
		if _, copyErr := io.Copy(part, progReader); copyErr != nil {
			pw.CloseWithError(copyErr)
			return
		}
	}()

	longClient := &http.Client{Timeout: 7200 * time.Second}
	req, err := http.NewRequest("POST", c.buildURL("/upload"), pr)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	c.setAuth(req)

	resp, err := longClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("上传失败: %s", util.FriendlyNetError(err))
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err == nil {
		if data, ok := result["data"].(map[string]interface{}); ok {
			if p, ok := data["path"].(string); ok && p != "" {
				return p, nil
			}
		}
	}
	return "/sdcard/upload/" + filepath.Base(filePath), nil
}
