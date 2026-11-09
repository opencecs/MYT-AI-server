package sdkapi

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"
)

// ImageLabels 镜像标签（m=型号, d=日期, v=版本）
type ImageLabels struct {
	Model  string `json:"m"`
	Date   string `json:"d"`
	Version string `json:"v"`
}

type ImageInfo struct {
	ID       string      `json:"id"`
	ImageURL string      `json:"imageUrl"`
	Size     string      `json:"size"`
	Created  string      `json:"created"`
	Labels   ImageLabels `json:"labels"`
}

// DisplayName 返回友好名称：P14-v3-20260208
func (img ImageInfo) DisplayName() string {
	if img.Labels.Model != "" {
		name := img.Labels.Model
		if img.Labels.Version != "" {
			name += "-v" + img.Labels.Version
		}
		if img.Labels.Date != "" {
			name += "-" + img.Labels.Date
		}
		return name
	}
	// fallback: 从 imageUrl 提取
	if idx := len(img.ImageURL) - 1; idx > 0 {
		if colon := lastIndexByte(img.ImageURL, ':'); colon >= 0 {
			return img.ImageURL[colon+1:]
		}
	}
	return img.ImageURL
}

func lastIndexByte(s string, c byte) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == c {
			return i
		}
	}
	return -1
}

func (c *Client) ListImages() ([]ImageInfo, error) {
	body, err := c.get("/android/image")
	if err != nil {
		return nil, err
	}
	resp, err := parseAPIResponse(body)
	if err != nil {
		return nil, err
	}
	var list []ImageInfo
	if err := parseListData(resp.Data, &list); err != nil {
		return nil, err
	}
	return list, nil
}

func (c *Client) PullImage(imageURL string) error {
	body, err := c.postJSON("/android/pullImage", map[string]string{"imageUrl": imageURL})
	if err != nil {
		return err
	}
	return c.parseMutationResponse(body)
}

// SwitchImageRequest 更新镜像请求
type SwitchImageRequest struct {
	Name          string `json:"name"`
	ImageURL      string `json:"imageUrl"`
	ModelID       string `json:"modelId,omitempty"`
	ModelName     string `json:"modelName,omitempty"`
	LocalModel    string `json:"localModel,omitempty"`
	DNS           string `json:"dns,omitempty"`
	SandboxSize   string `json:"sandboxSize,omitempty"`
	DoboxWidth    string `json:"doboxWidth,omitempty"`
	DoboxHeight   string `json:"doboxHeight,omitempty"`
	DoboxDpi      string `json:"doboxDpi,omitempty"`
	Enforce       bool   `json:"enforce"`
	Mgenable      string `json:"mgenable,omitempty"`
	Gmsenable     string `json:"gmsenable,omitempty"`
	CountryCode   string `json:"countryCode,omitempty"`
	RandomFile    bool   `json:"randomFile"`
	AdbPort       int    `json:"adbPort,omitempty"`
	MytBridgeName string `json:"mytBridgeName,omitempty"`
	MacVlanIp     string `json:"macVlanIp,omitempty"`
	VpcGroupID    string `json:"vpcGroupId,omitempty"`
	VpcID         string `json:"vpcID,omitempty"`
}

func (c *Client) SwitchImage(req SwitchImageRequest) error {
	body, err := c.postJSON("/android/switchImage", req)
	if err != nil {
		return err
	}
	return c.parseMutationResponse(body)
}

func (c *Client) DeleteImage(image string) error {
	body, err := c.deleteWithQuery(fmt.Sprintf("/android/image?image=%s", url.QueryEscape(image)))
	if err != nil {
		return err
	}
	return c.parseMutationResponse(body)
}

// DownloadExportedImage 下载导出后的安卓镜像包到本地
// GET /android/image/download?filename=xxx
func (c *Client) DownloadExportedImage(filename, localDir string) (string, error) {
	reqURL := fmt.Sprintf("%s/android/image/download?filename=%s", c.BaseURL, url.QueryEscape(filename))
	req, err := http.NewRequestWithContext(c.requestContext(), "GET", reqURL, nil)
	if err != nil {
		return "", fmt.Errorf("创建下载请求失败: %w", err)
	}
	if c.Password != "" {
		req.SetBasicAuth(c.Username, c.Password)
	}

	client := &http.Client{Timeout: 10 * time.Minute, Transport: DeviceTransport}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("下载镜像包失败: %s", friendlyNetError(err))
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("下载镜像包失败: HTTP %d", resp.StatusCode)
	}

	if err := os.MkdirAll(localDir, 0755); err != nil {
		return "", fmt.Errorf("创建本地目录失败: %w", err)
	}

	localPath := filepath.Join(localDir, filename)
	f, err := os.Create(localPath)
	if err != nil {
		return "", fmt.Errorf("创建本地文件失败: %w", err)
	}
	defer f.Close()

	if _, err := io.Copy(f, resp.Body); err != nil {
		os.Remove(localPath)
		return "", fmt.Errorf("写入镜像包失败: %w", err)
	}
	return localPath, nil
}
