package container

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

// RandomizeDeviceInfoOptions 容器异步随机设备信息接口参数。
//
// 对应 MYTOS API: GET /modifydev?cmd=2[&isasync=true][&isSpecifiedModel=true&modelId=N]
//   [&language=xx][&random_abroad=true][&modifymac=true][&userip=1.2.3.4]
//
// 与模拟器的 /android/switchModel 完全独立，不要复用。
type RandomizeDeviceInfoOptions struct {
	// IsAsync true = 异步（立刻返回 taskID），false = 同步阻塞
	IsAsync bool
	// IsSpecifiedModel true = 指定机型随机（需配合 ModelName）
	IsSpecifiedModel bool
	// ModelName 指定机型名称（从 mytosModelDictionary 中选取），自动翻译为 modelId
	ModelName string
	// Language 语言代码，例如 "en" / "zh"
	Language string
	// RandomAbroad true = 随机海外机型
	RandomAbroad bool
	// ModifyMAC true = 同时修改 MAC 地址
	ModifyMAC bool
	// UserIP 用于区域匹配的 IP
	UserIP string
}

// RandomizeDeviceInfoResult 异步随机设备信息接口返回
type RandomizeDeviceInfoResult struct {
	// TaskID 异步模式返回的任务 ID；同步模式为空
	TaskID string
	// Raw 原始响应
	Raw map[string]interface{}
}

// RandomizeDeviceInfo 容器异步随机设备信息（对应"切换机型"的容器实现）。
//
// 容器场景下切换机型 = 原地修改设备信息（无需重启），与模拟器的 /android/switchModel 完全独立。
//
// 返回：
//   - 异步模式 (IsAsync=true): 成功时 TaskID 为查询句柄，后续可用 QueryRandomizeTask 轮询
//   - 同步模式 (IsAsync=false): 操作完成才返回，TaskID 为空
func (c *Client) RandomizeDeviceInfo(opts RandomizeDeviceInfoOptions) (*RandomizeDeviceInfoResult, error) {
	q := url.Values{}
	q.Set("cmd", "2")
	if opts.IsAsync {
		q.Set("isasync", "true")
	}
	if opts.IsSpecifiedModel {
		q.Set("isSpecifiedModel", "true")
		modelID, ok := lookupMytosModelID(opts.ModelName)
		if !ok {
			return nil, fmt.Errorf("容器机型字典中未找到机型 %q", opts.ModelName)
		}
		q.Set("modelId", fmt.Sprintf("%d", modelID))
	}
	if opts.Language != "" {
		q.Set("language", opts.Language)
	}
	if opts.RandomAbroad {
		q.Set("random_abroad", "true")
	}
	if opts.ModifyMAC {
		q.Set("modifymac", "true")
	}
	if opts.UserIP != "" {
		q.Set("userip", opts.UserIP)
	}

	path := "/modifydev?" + q.Encode()
	result, err := c.get(path)
	if err != nil {
		return nil, err
	}

	out := &RandomizeDeviceInfoResult{Raw: result}
	// 异步成功返回 {"code":200,"msg":"<taskID>"}
	if opts.IsAsync {
		if msg, ok := result["msg"].(string); ok && msg != "" {
			out.TaskID = msg
		}
	}
	return out, nil
}

// QueryRandomizeTask 查询异步随机设备信息任务状态。
//
// 返回值：
//   - code==200 且 msg=="OK"：任务完成
//   - code==202 reason=="busy"：任务进行中
//   - code==202 reason=="task timeout"：任务已完成但句柄已超时（按完成处理）
//   - 其他：错误
func (c *Client) QueryRandomizeTask(taskID string) (map[string]interface{}, error) {
	if strings.TrimSpace(taskID) == "" {
		return nil, fmt.Errorf("taskID 不能为空")
	}
	path := "/modifydev?cmd=2&query=" + url.QueryEscape(taskID)
	return c.get(path)
}

// lookupMytosModelID 按机型名查找 MYTOS 字典中的 modelId（跨厂商搜索）
func lookupMytosModelID(modelName string) (int, bool) {
	for _, brand := range mytosModelDictionary {
		if id, ok := brand[modelName]; ok {
			return id, true
		}
	}
	return 0, false
}

// ListMytosModelDictionary 返回 MYTOS 机型字典的快照（按厂商分组）
func ListMytosModelDictionary() map[string]map[string]int {
	out := make(map[string]map[string]int, len(mytosModelDictionary))
	for brand, models := range mytosModelDictionary {
		cp := make(map[string]int, len(models))
		for k, v := range models {
			cp[k] = v
		}
		out[brand] = cp
	}
	return out
}

// MarshalMytosModelDictionary 返回 JSON 形式的机型字典（供前端使用）
func MarshalMytosModelDictionary() (string, error) {
	b, err := json.Marshal(ListMytosModelDictionary())
	if err != nil {
		return "", err
	}
	return string(b), nil
}
