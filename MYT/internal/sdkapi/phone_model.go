package sdkapi

import (
	"encoding/json"
	"fmt"
	"net/url"
)

// PhoneModelEntry 本地机型条目
type PhoneModelEntry struct {
	Name      string `json:"name"`
	MTimstamp int64  `json:"mtimestamp"`
}

// ListLocalPhoneModels 获取本地机型列表
func (c *Client) ListLocalPhoneModels() ([]PhoneModelEntry, error) {
	body, err := c.get("/phoneModel")
	if err != nil {
		return nil, err
	}
	resp, err := parseAPIResponse(body)
	if err != nil {
		return nil, err
	}
	var list []PhoneModelEntry
	if err := parseListData(resp.Data, &list); err != nil {
		return nil, err
	}
	return list, nil
}

// DeleteLocalPhoneModel 删除本地机型
func (c *Client) DeleteLocalPhoneModel(name string) error {
	body, err := c.deleteWithQuery(fmt.Sprintf("/phoneModel?name=%s", url.QueryEscape(name)))
	if err != nil {
		return err
	}
	return c.parseMutationResponse(body)
}

// ExportLocalPhoneModel 导出本地机型
func (c *Client) ExportLocalPhoneModel(name string) error {
	body, err := c.postJSON("/phoneModel/export", map[string]string{"name": name})
	if err != nil {
		return err
	}
	return c.parseMutationResponse(body)
}

// ImportLocalPhoneModel 导入本地机型（通过文件上传），返回设备端注册的机型名称
func (c *Client) ImportLocalPhoneModel(filePath string) (string, error) {
	body, err := c.postFile("/phoneModel/import", "file", filePath)
	if err != nil {
		return "", err
	}
	resp, err := parseAPIResponse(body)
	if err != nil {
		return "", err
	}
	var name string
	if len(resp.Data) > 0 {
		if err := json.Unmarshal(resp.Data, &name); err != nil {
			var obj map[string]any
			if json.Unmarshal(resp.Data, &obj) == nil {
				if n, ok := obj["name"].(string); ok {
					name = n
				}
			}
		}
	}
	return name, nil
}

// PhoneModelBackup 机型备份条目
type PhoneModelBackup struct {
	Name string `json:"name"`
}

// ListPhoneModelBackups 获取机型备份列表
func (c *Client) ListPhoneModelBackups() ([]PhoneModelBackup, error) {
	body, err := c.get("/android/backup/model")
	if err != nil {
		return nil, err
	}
	resp, err := parseAPIResponse(body)
	if err != nil {
		return nil, err
	}
	var list []PhoneModelBackup
	if err := parseListData(resp.Data, &list); err != nil {
		return nil, err
	}
	return list, nil
}

// SavePhoneModelBackup 保存机型备份
func (c *Client) SavePhoneModelBackup(name, suffix string) error {
	body, err := c.postJSON("/android/backup/model", map[string]string{
		"name":   name,
		"suffix": suffix,
	})
	if err != nil {
		return err
	}
	return c.parseMutationResponse(body)
}

// DeletePhoneModelBackup 删除机型备份
func (c *Client) DeletePhoneModelBackup(name string) error {
	body, err := c.deleteWithQuery(fmt.Sprintf("/android/backup/model?name=%s", url.QueryEscape(name)))
	if err != nil {
		return err
	}
	return c.parseMutationResponse(body)
}

// ExportPhoneModelBackup 导出机型备份文件
func (c *Client) ExportPhoneModelBackup(name string) ([]byte, error) {
	path := fmt.Sprintf("/android/backup/modelExport?name=%s", url.QueryEscape(name))
	return c.get(path)
}

// ImportPhoneModelBackup 导入机型备份文件
func (c *Client) ImportPhoneModelBackup(fileURL string) error {
	body, err := c.postJSON("/android/backup/modelImport", map[string]string{"file": fileURL})
	if err != nil {
		return err
	}
	// 响应可能是空或非标准 JSON
	var resp APIResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil // 非 JSON 响应视为成功
	}
	if !isSuccessCode(resp.Code) {
		return fmt.Errorf("API error: %s", resp.ErrorMsg())
	}
	return nil
}

// CheckUpgrade 检查 SDK 升级（无进度回调）
func (c *Client) CheckUpgrade() (string, error) {
	return c.CheckUpgradeWithProgress(nil)
}

// CheckUpgradeWithProgress 检查 SDK 升级
// 兼容 SSE 流式响应和标准 JSON 响应
// onEvent 回调在每个 SSE 事件触发
func (c *Client) CheckUpgradeWithProgress(onEvent func(body []byte)) (string, error) {
	lastBody, err := c.doGetSSE("/server/upgrade", onEvent)
	if err != nil {
		return "", err
	}
	// 尝试标准 API 响应格式
	resp, parseErr := parseAPIResponse(lastBody)
	if parseErr == nil {
		var result struct {
			Msg string `json:"msg"`
		}
		if err := json.Unmarshal(resp.Data, &result); err != nil {
			return string(resp.Data), nil
		}
		return result.Msg, nil
	}
	// SSE 模式：返回最后事件内容
	var progress SSEProgress
	if err := json.Unmarshal(lastBody, &progress); err == nil && progress.Total > 0 {
		pct := int(progress.Progress * 100)
		return fmt.Sprintf("升级进度: %d%% (%d/%d)", pct, progress.Current, progress.Total), nil
	}
	return string(lastBody), nil
}
