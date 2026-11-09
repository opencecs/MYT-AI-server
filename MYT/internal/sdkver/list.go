package sdkver

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// SdkVersionFile 版本包内单个文件的元数据
type SdkVersionFile struct {
	Filename    string `json:"filename"`
	Size        int64  `json:"size"`
	Description string `json:"description"`
	UploadTime  string `json:"upload_time"`
	Checksum    string `json:"checksum"`
}

// SdkVersion 云端 SDK 版本条目（/sdk/list 的 versions 元素）。
// Version 为字符串版本号（如 "223"），原样透传不做数值转换。
type SdkVersion struct {
	SdkType string           `json:"sdk_type"`
	Version string           `json:"version"`
	Status  string           `json:"status"` // published / test
	Files   []SdkVersionFile `json:"files"`
}

// sdkListResponse 版本列表 API 响应
type sdkListResponse struct {
	CodeID int    `json:"code_id"`
	Msg    string `json:"msg"`
	Data   struct {
		SdkType  string       `json:"sdk_type"`
		Versions []SdkVersion `json:"versions"`
	} `json:"data"`
}

// ListVersions 查询指定 sdk_type 的全量版本列表。云端已按版本号降序返回、
// 无分页，这里不二次排序；也不按 status 过滤——test 版本混在结果中，由 UI
// 标注展示而非静默隐藏。未知 sdk_type 返回空列表不报错。
// 与 FetchLatestVersionOfType 同款纪律：该网关对未知路径会以 200 + HTML
// （SPA 兜底）响应，必须以 JSON 解析成功 + code_id==200 为准。
func ListVersions(sdkType string) ([]SdkVersion, error) {
	listURL := apiBase + "/sdk/list?sdk_type=" + url.QueryEscape(sdkType)
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(listURL)
	if err != nil {
		return nil, fmt.Errorf("查询SDK版本列表失败 (%s): %w", listURL, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, fmt.Errorf("读取版本列表响应失败 (%s): %w", listURL, err)
	}

	var lr sdkListResponse
	if err := json.Unmarshal(body, &lr); err != nil {
		// 典型场景：路径被 SPA 兜底成 200 + HTML，或网关不可用
		return nil, fmt.Errorf("版本列表响应非 JSON (%s): %.120s", listURL, string(body))
	}
	if lr.CodeID != 200 {
		return nil, fmt.Errorf("版本列表查询失败 (%s): code=%d msg=%s", listURL, lr.CodeID, lr.Msg)
	}
	if lr.Data.Versions == nil {
		lr.Data.Versions = []SdkVersion{} // 未知 sdk_type：返回空列表而非 nil
	}
	return lr.Data.Versions, nil
}
