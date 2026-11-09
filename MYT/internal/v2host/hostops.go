package v2host

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// --- 主机级操作（81 mytsdk 插件机制 + 主机重启）---
// 对齐 V2 Web hostDetail.html：超级 SDK 是一种"插件"（myt_plugin_*），更新 =
// 卸载（del_plugin）+ 重装（install_plugin）两步；重启主机走 reboot_host。

// pluginActionURL 组装 81 端口插件操作 URL
func pluginActionURL(base, ip, action, name, yamlAddr string) (string, error) {
	switch action {
	case "install":
		if yamlAddr == "" {
			return "", fmt.Errorf("缺少插件 yaml 地址")
		}
		return fmt.Sprintf("%s/install_plugin/%s?addr=%s", base, ip, url.QueryEscape(yamlAddr)), nil
	case "del":
		if name == "" || yamlAddr == "" {
			return "", fmt.Errorf("缺少插件名或 yaml 地址")
		}
		return fmt.Sprintf("%s/del_plugin/%s/%s?addr=%s", base, ip, name, url.QueryEscape(yamlAddr)), nil
	case "start", "stop":
		if name == "" {
			return "", fmt.Errorf("缺少插件名")
		}
		return fmt.Sprintf("%s/%s_plugin/%s/%s", base, action, ip, name), nil
	default:
		return "", fmt.Errorf("不支持的插件操作: %s", action)
	}
}

// PluginAction 插件操作（install/del/start/stop）。MYT_SDK_WEB 返回
// {code, message}，code==200 视为成功。
func (c *Client) PluginAction(ctx context.Context, ip, action, name, yamlAddr string) error {
	u, err := pluginActionURL(c.sdkBaseURL(ip), ip, action, name, yamlAddr)
	if err != nil {
		return err
	}
	return c.callOpsAPI(ctx, u)
}

// RebootHost 重启主机（mytsdk 内部对 ARM 执行重启，连接会中断属预期）。
func (c *Client) RebootHost(ctx context.Context, ip string) error {
	return c.callOpsAPI(ctx, fmt.Sprintf("%s/reboot_host/%s", c.sdkBaseURL(ip), ip))
}

// HostImage 主机本地镜像（2375 /images/json）。
// json tag 小写：Docker 返回的 Id/RepoTags 反序列化大小写不敏感，
// 而前端 bindings 按小写读取（与 V2SourceImage 一致）。
type HostImage struct {
	ID         string   `json:"id"`
	RepoTags   []string `json:"repoTags"`
	Size       int64    `json:"size"`
	Created    int64    `json:"created"`
	Containers int64    `json:"containers"`
}

// ListHostImages 主机本地镜像列表（2375 /images/json）。
func (c *Client) ListHostImages(ctx context.Context, ip string) ([]HostImage, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://%s:%d/images/json", ip, c.port), nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("http %d", resp.StatusCode)
	}
	var images []HostImage
	if err := json.NewDecoder(resp.Body).Decode(&images); err != nil {
		return nil, err
	}
	return images, nil
}

// DeleteHostImage 删除主机本地镜像（2375 DELETE /images/{tag}?force=1）。
func (c *Client) DeleteHostImage(ctx context.Context, ip, tag string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete,
		fmt.Sprintf("http://%s:%d/images/%s?force=1", ip, c.port, url.PathEscape(tag)), nil)
	if err != nil {
		return err
	}
	resp, err := c.opsHC.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("删除镜像失败: http %d %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

// PullImage 拉取镜像到主机（81 dc_api/v1/pull_image?image_addr=）。
// 镜像拉取分钟级——专用无上限客户端，超时由调用方 ctx 控制。
func (c *Client) PullImage(ctx context.Context, ip, imageAddr string) error {
	u := fmt.Sprintf("%s/dc_api/v1/pull_image/%s?image_addr=%s", c.sdkBaseURL(ip), ip, url.QueryEscape(imageAddr))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, nil)
	if err != nil {
		return err
	}
	resp, err := c.pullHC.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	var r struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	_ = json.Unmarshal(body, &r)
	if r.Code != 200 && r.Code != 0 {
		if r.Message != "" {
			return fmt.Errorf("%s", r.Message)
		}
		return fmt.Errorf("拉取失败: code=%d", r.Code)
	}
	return nil
}

// PruneImages 清理主机悬空镜像（81 dc_api/v1/prune_images）。
func (c *Client) PruneImages(ctx context.Context, ip string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		fmt.Sprintf("%s/dc_api/v1/prune_images/%s", c.sdkBaseURL(ip), ip), nil)
	if err != nil {
		return err
	}
	resp, err := c.opsHC.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	var r struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	_ = json.Unmarshal(body, &r)
	if r.Code != 200 && r.Code != 0 {
		if r.Message != "" {
			return fmt.Errorf("%s", r.Message)
		}
		return fmt.Errorf("清理失败: code=%d", r.Code)
	}
	return nil
}

// callOpsAPI 调用 81 端口操作接口（长超时，操作耗时高于探测/查询）。
func (c *Client) callOpsAPI(ctx context.Context, u string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	resp, err := c.opsHC.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return err
	}
	var r struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Msg     string `json:"msg"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return fmt.Errorf("http %d", resp.StatusCode)
	}
	if r.Code != 200 && r.Code != 0 {
		msg := r.Message
		if msg == "" {
			msg = r.Msg
		}
		if msg == "" {
			msg = fmt.Sprintf("code=%d", r.Code)
		}
		return fmt.Errorf("%s", msg)
	}
	return nil
}
