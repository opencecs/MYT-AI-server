package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/moyunteng/myt/internal/auth"
	"github.com/moyunteng/myt/internal/config"
	"github.com/moyunteng/myt/internal/v2host"
)

// --- V2 主机 tab：面板聚合 + 超级 SDK 管理 + 主机重启 ---
// 对齐 V2 Web hostDetail.html：超级 SDK = 云端插件元数据（get_plugins_list，
// myt_plugin_id=1000）+ 本地容器 label（myt_plugin_name/myt_plugin_ver）比对，
// 更新 = del_plugin + install_plugin 两步。

// V2SdkState 超级 SDK 状态（面板 SDK 卡）
type V2SdkState struct {
	LoggedIn   bool   `json:"loggedIn"`
	Installed  bool   `json:"installed"`
	Running    bool   `json:"running"`
	LocalVer   string `json:"localVer"`
	CloudVer   string `json:"cloudVer"`
	NeedUpdate bool   `json:"needUpdate"`
	PluginName string `json:"pluginName"`
	PluginYaml string `json:"pluginYaml"`
	PluginURL  string `json:"pluginUrl"`
}

// V2HostPanel 主机 tab 面板数据（一次聚合，前端单次 IPC）
type V2HostPanel struct {
	DeviceID string     `json:"deviceId"`
	IP       string     `json:"ip"`
	Model    string     `json:"model"`
	Version  string     `json:"version"` // Docker Server 版本
	Mac      string     `json:"mac"`
	CPU      float64    `json:"cpu"` // 占用 %
	CPUTemp  int        `json:"cpuTemp"`
	MemTotal int64      `json:"memTotal"` // MB
	MemUse   int64      `json:"memUse"`
	DiskMB   int64      `json:"diskMB"`
	DiskUse  int64      `json:"diskUse"`
	Sdk      V2SdkState `json:"sdk"`
}

// SetTokenGetter 注入「当前登录账号」token 来源（paymentSvc.GetToken，注入模式）。
func (s *V2ServiceWrapper) SetTokenGetter(fn func() (string, error)) {
	s.tokenGetter = fn
}

// GetV2HostPanel 聚合主机 tab 数据：设备缓存 + systeminfo + 超级 SDK 状态。
// 部分子项失败不整卡报错（systeminfo/云端元数据失败留空，前端显示占位）。
func (s *V2ServiceWrapper) GetV2HostPanel(deviceID string) (*V2HostPanel, error) {
	dev := s.configManager.GetDevice(deviceID)
	if dev == nil {
		return nil, fmt.Errorf("设备不存在: %s", deviceID)
	}
	panel := &V2HostPanel{
		DeviceID: dev.DeviceID,
		IP:       dev.IP,
		Model:    dev.DeviceType,
		Version:  dev.Version,
	}
	if dev.IP == "" {
		return panel, nil
	}
	panel.Mac = v2host.LookupMAC(dev.IP)
	if sys, err := s.client.SystemInfo(context.Background(), dev.IP); err == nil {
		panel.CPU = sys.CPU
		panel.CPUTemp = int(sys.Temperatures)
		const mb = 1024 * 1024
		panel.MemTotal = sys.MemTotal / mb
		panel.MemUse = sys.MemTotal * int64(sys.MemPercent) / 100 / mb
		panel.DiskMB = sys.DiskTotal / mb
		panel.DiskUse = sys.DiskTotal * int64(sys.DiskPercent) / 100 / mb
	} else {
		log.Printf("[v2] 主机面板 systeminfo 拉取失败 ip=%s: %v", dev.IP, err)
	}
	s.fillSdkState(panel, dev.IP)
	return panel, nil
}

// fillSdkState 超级 SDK 状态：云端元数据（需登录）+ 本地容器 label 比对。
func (s *V2ServiceWrapper) fillSdkState(panel *V2HostPanel, ip string) {
	sdk := &panel.Sdk
	token := ""
	if s.tokenGetter != nil {
		if t, err := s.tokenGetter(); err == nil && t != "" {
			token = t
			sdk.LoggedIn = true
		}
	}
	if sdk.LoggedIn {
		if meta := s.fetchSuperSdkMeta(token); meta != nil {
			sdk.CloudVer = meta.PluginVer
			sdk.PluginName = meta.PluginName
			sdk.PluginYaml = meta.PluginYaml
			sdk.PluginURL = meta.PluginURL
		} else {
			sdk.LoggedIn = false // 云端接口失败按未登录降级（只显示本地状态）
		}
	}
	// 本地安装状态：容器 label 比对（云端元数据在时按插件名精确匹配，缺失时
	// 只认 myt_sdk 容器——同 GetSuperSdkVersion 的匹配语义，见
	// matchSuperSdkContainer 注释）
	containers, err := s.client.ListContainers(context.Background(), ip)
	if err != nil {
		log.Printf("[v2] 主机面板容器列表拉取失败 ip=%s: %v", ip, err)
		return
	}
	if c := matchSuperSdkContainer(containers, sdk.PluginName); c != nil {
		sdk.PluginName = c.Labels["myt_plugin_name"]
		sdk.Installed = true
		sdk.LocalVer = c.Labels["myt_plugin_ver"]
		sdk.Running = strings.EqualFold(c.State, "running")
		sdk.NeedUpdate = sdk.CloudVer != "" && compareVer(sdk.LocalVer, sdk.CloudVer) < 0
	}
}

// fetchSuperSdkMeta 云端超级 SDK 插件元数据
func (s *V2ServiceWrapper) fetchSuperSdkMeta(token string) *auth.PluginInfo {
	client := &http.Client{Timeout: 15 * time.Second}
	result, err := auth.GetPluginsListAPI(client, token)
	if err != nil {
		log.Printf("[v2] 云端插件列表拉取失败: %v", err)
		return nil
	}
	return auth.FindSuperSdk(result.List)
}

// --- V2 镜像 tab：镜像源（云端）/ 主机本地镜像 的查询与操作 ---

// resolveDevice 解析设备（v2 镜像/主机操作共用）。
// 兜底链：按序列号查 → 按 IP 查（旧版添加的条目 DeviceID 为空、以 IP 为主键，
// 前端 v2 的 getDeviceId 传的是 IP）。
func (s *V2ServiceWrapper) resolveDevice(deviceID string) (*config.DeviceCache, error) {
	if dev := s.configManager.GetDevice(deviceID); dev != nil {
		return dev, nil
	}
	if dev := s.configManager.GetDeviceByIP(deviceID); dev != nil {
		return dev, nil
	}
	return nil, fmt.Errorf("设备不存在: %s", deviceID)
}

// PullV2Image 拉取镜像到主机（81 dc_api/v1/pull_image，镜像拉取分钟级）。
func (s *V2ServiceWrapper) PullV2Image(deviceID, imageAddr string) error {
	dev, err := s.resolveDevice(deviceID)
	if err != nil {
		return err
	}
	if imageAddr == "" {
		return fmt.Errorf("镜像地址为空")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	return s.client.PullImage(ctx, dev.IP, imageAddr)
}

// GetV2HostImages 主机本地镜像列表（2375 /images/json）。
func (s *V2ServiceWrapper) GetV2HostImages(deviceID string) ([]v2host.HostImage, error) {
	dev, err := s.resolveDevice(deviceID)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	images, err := s.client.ListHostImages(ctx, dev.IP)
	if err != nil {
		return nil, err
	}
	if images == nil {
		images = []v2host.HostImage{}
	}
	return images, nil
}

// DeleteV2HostImage 删除主机本地镜像（2375，被容器引用时 force 删除会失败——错误透传）。
func (s *V2ServiceWrapper) DeleteV2HostImage(deviceID, tag string) error {
	dev, err := s.resolveDevice(deviceID)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	return s.client.DeleteHostImage(ctx, dev.IP, tag)
}

// PruneV2Images 清理主机悬空镜像（81 dc_api/v1/prune_images）。
func (s *V2ServiceWrapper) PruneV2Images(deviceID string) error {
	dev, err := s.resolveDevice(deviceID)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	return s.client.PruneImages(ctx, dev.IP)
}

// UpdateSuperSdk 更新超级 SDK（对齐 V2 Web：先卸载再重装，镜像拉取分钟级）。
func (s *V2ServiceWrapper) UpdateSuperSdk(deviceID string) error {
	ip, sdk, err := s.sdkContext(deviceID)
	if err != nil {
		return err
	}
	return s.updateSuperSdkByIP(ip, sdk)
}

// updateSuperSdkByIP 按 IP 更新超级 SDK（供 SdkApiServiceWrapper 的 v2 升级
// 回调复用——taskqueue 批量升级传的是 deviceIP）。
func (s *V2ServiceWrapper) updateSuperSdkByIP(ip string, sdk *V2SdkState) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if err := s.client.PluginAction(ctx, ip, "del", sdk.PluginName, sdk.PluginYaml); err != nil {
		// 未安装时卸载会报错，继续重装（V2 Web 同语义）
		log.Printf("[v2] 超级SDK卸载步骤失败（继续重装）: %v", err)
	}
	return s.client.PluginAction(ctx, ip, "install", sdk.PluginName, sdk.PluginYaml)
}

// GetHostAndroidVersion 探测主机安卓版本（81 get_key_value ANDROID_VERSION，
// 供创建云机弹窗按 os_ver 过滤镜像）。失败返回空串——前端用默认 'and12'
// 继续过滤，不阻塞 UI（壳子同款语义）。
func (s *V2ServiceWrapper) GetHostAndroidVersion(ip string) string {
	if ip == "" {
		return ""
	}
	return s.client.GetHostAndroidVersion(ip)
}

// UpgradeSuperSdkByIP 按 IP 触发超级 SDK 更新（SdkApiServiceWrapper v2 升级
// 回调入口：taskqueue 的 sdkUpgrade handler 传 deviceIP）。
// 返回 true 表示已下发更新指令。
func (s *V2ServiceWrapper) UpgradeSuperSdkByIP(deviceIP string) (bool, error) {
	panel := &V2HostPanel{DeviceID: "", IP: deviceIP}
	s.fillSdkState(panel, deviceIP)
	if !panel.Sdk.Installed && panel.Sdk.PluginYaml == "" {
		return false, fmt.Errorf("超级 SDK 元数据不可用（未登录或云端接口失败）")
	}
	if panel.Sdk.CloudVer != "" && !panel.Sdk.NeedUpdate {
		return false, nil // 已是最新
	}
	if err := s.updateSuperSdkByIP(deviceIP, &panel.Sdk); err != nil {
		return false, err
	}
	return true, nil
}

// SuperSdkOp 超级 SDK 单步操作：install / start / stop（前端 SDK 卡按钮）。
func (s *V2ServiceWrapper) SuperSdkOp(deviceID, action string) error {
	ip, sdk, err := s.sdkContext(deviceID)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	return s.client.PluginAction(ctx, ip, action, sdk.PluginName, sdk.PluginYaml)
}

// matchSuperSdkContainer 从容器列表中匹配超级 SDK 容器。
// pluginName 非空时只按 myt_plugin_name 精确匹配（云端元数据在场的严格语义）；
// 为空时优先取 myt_plugin_name=="myt_sdk" 的容器（超级 SDK 固定容器名/label，
// 实测主机上 myt_vpc_plugin 等其他插件容器也带 myt_plugin_name label 且顺序
// 不定，「取第一个带 label 的」会错拿 vpc 插件的版本），无 myt_sdk 时返回 nil
// （未装超级 SDK 就不该把其他插件当成它）。
func matchSuperSdkContainer(containers []v2host.ContainerInfo, pluginName string) *v2host.ContainerInfo {
	for i := range containers {
		c := &containers[i]
		if pluginName != "" && c.Labels["myt_plugin_name"] == pluginName {
			return c
		}
	}
	if pluginName != "" {
		return nil
	}
	for i := range containers {
		c := &containers[i]
		if c.Labels["myt_plugin_name"] == "myt_sdk" {
			return c
		}
	}
	return nil
}

// GetSuperSdkVersion 超级 SDK 本地版本（轻量查询：只走 2375 容器列表读
// myt_plugin_ver label，不查云端插件元数据/systeminfo/MAC——DeviceInfoBar
// 的 v2 版本号展示用，未安装或查询失败返回空串，前端静默显示占位）。
func (s *V2ServiceWrapper) GetSuperSdkVersion(deviceID string) string {
	dev, err := s.resolveDevice(deviceID) // GetDeviceByIP 兜底（v2 旧数据 device_id 为空时前端传 IP）
	if err != nil {
		return ""
	}
	containers, err := s.client.ListContainers(context.Background(), dev.IP)
	if err != nil {
		return ""
	}
	if c := matchSuperSdkContainer(containers, ""); c != nil {
		return c.Labels["myt_plugin_ver"]
	}
	return ""
}

// RebootV2Host 重启主机（二次确认在前端）。
func (s *V2ServiceWrapper) RebootV2Host(deviceID string) error {
	dev := s.configManager.GetDevice(deviceID)
	if dev == nil {
		return fmt.Errorf("设备不存在: %s", deviceID)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	err := s.client.RebootHost(ctx, dev.IP)
	if err == nil {
		// 重启后主机/容器状态会短暂离线，立即标记等待心跳校准
		s.configManager.UpdateDeviceState(deviceID, 1)
		s.emitDeviceList()
	}
	return err
}

// sdkContext 取设备 IP + 最新 SDK 元数据（操作前刷新云端/本地状态）。
func (s *V2ServiceWrapper) sdkContext(deviceID string) (string, *V2SdkState, error) {
	dev := s.configManager.GetDevice(deviceID)
	if dev == nil {
		return "", nil, fmt.Errorf("设备不存在: %s", deviceID)
	}
	panel := &V2HostPanel{DeviceID: dev.DeviceID, IP: dev.IP}
	s.fillSdkState(panel, dev.IP)
	if !panel.Sdk.Installed && panel.Sdk.PluginYaml == "" {
		return "", nil, fmt.Errorf("超级 SDK 元数据不可用（未登录或云端接口失败）")
	}
	return dev.IP, &panel.Sdk, nil
}

// compareVer 语义化版本比较（对齐 V2 Web compareVersion）：
// 返回 -1/0/1 = a<b / a==b / a>b。忽略非数字前缀（v1.2.3 → 1.2.3）。
func compareVer(a, b string) int {
	pa := verParts(a)
	pb := verParts(b)
	n := len(pa)
	if len(pb) > n {
		n = len(pb)
	}
	for i := 0; i < n; i++ {
		var x, y int
		if i < len(pa) {
			x = pa[i]
		}
		if i < len(pb) {
			y = pb[i]
		}
		if x != y {
			if x > y {
				return 1
			}
			return -1
		}
	}
	return 0
}

func verParts(v string) []int {
	v = strings.TrimLeft(strings.TrimSpace(v), "vV ")
	parts := strings.Split(v, ".")
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		num := 0
		for _, r := range p {
			if r < '0' || r > '9' {
				break
			}
			num = num*10 + int(r-'0')
		}
		out = append(out, num)
	}
	return out
}
