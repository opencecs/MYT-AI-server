package emulator

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
	"strings"
	"time"

	"github.com/moyunteng/myt/internal/mytos"
	"github.com/moyunteng/myt/internal/util"
)

const defaultTimeout = 15 * time.Second

// Client 模拟器安卓实例 HTTP 客户端
//
// 与容器客户端完全隔离，独立维护。
// 即使 URL 与容器相同，参数和返回字段也可能有差异，不能假设通用。
type Client struct {
	ip       string
	port     int
	client   *http.Client
	// 公网模式
	isPublic bool
	deviceID string
	token    string
	indexNum int
	bridge   int
}

const cloudBaseURL = "http://119.96.73.226:8892"

// NewClient 创建模拟器客户端（local 模式直连设备）
func NewClient(ip string, port int) *Client {
	transport := &http.Transport{
		ExpectContinueTimeout: 0, // 禁用 100-continue，避免部分设备不支持
	}
	return &Client{
		ip:     ip,
		port:   port,
		client: &http.Client{Timeout: 15 * time.Second, Transport: transport},
	}
}

// NewPublicClient 创建模拟器客户端（public 模式走网关透传）
// deviceID 为目标设备 ID；token 为网关 access_token；indexNum 为坑位号；
// bridge 为桥接模式标记（1=桥接，0=非桥接），网关据此路由到正确端口。
func NewPublicClient(deviceID, token string, indexNum, bridge int) *Client {
	transport := &http.Transport{
		ExpectContinueTimeout: 0,
	}
	return &Client{
		isPublic: true,
		deviceID: deviceID,
		token:    token,
		indexNum: indexNum,
		bridge:   bridge,
		client:   &http.Client{Timeout: 15 * time.Second, Transport: transport},
	}
}

// buildURL 按模式拼接请求 URL：
// public 走网关 /v1/devices/{deviceID}/android{path}?index={idx}&bridge={0|1}
// local 走设备 http://{ip}:{port}{path}
func (c *Client) buildURL(path string) string {
	if c.isPublic {
		sep := "?"
		if strings.Contains(path, "?") {
			sep = "&"
		}
		return fmt.Sprintf("%s/v1/devices/%s/android%s%sindex=%d&bridge=%d",
			cloudBaseURL, c.deviceID, path, sep, c.indexNum, c.bridge)
	}
	return fmt.Sprintf("http://%s:%d%s", c.ip, c.port, path)
}

// setAuth 按模式设置鉴权
func (c *Client) setAuth(req *http.Request) {
	if c.isPublic {
		req.Header.Set("Authorization", "Bearer "+c.token)
		req.Header.Set("Accept", "*/*")
	}
}

// get 发送 GET 请求
func (c *Client) get(path string) (map[string]interface{}, error) {
	rawURL := c.buildURL(path)
	log.Printf("[emulator] GET rawURL=%s", rawURL)

	req, err := http.NewRequest("GET", rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}
	c.setAuth(req)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("模拟器请求失败: %s", util.FriendlyNetError(err))
	}
	defer resp.Body.Close()

	log.Printf("[emulator] GET actual=%s status=%d", resp.Request.URL.String(), resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取模拟器响应失败: %w", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		if hint := util.CheckUnsupported(body); hint != "" {
			return nil, fmt.Errorf("%s", hint)
		}
		return nil, fmt.Errorf("解析模拟器响应失败: %w, 原始响应: %s", err, util.TruncateBody(body))
	}
	return result, nil
}

// post 发送 POST 请求
func (c *Client) post(path string, body io.Reader) (map[string]interface{}, error) {
	rawURL := c.buildURL(path)
	log.Printf("[emulator] POST %s", rawURL)

	req, err := http.NewRequest("POST", rawURL, body)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	c.setAuth(req)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("模拟器请求失败: %s", util.FriendlyNetError(err))
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取模拟器响应失败: %w", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		if hint := util.CheckUnsupported(respBody); hint != "" {
			return nil, fmt.Errorf("%s", hint)
		}
		return nil, fmt.Errorf("解析模拟器响应失败: %w, 原始响应: %s", err, util.TruncateBody(respBody))
	}
	return result, nil
}

// isSuccess 判断响应是否成功
func isSuccess(result map[string]interface{}) bool {
	code, _ := result["code"].(float64)
	return int(code) == 200
}

// --- P0 方法 ---

// Screenshot 轻量截图（JPG格式，适合状态监控）
func (c *Client) Screenshot(quality int) ([]byte, error) {
	rawURL := c.buildURL(fmt.Sprintf("/snapshot?quality=%d", quality))
	req, err := http.NewRequest("GET", rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}
	c.setAuth(req)
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("模拟器截图失败: %s", util.FriendlyNetError(err))
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取截图数据失败: %w", err)
	}
	return data, nil
}

// AutoClick 自动点击/按键操作
func (c *Client) AutoClick(action string, id, x, y int, code ...int) error {
	path := fmt.Sprintf("/autoclick?action=%s&id=%d&x=%d&y=%d", action, id, x, y)
	if len(code) > 0 {
		path += fmt.Sprintf("&code=%d", code[0])
	}
	result, err := c.get(path)
	if err != nil {
		return err
	}
	if !isSuccess(result) {
		return fmt.Errorf("模拟器点击失败: %v", result)
	}
	return nil
}

// --- P1 方法 ---

// GetClipboard 获取剪贴板
func (c *Client) GetClipboard() (string, error) {
	result, err := c.get("/clipboard")
	if err != nil {
		return "", err
	}
	if data, ok := result["data"].(map[string]interface{}); ok {
		if text, ok := data["text"].(string); ok {
			return text, nil
		}
	}
	return "", nil
}

// SetClipboard 设置剪贴板
func (c *Client) SetClipboard(text string) error {
	path := fmt.Sprintf("/clipboard?cmd=2&text=%s", text)
	result, err := c.get(path)
	if err != nil {
		return err
	}
	if !isSuccess(result) {
		return fmt.Errorf("设置剪贴板失败: %v", result)
	}
	return nil
}

// QueryProxy 查询代理状态
func (c *Client) QueryProxy() (map[string]interface{}, error) {
	return c.get("/proxy")
}

// StartProxy 启动代理
func (c *Client) StartProxy(ip string, port int, usr, pwd string, proxyType int) error {
	path := fmt.Sprintf("/proxy?cmd=2&ip=%s&port=%d&usr=%s&pwd=%s&type=%d",
		ip, port, usr, pwd, proxyType)
	result, err := c.get(path)
	if err != nil {
		return err
	}
	if !isSuccess(result) {
		return fmt.Errorf("启动代理失败: %v", result)
	}
	return nil
}

// StopProxy 停止代理
func (c *Client) StopProxy() error {
	result, err := c.get("/proxy?cmd=3")
	if err != nil {
		return err
	}
	if !isSuccess(result) {
		return fmt.Errorf("停止代理失败: %v", result)
	}
	return nil
}

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

// DeviceSnap 设备截图（多分辨率级别）
func (c *Client) DeviceSnap(level int) ([]byte, error) {
	rawURL := c.buildURL(fmt.Sprintf("/?task=snap&level=%d", level))
	req, err := http.NewRequest("GET", rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}
	c.setAuth(req)
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("设备截图失败: %s", util.FriendlyNetError(err))
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

// ListFiles 获取云机指定目录下的文件列表
// API: GET /files?list={path}
func (c *Client) ListFiles(dirPath string) ([]mytos.CloudFileEntry, error) {
	rawURL := c.buildURL("/files?list=" + url.QueryEscape(dirPath))
	log.Printf("[emulator] GET %s", rawURL)

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
		Code  int                `json:"code"`
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
