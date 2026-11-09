package v2host

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// --- 超级 SDK 一键安装（Go 版 initMytSdk，对齐 V2 Web LG.js createMytSdkContainers）---
//
// 流程：2375 查镜像 RepoTags → 缺失经 POST /images/create 拉取（分钟级，stream
// 响应读完即完成）→ 2375 /containers/json?all=1 查 myt_sdk 容器：不存在按 V2 Web
// 同款规格创建（always 重启/特权/5000→81/20000-20050 TCP+UDP 端口段/backup 挂载），
// 存在未跑则 start，创建 409 视为已存在再 start → 81 回验 ListContainersViaSDK
// 成功才算装完。存在但缺端口段的旧容器（旧版客户端安装批次）删除重建。
// 81 已就绪直接返回幂等结果（调用方先探测时无需走到这里）。
//
// 注意：mytSdkInit.js 是漏 20000-20050 端口段的简化版，不要对齐它——容器内
// mytsdk 写的转发规则经这些宿主端口暴露，缺了宿主侧看不见（对齐 LG.js:1867-1880）。

// SuperSdkImage 超级 SDK 载体镜像（写死常量；后续可接云端 get_sdk 动态下发）
const SuperSdkImage = "registry.cn-guangzhou.aliyuncs.com/mytos/dobox:myt_supser_sdk_v1.0.14.30.42"

// superSdkContainerName 超级 SDK 固定容器名（V2 Web LG.js mytSdkContainersName）
const superSdkContainerName = "myt_sdk"

// EnsureResult EnsureSuperSdk 执行结果
type EnsureResult struct {
	PulledImage bool   `json:"pulledImage"` // 是否执行了镜像拉取
	Created     bool   `json:"created"`     // 是否新建了容器
	Started     bool   `json:"started"`     // 是否执行了容器启动
	Ready       bool   `json:"ready"`       // 81 回验通过
	Message     string `json:"message"`
}

// dockerProgressLine /images/create 的 stream 响应行（只需读完驱动进度，不解析细节）
type dockerProgressLine struct {
	Status string `json:"status"`
	Error  string `json:"error"`
}

// EnsureSuperSdk 确保超级 SDK 就绪（幂等：已就绪直接返回 Ready=true）。
// onProgress 每阶段回调（stage/message），可为 nil。镜像拉取分钟级，超时由调用方 ctx 控制。
func (c *Client) EnsureSuperSdk(ctx context.Context, ip string, onProgress func(stage, message string)) (*EnsureResult, error) {
	result := &EnsureResult{}
	emit := func(stage, msg string) {
		if onProgress != nil {
			onProgress(stage, msg)
		}
	}

	// 1. 幂等短路：81 已就绪 = 已安装（例外：缺 20000 端口段的旧容器不短路，
	// 落穿到第 5 步删除重建修复——旧版客户端安装的容器 81 可达但端口段缺失）
	emit("check", "正在检测超级 SDK 状态")
	if _, err := c.ListContainersViaSDK(ctx, ip); err == nil {
		hasPorts, perr := c.hasSuperSdkPortRange(ctx, ip)
		// inspect 失败保守视为端口段在位：不打断正常幂等路径，缺端口段交给
		// 下一次运行再兜底（避免瞬时网络抖动触发无谓的容器重建）
		if perr != nil || hasPorts {
			result.Ready = true
			result.Message = "超级 SDK 已安装"
			return result, nil
		}
	}

	// 2. 镜像检查：RepoTags 含目标镜像则跳过拉取
	hasImage, err := c.hasSuperSdkImage(ctx, ip)
	if err != nil {
		return nil, fmt.Errorf("查询主机镜像失败: %w", err)
	}
	if !hasImage {
		emit("pull", "正在拉取镜像，可能需要几分钟")
		if err := c.pullSuperSdkImage(ctx, ip); err != nil {
			return nil, fmt.Errorf("拉取镜像失败: %w", err)
		}
		result.PulledImage = true
		emit("pull", "镜像拉取完成")
	}

	// 3. 容器检查/创建/启动；存在但缺端口段的旧容器直接删除重建（见下）
	ctr, err := c.findSuperSdkContainer(ctx, ip)
	if err != nil {
		return nil, fmt.Errorf("查询容器失败: %w", err)
	}
	// createAndStart 创建容器（409 容错）+ 启动：初装与重建共用
	createAndStart := func(recreating bool) error {
		emit("create", "正在创建超级 SDK 容器")
		if err := c.createSuperSdkContainer(ctx, ip); err != nil {
			// 409 = 容器已存在（并发安装/残留/并发重建），视为已存在继续启动
			if !SuperSdkErrContainerConflict(err) {
				if recreating {
					return fmt.Errorf("重建容器失败: %w", err)
				}
				return fmt.Errorf("创建容器失败: %w", err)
			}
		} else {
			result.Created = true
		}
		emit("start", "正在启动超级 SDK 容器")
		if err := c.startContainer(ctx, ip, superSdkContainerName); err != nil {
			if recreating {
				return fmt.Errorf("重建后启动容器失败: %w", err)
			}
			return fmt.Errorf("启动容器失败: %w", err)
		}
		result.Started = true
		return nil
	}
	// 5. 端口段对齐（幂等重建）：容器存在但缺 20000 端口段映射（旧版客户端
	// 安装或容器内写转发规则宿主不可见缺陷批次）→ 删除重建。先于常规启动
	// 判定：不浪费一次注定要删的容器 start。
	recreating := false
	if ctr != nil {
		hasPorts, err := c.hasSuperSdkPortRange(ctx, ip)
		if err != nil {
			return nil, fmt.Errorf("检查容器端口段失败: %w", err)
		}
		if !hasPorts {
			emit("recreate", "检测到容器缺端口段，正在重建")
			if err := c.removeSuperSdkContainer(ctx, ip); err != nil {
				return nil, fmt.Errorf("重建容器删除旧容器失败: %w", err)
			}
			ctr = nil // 已删除，走下面的创建分支
			recreating = true
		}
	}
	switch {
	case ctr == nil:
		if err := createAndStart(recreating); err != nil {
			return nil, err
		}
	case !strings.EqualFold(ctr.State, "running"):
		emit("start", "超级 SDK 容器未运行，正在启动")
		if err := c.startContainer(ctx, ip, superSdkContainerName); err != nil {
			return nil, fmt.Errorf("启动容器失败: %w", err)
		}
		result.Started = true
	}

	// 4. 81 回验（容器内 mytsdk 起服务需要数秒，轮询最多 60s）
	emit("verify", "正在验证超级 SDK 服务")
	deadline := time.Now().Add(60 * time.Second)
	for {
		if _, err := c.ListContainersViaSDK(ctx, ip); err == nil {
			result.Ready = true
			result.Message = "超级 SDK 安装完成"
			emit("done", result.Message)
			return result, nil
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("超级 SDK 已启动但 81 端口验证超时（60s），请稍后在设备详情重试检测")
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(3 * time.Second):
		}
	}
}

// hasSuperSdkImage 主机本地镜像 RepoTags 是否含 SuperSdkImage。
func (c *Client) hasSuperSdkImage(ctx context.Context, ip string) (bool, error) {
	images, err := c.ListHostImages(ctx, ip)
	if err != nil {
		return false, err
	}
	for _, img := range images {
		for _, tag := range img.RepoTags {
			if tag == SuperSdkImage {
				return true, nil
			}
		}
	}
	return false, nil
}

// pullSuperSdkImage 经 2375 拉取镜像（POST /images/create?fromImage=...，stream 响应
// 读完即拉取完成；pullHC 无超时上限，分钟级耗时由调用方 ctx 控制）。
func (c *Client) pullSuperSdkImage(ctx context.Context, ip string) error {
	u := fmt.Sprintf("http://%s:%d/images/create?fromImage=%s", ip, c.port, url.QueryEscape(SuperSdkImage))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, nil)
	if err != nil {
		return err
	}
	resp, err := c.pullHC.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("http %d", resp.StatusCode)
	}
	// 流式读取进度行：任一行带 error 字段即拉取失败（Docker stream 协议）
	dec := json.NewDecoder(resp.Body)
	for {
		var line dockerProgressLine
		if err := dec.Decode(&line); err != nil {
			if err == io.EOF {
				return nil // 流结束 = 拉取完成
			}
			return err
		}
		if line.Error != "" {
			return fmt.Errorf("%s", line.Error)
		}
	}
}

// findSuperSdkContainer 在主机全量容器中找 myt_sdk 容器（Names[0] 去 "/" 前缀比对）。
func (c *Client) findSuperSdkContainer(ctx context.Context, ip string) (*ContainerInfo, error) {
	containers, err := c.ListContainers(ctx, ip)
	if err != nil {
		return nil, err
	}
	for i := range containers {
		for _, name := range containers[i].Names {
			if strings.TrimPrefix(name, "/") == superSdkContainerName {
				return &containers[i], nil
			}
		}
	}
	return nil, nil
}

// errContainerConflict 容器名已存在（Docker POST /containers/create 返回 409）。
// createSuperSdkContainer 包装返回，EnsureSuperSdk 用 errors.Is 判别做「视为已存在」容错。
var errContainerConflict = errors.New("container conflict (409)")

// SuperSdkErrContainerConflict 判断 err 是否为容器已存在冲突（409）。
func SuperSdkErrContainerConflict(err error) bool {
	return errors.Is(err, errContainerConflict)
}

// superSdkPortRangeStart / superSdkPortRangeCount SDK 容器 NAT 端口段：
// 20000-20050（TCP+UDP 各 51 条同号映射，容器内写转发规则的宿主侧出口）。
// 与设备端标准部署（V2 Web LG.js createMytSdkContainers）一致。
const (
	superSdkPortRangeStart  = 20000
	superSdkPortRangeCount  = 51
	superSdkNatProbePort    = superSdkPortRangeStart // 端口段哨兵：仅探 20000 一条
	superSdkContainerPort81 = "5000/tcp"
	superSdkHostPort81      = "81"
)

// createSuperSdkContainer 按设备端标准部署规格创建 myt_sdk 容器：
// always 重启策略 + 特权 + 5000→81 + 20000-20050 TCP/UDP 端口段（同号映射）
// + ExposedPorts + sdk_backup 目录挂载。
func (c *Client) createSuperSdkContainer(ctx context.Context, ip string) error {
	portBindings := map[string]any{
		superSdkContainerPort81: []map[string]string{{"HostPort": superSdkHostPort81}},
	}
	exposedPorts := map[string]any{
		superSdkContainerPort81: struct{}{},
	}
	for i := superSdkPortRangeStart; i < superSdkPortRangeStart+superSdkPortRangeCount; i++ {
		portBindings[fmt.Sprintf("%d/tcp", i)] = []map[string]string{{"HostPort": fmt.Sprintf("%d", i)}}
		portBindings[fmt.Sprintf("%d/udp", i)] = []map[string]string{{"HostPort": fmt.Sprintf("%d", i)}}
		exposedPorts[fmt.Sprintf("%d/tcp", i)] = struct{}{}
		exposedPorts[fmt.Sprintf("%d/udp", i)] = struct{}{}
	}
	payload := map[string]any{
		"Image": SuperSdkImage,
		"HostConfig": map[string]any{
			"RestartPolicy": map[string]string{"Name": "always"},
			"Privileged":    true,
			"PortBindings":  portBindings,
			"Binds":         []string{"/mmc/data/sdk_backup:/usr/local/myt_sdk/backup:rw"},
		},
		"Config": map[string]any{
			"ExposedPorts": exposedPorts,
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	u := fmt.Sprintf("http://%s:%d/containers/create?name=%s", ip, c.port, url.QueryEscape(superSdkContainerName))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, strings.NewReader(string(body)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.opsHC.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode == http.StatusCreated {
		return nil
	}
	if resp.StatusCode == http.StatusConflict {
		// 409 = 容器名已存在（并发安装/残留），sentinel error 供上层 errors.Is 判别
		return fmt.Errorf("%w: %s", errContainerConflict, strings.TrimSpace(string(respBody)))
	}
	return fmt.Errorf("http %d %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
}

// superSdkInspectResult Docker GET /containers/{name}/json 的子集：
// 仅取端口段判定所需的 HostConfig.PortBindings。
type superSdkInspectResult struct {
	HostConfig struct {
		PortBindings map[string][]struct {
			HostPort string `json:"HostPort"`
		} `json:"PortBindings"`
	} `json:"HostConfig"`
}

// hasSuperSdkPortRange inspect 容器的 PortBindings，判断 20000/tcp→20000
// 哨兵映射是否存在。inspect 失败（网络/瞬时）保守返回 true——不打断正常幂等
// 路径，缺端口的重建交给下一次运行再兜底。
func (c *Client) hasSuperSdkPortRange(ctx context.Context, ip string) (bool, error) {
	u := fmt.Sprintf("http://%s:%d/containers/%s/json", ip, c.port, url.PathEscape(superSdkContainerName))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return true, err
	}
	resp, err := c.opsHC.Do(req)
	if err != nil {
		return true, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 65536))
	if resp.StatusCode != http.StatusOK {
		return true, fmt.Errorf("http %d %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var ins superSdkInspectResult
	if err := json.Unmarshal(body, &ins); err != nil {
		return true, err
	}
	for _, b := range ins.HostConfig.PortBindings[fmt.Sprintf("%d/tcp", superSdkNatProbePort)] {
		if b.HostPort == fmt.Sprintf("%d", superSdkNatProbePort) {
			return true, nil
		}
	}
	return false, nil
}

// removeSuperSdkContainer 删除 myt_sdk 容器（DELETE /containers/{name}?force=1）。
// Docker 无「按名字删除」端点，走名字 URL 由 daemon 自行解析（404 视为已不存在）。
func (c *Client) removeSuperSdkContainer(ctx context.Context, ip string) error {
	u := fmt.Sprintf("http://%s:%d/containers/%s?force=1", ip, c.port, url.PathEscape(superSdkContainerName))
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, u, nil)
	if err != nil {
		return err
	}
	resp, err := c.opsHC.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusNotFound {
		return nil
	}
	return fmt.Errorf("http %d %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
}

// startContainer 启动容器（POST /containers/{name}/start）。
func (c *Client) startContainer(ctx context.Context, ip, name string) error {
	u := fmt.Sprintf("http://%s:%d/containers/%s/start", ip, c.port, url.PathEscape(name))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, nil)
	if err != nil {
		return err
	}
	resp, err := c.opsHC.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusNotModified {
		return nil
	}
	return fmt.Errorf("http %d %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
}
