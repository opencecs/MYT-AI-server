package container

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/moyunteng/myt/internal/util"
)

const defaultTimeout = 15 * time.Second

const cloudBaseURL = "http://119.96.73.226:8892"

// Client 容器安卓实例 HTTP 客户端
//
// 与模拟器客户端完全隔离，独立维护。
// 即使 URL 与模拟器相同，参数和返回字段也可能有差异，不能假设通用。
type Client struct {
	ip     string
	port   int
	client *http.Client
	// 公网模式
	isPublic bool
	deviceID string
	token    string
	indexNum int
	bridge   int
}

// NewClient 创建容器客户端（local 模式直连设备）
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

// NewPublicClient 创建容器客户端（public 模式走网关透传）
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

// buildURL 按模式拼接请求 URL
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
	log.Printf("[container] GET rawURL=%s", rawURL)

	req, err := http.NewRequest("GET", rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}
	c.setAuth(req)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("容器请求失败: %s", util.FriendlyNetError(err))
	}
	defer resp.Body.Close()

	log.Printf("[container] GET actual=%s status=%d", resp.Request.URL.String(), resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取容器响应失败: %w", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		if hint := util.CheckUnsupported(body); hint != "" {
			return nil, fmt.Errorf("%s", hint)
		}
		return nil, fmt.Errorf("解析容器响应失败: %w, 原始响应: %s", err, util.TruncateBody(body))
	}
	return result, nil
}

// post 发送 POST 请求
func (c *Client) post(path string, body io.Reader) (map[string]interface{}, error) {
	rawURL := c.buildURL(path)
	log.Printf("[container] POST %s", rawURL)

	req, err := http.NewRequest("POST", rawURL, body)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	c.setAuth(req)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("容器请求失败: %s", util.FriendlyNetError(err))
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取容器响应失败: %w", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		if hint := util.CheckUnsupported(respBody); hint != "" {
			return nil, fmt.Errorf("%s", hint)
		}
		return nil, fmt.Errorf("解析容器响应失败: %w, 原始响应: %s", err, util.TruncateBody(respBody))
	}
	return result, nil
}

// postForm 发送 POST 表单请求（application/x-www-form-urlencoded）
func (c *Client) postForm(path string, form url.Values) (map[string]interface{}, error) {
	rawURL := c.buildURL(path)
	log.Printf("[container] POST_FORM %s body=%s", rawURL, form.Encode())

	req, err := http.NewRequest("POST", rawURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	c.setAuth(req)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("容器请求失败: %s", util.FriendlyNetError(err))
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取容器响应失败: %w", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		if hint := util.CheckUnsupported(respBody); hint != "" {
			return nil, fmt.Errorf("%s", hint)
		}
		return nil, fmt.Errorf("解析容器响应失败: %w, 原始响应: %s", err, util.TruncateBody(respBody))
	}
	return result, nil
}

// isSuccess 判断响应是否成功
func isSuccess(result map[string]interface{}) bool {
	code, _ := result["code"].(float64)
	return int(code) == 200
}
