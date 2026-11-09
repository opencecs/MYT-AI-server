package auth

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// --- 云端插件元数据（超级 SDK 更新用）---
// V2 体系把超级 SDK（myt_sdk 容器）作为一种"插件"管理：云端 get_plugins_list
// 提供插件名/最新版本/yaml 描述地址，主机 mytsdk（81 端口）执行 install/del。

// PluginInfo 云端插件条目
type PluginInfo struct {
	PluginID    string `json:"myt_plugin_id"`
	PluginName  string `json:"myt_plugin_name"`
	PluginNames string `json:"myt_plugin_names"` // 显示名
	PluginVer   string `json:"myt_plugin_ver"`
	PluginYaml  string `json:"myt_plugin_yaml"`
	PluginURL   string `json:"myt_plugin_url"`
}

// MytSdkInfo mytSdk 容器镜像信息（首次安装直接建容器用）
type MytSdkInfo struct {
	ID    string `json:"id"`
	Image string `json:"image"`
	Name  string `json:"name"`
}

// PluginsListResult get_plugins_list 响应
type PluginsListResult struct {
	Code   string       `json:"code"`
	List   []PluginInfo `json:"list"`
	MytSdk MytSdkInfo   `json:"mytSdk"`
}

// SuperSdkPluginID 超级 SDK 在云端插件表中的固定 ID（V2 Web 同款）
const SuperSdkPluginID = "1000"

// GetPluginsListAPI 拉取云端插件列表（需登录 token）。
// 返回列表与 mytSdk 容器镜像信息；code 非 200 时返回错误。
func GetPluginsListAPI(client *http.Client, token string) (*PluginsListResult, error) {
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	params := url.Values{}
	params.Set("c", "index")
	params.Set("a", "get_plugins_list")
	params.Set("token", token)

	resp, err := client.PostForm(apiGeneralURL, params)
	if err != nil {
		return nil, fmt.Errorf("插件列表请求失败: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取插件列表响应失败: %w", err)
	}

	var result PluginsListResult
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("解析插件列表失败: %w", err)
	}
	if result.Code != "" && result.Code != "200" {
		return nil, fmt.Errorf("插件列表接口错误: code=%s", result.Code)
	}
	return &result, nil
}

// FindSuperSdk 从插件列表中找超级 SDK 条目
func FindSuperSdk(list []PluginInfo) *PluginInfo {
	for i := range list {
		if list[i].PluginID == SuperSdkPluginID {
			return &list[i]
		}
	}
	return nil
}
