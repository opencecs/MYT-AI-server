package sdkapi

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

// PortBinding Docker 端口映射条目
type PortBinding struct {
	HostIP   string `json:"HostIp"`
	HostPort string `json:"HostPort"`
}

// PhoneModel 手机机型信息
type PhoneModel struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	AndroidVersion string `json:"android_version"`
}

type androidInfo struct {
	ID           string                   `json:"id"`
	Name         string                   `json:"name"`
	Status       string                   `json:"status"`
	Image        string                   `json:"image"`
	IndexNum     int                      `json:"indexNum"`
	DeviceID     string                   `json:"deviceId"`
	IPAddress    string                   `json:"ip"`
	NetworkName  string                   `json:"networkName"`
	PortBindings map[string][]PortBinding `json:"portBindings"`
	ModelPath    string                   `json:"modelPath"`
	CPUUsage     float64                  `json:"cpuUsage"` // 容器 CPU 占用百分比（running 时有效）
	MemUsage     int64                    `json:"memUsage"`  // 容器内存使用量（字节，running 时有效）
	MemLimit     int64                    `json:"memLimit"`   // 容器内存上限（字节，0=无限制）
	DiskUsage    int64                    `json:"diskUsage"` // 容器磁盘使用量（字节，running 时有效）
	Tags         []string                 `json:"tags"`      // 容器标签（设备端下发，快照/列表同源）
}

// CreateAndroidRequest 创建安卓云机请求
type CreateAndroidRequest struct {
	Name          string `json:"name"`
	ImageURL      string `json:"imageUrl"`
	DNS           string `json:"dns"`
	IndexNum      int    `json:"indexNum,omitempty"`
	ModelID       string `json:"modelId,omitempty"`
	ModelName     string `json:"modelName,omitempty"`
	LocalModel    string `json:"localModel,omitempty"`
	ModelStatic   string `json:"modelStatic,omitempty"`
	SandboxSize   string `json:"sandboxSize,omitempty"`
	DoboxWidth    string `json:"doboxWidth,omitempty"`
	DoboxHeight   string `json:"doboxHeight,omitempty"`
	DoboxDpi      string `json:"doboxDpi,omitempty"`
	Start         *bool  `json:"start,omitempty"`
	CountryCode   string `json:"countryCode,omitempty"`
	Mgenable      string `json:"mgenable,omitempty"`
	Gmsenable     string `json:"gmsenable,omitempty"`
	Enforce       bool   `json:"enforce"`
	RandomFile    bool   `json:"randomFile"`
	MytBridgeName string `json:"mytBridgeName,omitempty"`
	MacVlanIp     string `json:"macVlanIp,omitempty"`
}

func (c *Client) ListAndroids() ([]androidInfo, error) {
	body, err := c.get("/android")
	if err != nil {
		return nil, err
	}
	resp, err := parseAPIResponse(body)
	if err != nil {
		return nil, err
	}
	var list []androidInfo
	if err := parseListData(resp.Data, &list); err != nil {
		return nil, err
	}
	return list, nil
}

func (c *Client) CreateAndroid(req CreateAndroidRequest) error {
	body, err := c.postJSON("/android", req)
	if err != nil {
		return err
	}
	return c.parseMutationResponse(body)
}

func (c *Client) DeleteAndroid(name string) error {
	body, err := c.deleteWithQuery(fmt.Sprintf("/android?name=%s", url.QueryEscape(name)))
	if err != nil {
		return err
	}
	return c.parseMutationResponse(body)
}

func (c *Client) StartAndroid(name string) error {
	body, err := c.postJSON("/android/start", map[string]string{"name": name})
	if err != nil {
		return err
	}
	return c.parseMutationResponse(body)
}

func (c *Client) StopAndroid(name string) error {
	body, err := c.postJSON("/android/stop", map[string]string{"name": name})
	if err != nil {
		return err
	}
	return c.parseMutationResponse(body)
}

func (c *Client) RestartAndroid(name string) error {
	body, err := c.postJSON("/android/restart", map[string]string{"name": name})
	if err != nil {
		return err
	}
	return c.parseMutationResponse(body)
}

// SetAndroidLabel 设置容器标签（POST /android/label）。
// tags 传空数组 = 清空该容器标签；响应 {code:0, data:{name, tags}}，data.tags 为
// 设备端归一化后的最终标签。设备端不校验容器存在性；改名/删除由设备端自动迁移/清理标签。
func (c *Client) SetAndroidLabel(name string, tags []string) error {
	if name == "" {
		return fmt.Errorf("容器名为空")
	}
	if tags == nil {
		tags = []string{}
	}
	body, err := c.postJSON("/android/label", map[string]any{
		"name": name,
		"tags": tags,
	})
	if err != nil {
		return err
	}
	return c.parseMutationResponse(body)
}

// GetAndroidTagDict 拉取本机标签字典（GET /android/tags，扁平去重全集，不含容器归属）。
// 与 WS tagsChanged 事件载荷同源同序；作为标签字典的 REST 兜底源——老设备无
// tagsChanged 事件时，标签筛选/建议列表以此刷新。
func (c *Client) GetAndroidTagDict() ([]string, error) {
	body, err := c.get("/android/tags")
	if err != nil {
		return nil, err
	}
	resp, err := parseAPIResponse(body)
	if err != nil {
		return nil, err
	}
	var data struct {
		Tags []string `json:"tags"`
	}
	if err := json.Unmarshal(resp.Data, &data); err != nil {
		return nil, fmt.Errorf("标签字典解析失败: %w", err)
	}
	if data.Tags == nil {
		data.Tags = []string{}
	}
	return data.Tags, nil
}

func (c *Client) RenameAndroid(oldName, newName string) error {
	body, err := c.postJSON("/android/rename", map[string]string{
		"name":    oldName,
		"newName": newName,
	})
	if err != nil {
		return err
	}
	return c.parseMutationResponse(body)
}

func (c *Client) MoveAndroid(name string, indexNum int) error {
	body, err := c.postJSON("/android/move", map[string]any{
		"name":     name,
		"indexNum": indexNum,
	})
	if err != nil {
		return err
	}
	return c.parseMutationResponse(body)
}

// DockerExecResponse docker exec 执行结果
type DockerExecResponse struct {
	ExitCode int    `json:"exitCode"`
	Output   string `json:"output"`
}

// DockerExec 在安卓容器内执行命令（通过设备 SDK API）
func (c *Client) DockerExec(containerName string, command string) (*DockerExecResponse, error) {
	body, err := c.postJSON("/android/exec", map[string]interface{}{
		"name":    containerName,
		"command": []string{"sd", "-c", command},
	})
	if err != nil {
		return nil, err
	}

	// 优先尝试标准 API 响应格式
	resp, err := parseAPIResponse(body)
	if err == nil {
		var result DockerExecResponse
		if err := json.Unmarshal(resp.Data, &result); err != nil {
			return &DockerExecResponse{ExitCode: 0, Output: string(resp.Data)}, nil
		}
		// exitCode 非零也视为执行错误
		if result.ExitCode != 0 {
			return nil, fmt.Errorf("容器命令执行失败(exitCode=%d): %s", result.ExitCode, result.Output)
		}
		return &result, nil
	}

	// 非标准响应：可能是纯文本命令输出或 Docker 错误文本
	raw := strings.TrimSpace(string(body))
	if raw == "" {
		return nil, fmt.Errorf("容器未运行或未授权")
	}
	// OCI runtime 错误：容器未授权或 sd 不可用
	if strings.Contains(raw, "OCI runtime exec failed") ||
		strings.Contains(raw, "executable file not found") ||
		strings.Contains(raw, "no such file or directory") {
		return nil, fmt.Errorf("安卓容器未授权或未正常运行，无法执行诊断命令")
	}
	// 纯文本输出，视为成功
	return &DockerExecResponse{ExitCode: 0, Output: raw}, nil
}

// ListPhoneModels 获取设备支持的机型列表
func (c *Client) ListPhoneModels() ([]PhoneModel, error) {
	body, err := c.get("/android/phoneModel?page=0")
	if err != nil {
		return nil, err
	}
	resp, err := parseAPIResponse(body)
	if err != nil {
		return nil, err
	}
	var list []PhoneModel
	if err := parseListData(resp.Data, &list); err != nil {
		return nil, err
	}
	return list, nil
}
