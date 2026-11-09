package sdkver

import (
	"encoding/json"
	"sync/atomic"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

const (
	apiBase        = "https://newapi.moyunteng.com/api/v1"
	sdkVersionURL  = apiBase + "/sdk_ver/latest?sdk_type=box_sdk"
	sdkDownloadURL  = apiBase + "/sdk/download-url?sdk_type=box_sdk&version=%s&filename=myt-sdk.zip"
	sdkZipName     = "myt-sdk.zip"
	sdkVersionFile = "myt-sdk.version"
	sdkType        = "box_sdk"
)

// VersionResponse 公网版本查询 API 响应
type VersionResponse struct {
	CodeID int `json:"code_id"`
	Msg    string `json:"msg"`
	Data   struct {
		LatestVersion string `json:"latest_version"`
		SDKType       string `json:"sdk_type"`
		Status        string `json:"status"`
		Files         []struct {
			Checksum    string `json:"checksum"`
			Filename    string `json:"filename"`
			Size        int64  `json:"size"`
			Description string `json:"description"`
			UploadTime  string `json:"upload_time"`
		} `json:"files"`
	} `json:"data"`
}

// DownloadURLResponse 下载 URL 查询 API 响应
type DownloadURLResponse struct {
	CodeID int `json:"code_id"`
	Msg    string `json:"msg"`
	Data   struct {
		DownloadURL string `json:"download_url"`
		SDKType     string `json:"sdk_type"`
	} `json:"data"`
}

// Client SDK 版本查询和下载客户端
type Client struct {
	sdkDir     string
	httpClient *http.Client
}

// NewClient 创建 SDK 版本管理客户端
func NewClient(sdkDir string) *Client {
	log.Printf("[sdkver] 初始化, SDK目录: %s", sdkDir)
	return &Client{
		sdkDir:     sdkDir,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// FetchLatestVersion 查询公网最新 SDK 版本号
func (c *Client) FetchLatestVersion() (int, error) {
	log.Printf("[sdkver] 查询公网最新版本: %s", sdkVersionURL)

	resp, err := c.httpClient.Get(sdkVersionURL)
	if err != nil {
		return 0, fmt.Errorf("查询SDK版本失败: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, fmt.Errorf("读取版本响应失败: %w", err)
	}

	var vr VersionResponse
	if err := json.Unmarshal(body, &vr); err != nil {
		return 0, fmt.Errorf("解析版本响应失败: %w", err)
	}
	if vr.CodeID != 200 {
		return 0, fmt.Errorf("版本查询失败: code=%d msg=%s", vr.CodeID, vr.Msg)
	}

	version, err := strconv.Atoi(vr.Data.LatestVersion)
	if err != nil {
		return 0, fmt.Errorf("无效版本号: %s", vr.Data.LatestVersion)
	}

	log.Printf("[sdkver] 公网最新版本: %d (status=%s)", version, vr.Data.Status)

	// 描述带出（LastFetchDescription 供 SdkCheckUpgrade 组装返回用——FetchLatestVersion
	// 签名是版本号语义不能改，描述经包级最近值传递；本包 latest 查询全局只有这一处）
	desc := ""
	if len(vr.Data.Files) > 0 {
		desc = vr.Data.Files[0].Description
	}
	lastDesc.Store(desc)

	return version, nil
}

// lastDesc 最近一次 box_sdk latest 查询的版本描述（LastFetchDescription 读取）。
// 并发查询互相覆盖无害——描述与版本号在同一次响应里，缓存节流（10 分钟）下
// 实际并发概率极低，读到旧值也只是描述短暂滞后。
var lastDesc atomic.Value // string

// LastFetchDescription 最近一次 box_sdk 公网版本查询带出的描述。
func LastFetchDescription() string {
	if v, ok := lastDesc.Load().(string); ok {
		return v
	}
	return ""
}

// FetchLatestVersionOfType 查询指定 sdk_type 的最新发布版本号（字符串原样返回，
// 如 "1"）。独立于 Client（box_sdk 本地缓存流程）——v2_box_sdk 等类型只查
// latest，不落本地。注意：该网关对未知路径会以 200 + HTML（SPA 兜底）响应，
// 因此必须以 JSON 解析成功 + code_id==200 为准，不能只看 HTTP 状态码。
func FetchLatestVersionOfType(sdkType string) (string, error) {
	info, err := FetchLatestVersionOfTypeInfo(sdkType)
	if err != nil {
		return "", err
	}
	return info.LatestVersion, nil
}

// LatestVersionInfo latest 查询结果：版本号 + 首个文件的描述（升级确认弹窗展示用，
// files 为空或描述为空时 Description 为空串）。
type LatestVersionInfo struct {
	LatestVersion string
	Description   string
}

// FetchLatestVersionOfTypeInfo 同 FetchLatestVersionOfType，附带返回云端版本描述
// （/sdk_ver/latest 的 files[0].description，与 /sdk/list 同结构）。
func FetchLatestVersionOfTypeInfo(sdkType string) (LatestVersionInfo, error) {
	url := fmt.Sprintf("%s/sdk_ver/latest?sdk_type=%s", apiBase, sdkType)
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return LatestVersionInfo{}, fmt.Errorf("查询SDK版本失败 (%s): %w", url, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return LatestVersionInfo{}, fmt.Errorf("读取版本响应失败 (%s): %w", url, err)
	}

	var vr VersionResponse
	if err := json.Unmarshal(body, &vr); err != nil {
		// 典型场景：路径被 SPA 兜底成 200 + HTML，或网关不可用
		return LatestVersionInfo{}, fmt.Errorf("版本查询响应非 JSON (%s): %.120s", url, string(body))
	}
	if vr.CodeID != 200 {
		return LatestVersionInfo{}, fmt.Errorf("版本查询失败 (%s): code=%d msg=%s", url, vr.CodeID, vr.Msg)
	}
	if vr.Data.LatestVersion == "" {
		return LatestVersionInfo{}, fmt.Errorf("版本查询响应缺少 latest_version (%s)", url)
	}
	desc := ""
	if len(vr.Data.Files) > 0 {
		desc = vr.Data.Files[0].Description
	}
	return LatestVersionInfo{LatestVersion: vr.Data.LatestVersion, Description: desc}, nil
}

// LocalVersion 读取本地缓存的 SDK 版本号，没有则返回 0
func (c *Client) LocalVersion() int {
	data, err := os.ReadFile(c.versionPath())
	if err != nil {
		log.Printf("[sdkver] 本地无版本缓存: %s", c.versionPath())
		return 0
	}
	v, err := strconv.Atoi(string(data))
	if err != nil {
		log.Printf("[sdkver] 本地版本号格式异常: %s", string(data))
		return 0
	}
	return v
}

// EnsureSDK 确保本地有指定版本的 SDK 文件
// 版本一致且文件存在则直接返回路径，否则下载新版本
func (c *Client) EnsureSDK(version int) (string, error) {
	localVer := c.LocalVersion()
	if localVer == version && c.zipExists() {
		log.Printf("[sdkver] 本地SDK已是版本 %d, 无需下载: %s", version, c.zipPath())
		return c.zipPath(), nil
	}

	log.Printf("[sdkver] 需要下载SDK: 本地版本=%d, 目标版本=%d", localVer, version)

	if err := os.MkdirAll(c.sdkDir, 0755); err != nil {
		return "", fmt.Errorf("创建SDK目录失败: %w", err)
	}

	downloadURL, err := c.fetchDownloadURL(version)
	if err != nil {
		return "", err
	}

	if err := c.downloadSDK(downloadURL, version); err != nil {
		return "", err
	}

	log.Printf("[sdkver] SDK下载完成: %s (版本 %d)", c.zipPath(), version)
	return c.zipPath(), nil
}

// ZipPath 返回 SDK zip 文件路径
func (c *Client) ZipPath() string {
	return c.zipPath()
}

func (c *Client) zipPath() string    { return filepath.Join(c.sdkDir, sdkZipName) }
func (c *Client) versionPath() string { return filepath.Join(c.sdkDir, sdkVersionFile) }

func (c *Client) zipExists() bool {
	_, err := os.Stat(c.zipPath())
	return err == nil
}

func (c *Client) fetchDownloadURL(version int) (string, error) {
	url := fmt.Sprintf(sdkDownloadURL, strconv.Itoa(version))
	log.Printf("[sdkver] 获取下载URL: %s", url)

	resp, err := c.httpClient.Get(url)
	if err != nil {
		return "", fmt.Errorf("获取下载URL失败: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("读取下载URL响应失败: %w", err)
	}

	var dr DownloadURLResponse
	if err := json.Unmarshal(body, &dr); err != nil {
		return "", fmt.Errorf("解析下载URL响应失败: %w", err)
	}
	if dr.CodeID != 200 {
		return "", fmt.Errorf("获取下载URL失败: code=%d msg=%s", dr.CodeID, dr.Msg)
	}

	log.Printf("[sdkver] 下载URL: %s", dr.Data.DownloadURL)
	return dr.Data.DownloadURL, nil
}

func (c *Client) downloadSDK(downloadURL string, version int) error {
	log.Printf("[sdkver] 开始下载SDK (版本 %d)...", version)

	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Get(downloadURL)
	if err != nil {
		return fmt.Errorf("下载SDK失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("下载SDK失败: HTTP %d", resp.StatusCode)
	}

	tmpPath := c.zipPath() + ".tmp"

	f, err := os.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("创建临时文件失败: %w", err)
	}

	// 流式写入，32KB 缓冲区，不预占内存
	buf := make([]byte, 32*1024)
	var written int64
	written, err = io.CopyBuffer(f, resp.Body, buf)
	if err != nil {
		f.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("写入SDK文件失败: %w", err)
	}
	f.Close()

	log.Printf("[sdkver] SDK文件已下载 %d 字节, 保存到临时文件: %s", written, tmpPath)

	if err := os.Rename(tmpPath, c.zipPath()); err != nil {
		return fmt.Errorf("重命名SDK文件失败: %w", err)
	}

	// 保存版本号
	if err := os.WriteFile(c.versionPath(), []byte(strconv.Itoa(version)), 0644); err != nil {
		return fmt.Errorf("保存版本号失败: %w", err)
	}

	log.Printf("[sdkver] SDK文件就绪: %s, 版本号已保存: %s", c.zipPath(), c.versionPath())
	return nil
}
