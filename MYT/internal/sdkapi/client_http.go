package sdkapi

import (
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/moyunteng/myt/internal/util"
)

// doLogged 发请求并留一行接口调用痕迹（util.Debugf 生产构建编译期静默）：
//
//	[api] GET http://192.0.2.202:8000/android → 200 (45ms)
func (c *Client) doLogged(req *http.Request) (*http.Response, error) {
	start := time.Now()
	resp, err := c.httpClient.Do(req)
	if err != nil {
		util.Debugf("[api] %s %s → 失败: %v (%s)", req.Method, req.URL, err, time.Since(start).Round(time.Millisecond))
		return nil, err
	}
	util.Debugf("[api] %s %s → %d (%s)", req.Method, req.URL, resp.StatusCode, time.Since(start).Round(time.Millisecond))
	return resp, nil
}

func (c *Client) get(path string) ([]byte, error) {
	if c.v2HostIP != "" {
		return c.v2Dispatch("GET", path, nil)
	}
	return c.getRaw(path)
}

func (c *Client) getRaw(path string) ([]byte, error) {
	if c.errorMsg != "" {
		return nil, fmt.Errorf("%s", c.errorMsg)
	}
	req, err := http.NewRequestWithContext(c.requestContext(), "GET", c.buildURL(path), nil)
	if err != nil {
		return nil, err
	}
	c.setAuth(req)

	resp, err := c.doLogged(req)
	if err != nil {
		return nil, fmt.Errorf("GET %s 失败: %s", path, friendlyNetError(err))
	}
	defer resp.Body.Close()

	var respBody []byte
	respBody, err = io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("POST %s 读取响应失败: %w", path, err)
	}
	if resp.StatusCode >= 400 {
		return nil, httpError(path, resp.StatusCode, respBody)
	}
	return respBody, nil
}

func (c *Client) postJSON(path string, payload any) ([]byte, error) {
	if c.v2HostIP != "" {
		body, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("序列化请求失败: %w", err)
		}
		return c.v2Dispatch("POST", path, body)
	}
	return c.postRaw(path, payload)
}

func (c *Client) postRaw(path string, payload any) ([]byte, error) {
	if c.errorMsg != "" {
		return nil, fmt.Errorf("%s", c.errorMsg)
	}
	// payload 为 nil 时空 body（81 的 dc_api POST 端点不收 JSON body，
	// "null" 字面量可能触发 422）
	var payloadBytes []byte
	if payload != nil {
		var err error
		payloadBytes, err = json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("序列化请求失败: %w", err)
		}
	}

	req, err := http.NewRequestWithContext(c.requestContext(), "POST", c.buildURL(path), strings.NewReader(string(payloadBytes)))
	if err != nil {
		return nil, err
	}
	c.setAuth(req)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.doLogged(req)
	if err != nil {
		return nil, fmt.Errorf("POST %s 失败: %s", path, friendlyNetError(err))
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("POST %s 读取响应失败: %w", path, err)
	}
	if resp.StatusCode >= 400 {
		return nil, httpError(path, resp.StatusCode, respBody)
	}
	return respBody, nil
}

// postFile 通过 multipart/form-data 上传文件到指定路径
func (c *Client) postFile(path string, fieldName, filePath string) ([]byte, error) {
	if c.errorMsg != "" {
		return nil, fmt.Errorf("%s", c.errorMsg)
	}
	f, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("打开文件失败: %w", err)
	}
	defer f.Close()

	pr, pw := io.Pipe()
	writer := multipart.NewWriter(pw)

	go func() {
		defer pw.Close()
		part, err := writer.CreateFormFile(fieldName, filepath.Base(filePath))
		if err != nil {
			pw.CloseWithError(err)
			return
		}
		if _, err := io.Copy(part, f); err != nil {
			pw.CloseWithError(err)
			return
		}
		writer.Close()
	}()

	req, err := http.NewRequestWithContext(c.requestContext(), "POST", c.buildURL(path), pr)
	if err != nil {
		return nil, err
	}
	c.setAuth(req)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := c.doLogged(req)
	if err != nil {
		return nil, fmt.Errorf("POST %s 失败: %s", path, friendlyNetError(err))
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("POST %s 读取响应失败: %w", path, err)
	}
	if resp.StatusCode >= 400 {
		return nil, httpError(path, resp.StatusCode, body)
	}
	return body, nil
}

func (c *Client) deleteWithQuery(path string) ([]byte, error) {
	if c.v2HostIP != "" {
		return c.v2Dispatch("DELETE", path, nil)
	}
	return c.deleteRaw(path)
}

func (c *Client) deleteRaw(path string) ([]byte, error) {
	if c.errorMsg != "" {
		return nil, fmt.Errorf("%s", c.errorMsg)
	}
	req, err := http.NewRequestWithContext(c.requestContext(), "DELETE", c.buildURL(path), nil)
	if err != nil {
		return nil, err
	}
	c.setAuth(req)

	resp, err := c.doLogged(req)
	if err != nil {
		return nil, fmt.Errorf("DELETE %s 失败: %s", path, friendlyNetError(err))
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("DELETE %s 读取响应失败: %w", path, err)
	}
	if resp.StatusCode >= 400 {
		return nil, httpError(path, resp.StatusCode, body)
	}
	return body, nil
}

func (c *Client) putJSON(path string, payload any) ([]byte, error) {
	if c.v2HostIP != "" {
		body, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("序列化请求失败: %w", err)
		}
		return c.v2Dispatch("PUT", path, body)
	}
	return c.putRaw(path, payload)
}

func (c *Client) putRaw(path string, payload any) ([]byte, error) {
	if c.errorMsg != "" {
		return nil, fmt.Errorf("%s", c.errorMsg)
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("序列化请求失败: %w", err)
	}

	req, err := http.NewRequestWithContext(c.requestContext(), "PUT", c.buildURL(path), strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	c.setAuth(req)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.doLogged(req)
	if err != nil {
		return nil, fmt.Errorf("PUT %s 失败: %s", path, friendlyNetError(err))
	}
	defer resp.Body.Close()

	rbody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("PUT %s 读取响应失败: %w", path, err)
	}
	if resp.StatusCode >= 400 {
		return nil, httpError(path, resp.StatusCode, rbody)
	}
	return rbody, nil
}

// httpError 从 HTTP 响应构造有意义的错误信息
func httpError(path string, statusCode int, body []byte) error {
	// 提取 HTML 标题作为错误描述
	bodyStr := string(body)
	if titleStart := strings.Index(bodyStr, "<title>"); titleStart >= 0 {
		titleEnd := strings.Index(bodyStr[titleStart:], "</title>")
		if titleEnd > 0 {
			title := strings.TrimSpace(bodyStr[titleStart+7 : titleStart+titleEnd])
			return fmt.Errorf("%s 失败: HTTP %d %s", path, statusCode, title)
		}
	}
	preview := bodyStr
	if len(preview) > 200 {
		preview = preview[:200]
	}
	return fmt.Errorf("%s 失败: HTTP %d %s", path, statusCode, strings.TrimSpace(preview))
}

func friendlyNetError(err error) string {
	return util.FriendlyNetError(err)
}
