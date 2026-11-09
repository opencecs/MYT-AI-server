package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/moyunteng/myt/internal/auth"
	"github.com/moyunteng/myt/internal/maintenance"
	"github.com/moyunteng/myt/internal/v2host"
)

// --- V2 模式魔云互联：81 插件机制 + mytControl-link 容器（8003 端口） ---
//
// 对齐 V2 体系原生做法（V2 Web 的 plugin 管理与超级 SDK 同链路）：
//   - 安装 = 81 /install_plugin/<host>?addr=<yaml URL>（mytsdk 拉 yaml →
//     docker compose 拉镜像建容器，镜像分钟级）
//   - 卸载 = 81 /del_plugin/<host>/<plugin_name>?addr=<yaml URL>
//   - 状态 = 2375 /containers/json?all=1 按 Labels[myt_plugin_name] 匹配
//   - Web 入口 = http://<主机IP>:8003（yaml 端口映射 "8003:80"，已实测确认）
//
// 与超级 SDK（v2_service_host.go）同款模式；yaml 来源：云端插件表按名匹配，
// 取不到（未登录/云端无条目）用硬编码 URL 兜底（与 update_check.go 风格一致）。
// 回调经 maintenance.SetV2PanelOps 注入——任务队列 handler 直调 maintenance
// 不经 wrapper，注入必须落在 internal 包；依赖方向：internal/maintenance
// 不 import main，通过回调拿本文件的实现。

const (
	// v2PanelPluginName 魔云互联插件容器名 / myt_plugin_name label（云端表与
	// 主机容器一致；docker-compose_mytlink_v1.1.46.yml 的 container_name）
	v2PanelPluginName = "mytControl-link"
	// v2PanelPluginID 云端插件表条目 ID（yaml labels: myt_plugin_id=5）
	v2PanelPluginID = "5"
	// v2PanelFallbackYaml 云端插件表取不到时的 yaml 兜底地址（实测可拉，
	// HTTP 200；来源 www.moyunteng.com/down/plugin/ 惯例路径）
	v2PanelFallbackYaml = "http://www.moyunteng.com/down/plugin/docker-compose_mytlink_v1.1.46.yml"
	// v2PanelPort 魔云互联 Web 入口端口（yaml ports "8003:80"）
	v2PanelPort = 8003
)

// initV2PanelOps 组装 V2 魔云互联操作集（setup 调用）。
func (s *V2ServiceWrapper) initV2PanelOps() {
	maintenance.SetV2PanelOps(&maintenance.V2PanelOps{
		Install:   s.installV2Panel,
		Uninstall: s.uninstallV2Panel,
		Check:     s.checkV2Panel,
		Match:     func(string) bool { return s.configManager.GetDeployMode() == "v2" },
	})
}

// v2PanelContext 解析设备标识（序列号或旧数据 IP）为主机 IP + 云端 yaml 地址。
func (s *V2ServiceWrapper) v2PanelContext(deviceIP string) (string, string, error) {
	dev, err := s.resolveDevice(deviceIP) // GetDeviceByIP 兜底（旧数据传 IP）
	if err != nil {
		return "", "", err
	}
	yaml := s.v2PanelYaml()
	return dev.IP, yaml, nil
}

// v2PanelYaml 取魔云互联 yaml 地址：云端插件表按名/ID 匹配（需登录 token），
// 取不到用硬编码兜底（云端表即便有条目也可能晚于本地版本，兜底保证可装）。
func (s *V2ServiceWrapper) v2PanelYaml() string {
	if s.tokenGetter != nil {
		if token, err := s.tokenGetter(); err == nil && token != "" {
			client := &http.Client{Timeout: 15 * time.Second}
			if result, err := auth.GetPluginsListAPI(client, token); err == nil {
				for i := range result.List {
					p := &result.List[i]
					if p.PluginName == v2PanelPluginName || p.PluginID == v2PanelPluginID {
						if p.PluginYaml != "" {
							return p.PluginYaml
						}
					}
				}
			} else {
				log.Printf("[v2panel] 云端插件列表拉取失败（用兜底 yaml）: %v", err)
			}
		}
	}
	return v2PanelFallbackYaml
}

// matchV2PanelContainer 从容器列表中匹配魔云互联容器（myt_plugin_name label 精确匹配）。
func matchV2PanelContainer(containers []v2host.ContainerInfo) *v2host.ContainerInfo {
	for i := range containers {
		c := &containers[i]
		if c.Labels["myt_plugin_name"] == v2PanelPluginName {
			return c
		}
	}
	return nil
}

// installV2Panel V2 安装魔云互联（81 install_plugin；镜像拉取分钟级）。
// 卸载残留（未装时 del 会报错）不阻断安装（超级 SDK updateSuperSdkByIP 同语义）。
func (s *V2ServiceWrapper) installV2Panel(deviceIP string) map[string]interface{} {
	ip, yamlAddr, err := s.v2PanelContext(deviceIP)
	if err != nil {
		return maintenance.ErrResult("%v", err)
	}
	log.Printf("[v2panel] 开始安装魔云互联（81 install_plugin）: %s yaml=%s", ip, yamlAddr)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	if err := s.client.PluginAction(ctx, ip, "del", v2PanelPluginName, yamlAddr); err != nil {
		log.Printf("[v2panel] 预清理步骤失败（继续安装，未装时属预期）: %v", err)
	}
	if err := s.client.PluginAction(ctx, ip, "install", v2PanelPluginName, yamlAddr); err != nil {
		return maintenance.ErrResult("安装魔云互联失败: %v", err)
	}
	// 安装后等待容器就绪（镜像拉取分钟级；轮询 3 分钟，超时仍返回成功——
	// 任务面板有明确成功消息，容器状态由 CheckMytPanelInstalled 回查）。
	deadline := time.Now().Add(3 * time.Minute)
	for time.Now().Before(deadline) {
		if r := s.checkV2Panel(ip); r["installed"] == true {
			ver, _ := r["version"].(string)
			return map[string]interface{}{
				"success": true,
				"message": fmt.Sprintf("魔云互联已安装（v%s，入口 http://%s:%d）", ver, ip, v2PanelPort),
				"url":     fmt.Sprintf("http://%s:%d", ip, v2PanelPort),
			}
		}
		time.Sleep(5 * time.Second)
	}
	return map[string]interface{}{
		"success": true,
		"message": fmt.Sprintf("魔云互联安装指令已下发，镜像拉取中，稍后访问 http://%s:%d", ip, v2PanelPort),
		"url":     fmt.Sprintf("http://%s:%d", ip, v2PanelPort),
	}
}

// uninstallV2Panel V2 卸载魔云互联（81 del_plugin）。
func (s *V2ServiceWrapper) uninstallV2Panel(deviceIP string) map[string]interface{} {
	ip, yamlAddr, err := s.v2PanelContext(deviceIP)
	if err != nil {
		return maintenance.ErrResult("%v", err)
	}
	log.Printf("[v2panel] 开始卸载魔云互联: %s", ip)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if err := s.client.PluginAction(ctx, ip, "del", v2PanelPluginName, yamlAddr); err != nil {
		// 未安装时卸载报错属预期（超级 SDK 同语义）
		log.Printf("[v2panel] 卸载步骤返回: %v", err)
	}
	return map[string]interface{}{
		"success": true,
		"message": "魔云互联卸载完成",
	}
}

// checkV2Panel V2 状态查询：2375 容器 label 比对（myt_plugin_name/myt_plugin_ver）。
// 返回 installed/version/running/needUpdate/url，前端弹窗按 v2 语义展示。
func (s *V2ServiceWrapper) checkV2Panel(deviceIP string) map[string]interface{} {
	dev, err := s.resolveDevice(deviceIP)
	if err != nil {
		return map[string]interface{}{"installed": false, "message": err.Error()}
	}
	containers, err := s.client.ListContainers(context.Background(), dev.IP)
	if err != nil {
		return map[string]interface{}{"installed": false, "message": err.Error()}
	}
	c := matchV2PanelContainer(containers)
	if c == nil {
		return map[string]interface{}{
			"installed": false,
			"v2":        true,
			"url":       fmt.Sprintf("http://%s:%d", dev.IP, v2PanelPort),
		}
	}
	ver := c.Labels["myt_plugin_ver"]
	running := strings.EqualFold(c.State, "running")
	cloudVer := s.v2PanelCloudVer()
	return map[string]interface{}{
		"installed":  true,
		"v2":         true,
		"version":    ver,
		"running":    running,
		"needUpdate": cloudVer != "" && compareVer(ver, cloudVer) < 0,
		"cloudVer":   cloudVer,
		"url":        fmt.Sprintf("http://%s:%d", dev.IP, v2PanelPort),
	}
}

// v2PanelCloudVer 云端最新版本（未登录/查询失败返回空串——前端不显示更新提示）。
func (s *V2ServiceWrapper) v2PanelCloudVer() string {
	if s.tokenGetter == nil {
		return ""
	}
	token, err := s.tokenGetter()
	if err != nil || token == "" {
		return ""
	}
	client := &http.Client{Timeout: 15 * time.Second}
	result, err := auth.GetPluginsListAPI(client, token)
	if err != nil {
		return ""
	}
	for i := range result.List {
		p := &result.List[i]
		if p.PluginName == v2PanelPluginName || p.PluginID == v2PanelPluginID {
			return p.PluginVer
		}
	}
	return ""
}
