package emulator

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"

	"github.com/moyunteng/myt/internal/util"
)

// UploadKeybox 上传 Google 证书（PEM格式）
func (c *Client) UploadKeybox(certPath string) error {
	file, err := os.Open(certPath)
	if err != nil {
		return fmt.Errorf("打开证书文件失败: %w", err)
	}
	defer file.Close()

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", filepath.Base(certPath))
	if err != nil {
		return fmt.Errorf("创建表单失败: %w", err)
	}
	if _, err := io.Copy(part, file); err != nil {
		return fmt.Errorf("写入文件失败: %w", err)
	}
	writer.Close()

	url := c.buildURL("/uploadkeybox")
	req, err := http.NewRequest("POST", url, body)
	if err != nil {
		return fmt.Errorf("create request failed: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	c.setAuth(req)
	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("上传证书失败: %s", util.FriendlyNetError(err))
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil
	}
	if !isSuccess(result) {
		return fmt.Errorf("上传证书失败: %v", result)
	}
	return nil
}

// ModuleMgr 模块管理（Magisk/GMS 安装/卸载/查询）
func (c *Client) ModuleMgr(cmd, module string) (map[string]interface{}, error) {
	path := fmt.Sprintf("/modulemgr?cmd=%s&module=%s", cmd, module)
	return c.get(path)
}
