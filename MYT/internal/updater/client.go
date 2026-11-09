package updater

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"time"
)

const checkUpdateURL = "https://newapi.moyunteng.com/api/v1/new-myt-client/update"

// UpdateInfo 更新信息
type UpdateInfo struct {
	Version      string `json:"version"`
	DownloadURL  string `json:"downloadUrl"`
	Checksum     string `json:"checksum"` // SHA256 hex 字符串，后端未返回则为空
	FileSize     int64  `json:"fileSize"`
	ReleaseNotes string `json:"releaseNotes"`
	Mandatory    bool   `json:"mandatory"`
	Channel      string `json:"channel"`
	Platform     string `json:"platform"`
}

// CheckResponse 检查更新 API 响应
type CheckResponse struct {
	CodeID int        `json:"code_id"`
	Msg    string     `json:"msg"`
	Data   *UpdateInfo `json:"data"`
}

// GetPlatform 返回当前客户端平台标识，如 windows-amd64, darwin-arm64
func GetPlatform() string {
	return runtime.GOOS + "-" + runtime.GOARCH
}

// Client 客户端更新检查器
type Client struct {
	httpClient *http.Client
}

// NewClient 创建更新检查客户端
func NewClient() *Client {
	return &Client{
		httpClient: &http.Client{Timeout: 15 * time.Second},
	}
}

// Check 检查是否有更新，返回 nil 表示无更新
func (c *Client) Check(version, platform, channel string) (*UpdateInfo, error) {
	url := fmt.Sprintf("%s?platform=%s&version=%s", checkUpdateURL, platform, version)
	if channel != "" {
		url += "&channel=" + channel
	}

	resp, err := c.httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("检查更新失败: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取更新响应失败: %w", err)
	}

	var cr CheckResponse
	if err := json.Unmarshal(body, &cr); err != nil {
		return nil, fmt.Errorf("解析更新响应失败: %w", err)
	}

	if cr.CodeID != 200 {
		return nil, fmt.Errorf("检查更新失败: code=%d msg=%s", cr.CodeID, cr.Msg)
	}

	return cr.Data, nil
}
