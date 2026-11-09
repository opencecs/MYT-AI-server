package sdkapi

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

type DeviceInfo struct {
	DeviceID string `json:"deviceId"`
	Model    string `json:"model"`
	Version  string `json:"version"`
	HwAddr   string `json:"hwaddr"`
	IP       string `json:"ip"`
	CPUTemp  int    `json:"cputemp"`
	CPULoad  string `json:"cpuload"`
	MemTotal int64  `json:"memtotal"`
	MemUse   int64  `json:"memuse"`
	MMCTotal int64  `json:"mmctotal"`
	MMCUse   int64  `json:"mmcuse"`
	MMCModel string `json:"mmcmodel"`
	Speed    string `json:"speed"`
}

// rawDeviceInfo 用于解析设备 API 返回的 JSON（磁盘/内存字段可能是字符串、数字或空字符串）
type rawDeviceInfo struct {
	DeviceID string `json:"deviceId"`
	Model    string `json:"model"`
	Version  string `json:"version"`
	HwAddr   string `json:"hwaddr"`
	IP       string `json:"ip"`
	CPUTemp  int    `json:"cputemp"`
	CPULoad  string `json:"cpuload"`
	MemTotal string `json:"memtotal"`
	MemUse   string `json:"memuse"`
	MMCTotal string `json:"mmctotal"`
	MMCUse   string `json:"mmcuse"`
	MMCModel string `json:"mmcmodel"`
	Speed    string `json:"speed"`
}

func parseNum(s string) int64 {
	if s == "" {
		return 0
	}
	v, err := strconv.ParseInt(s, 10, 64)
	if err == nil {
		return v
	}
	f, err := strconv.ParseFloat(s, 64)
	if err == nil {
		return int64(f)
	}
	return 0
}

type NetworkInfo struct {
	Gateway      string `json:"gateway"`
	NetworkCIDR  string `json:"networkCIDR"`
	Interface    string `json:"interface"`
}

type networkInfoWrapper struct {
	Info NetworkInfo `json:"info"`
}

// APIVersionInfo 设备 API 版本信息
type APIVersionInfo struct {
	CurrentVersion int `json:"currentVersion"`
	LatestVersion  int `json:"latestVersion"`
	// v2 设备（myt-sdk-go-v2）版本号为 string（可能非纯数字）。V3 恒为空串，
	// 前端展示时 StringCurrentVersion 为空则回退 CurrentVersion，V3 行为不变。
	StringCurrentVersion string `json:"stringCurrentVersion,omitempty"`
	StringLatestVersion  string `json:"stringLatestVersion,omitempty"`
	// Description 云端最新版本的描述（/sdk_ver/latest files[0].description，升级
	// 确认弹窗展示用）。设备 /info 不回该字段，由 SdkCheckUpgrade 从云端查询补齐；
	// 云端不可知时为空串（前端不展示描述块）。
	Description string `json:"description,omitempty"`
}

// rawAPIVersionInfo 用于解析 /info 原始响应：V3 currentVersion/latestVersion 为
// 数字，v2 为 string——用 any 接住再按类型归一，避免一种结构解析两种设备。
type rawAPIVersionInfo struct {
	CurrentVersion any `json:"currentVersion"`
	LatestVersion  any `json:"latestVersion"`
	// Version 老 v2 SDK（myt-sdk-go-v2 早期版本）的版本字段名（如 "1"、"2.0.0"），
	// 无 currentVersion——解析时回退读取，否则版本折成 0（列表不显示版本徽标）。
	Version any `json:"version"`
	Status  any `json:"status"`
}

// versionToString 版本字段归一：数字（V3）与字符串（v2）统一转 string，
// 空/解析失败返回空串。
func versionToString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case float64:
		if x == float64(int(x)) {
			return strconv.Itoa(int(x))
		}
		return strconv.FormatFloat(x, 'f', -1, 64)
	default:
		return ""
	}
}

// versionToInt 版本字段转 int（V3 路径用）：非纯数字返回 0。
func versionToInt(v any) int {
	switch x := v.(type) {
	case float64:
		return int(x)
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(x))
		if err != nil {
			return 0
		}
		return n
	default:
		return 0
	}
}

// GetAPIVersionInfo 获取设备 API 版本信息（当前版本 + 最新版本）
func (c *Client) GetAPIVersionInfo() (*APIVersionInfo, error) {
	body, err := c.get("/info")
	if err != nil {
		return nil, err
	}
	resp, err := parseAPIResponse(body)
	if err != nil {
		return nil, err
	}
	var raw rawAPIVersionInfo
	if err := json.Unmarshal(resp.Data, &raw); err != nil {
		return nil, err
	}
	// 老 v2 SDK 无 currentVersion，版本在 version 字段——回退读取
	current, latest := raw.CurrentVersion, raw.LatestVersion
	if current == nil && raw.Version != nil {
		current = raw.Version
	}
	return &APIVersionInfo{
		CurrentVersion:       versionToInt(current),
		LatestVersion:        versionToInt(latest),
		StringCurrentVersion: versionToString(current),
		StringLatestVersion:  versionToString(latest),
	}, nil
}

func (c *Client) GetAPIVersion() (string, error) {
	// 先尝试解析为 APIVersionInfo
	body, err := c.get("/info")
	if err != nil {
		return "", err
	}
	resp, err := parseAPIResponse(body)
	if err != nil {
		return "", err
	}
	var info APIVersionInfo
	if err := json.Unmarshal(resp.Data, &info); err == nil && (info.CurrentVersion > 0 || info.StringCurrentVersion != "") {
		if info.StringCurrentVersion != "" {
			return info.StringCurrentVersion, nil
		}
		return fmt.Sprintf("%d", info.CurrentVersion), nil
	}
	// 降级：复用已有的 resp.Data 解析为字符串
	var version string
	if err := json.Unmarshal(resp.Data, &version); err != nil {
		return string(resp.Data), nil
	}
	return version, nil
}

func (c *Client) GetDeviceInfo() (*DeviceInfo, error) {
	body, err := c.get("/info/device")
	if err != nil {
		return nil, err
	}
	resp, err := parseAPIResponse(body)
	if err != nil {
		return nil, err
	}
	var raw rawDeviceInfo
	if err := json.Unmarshal(resp.Data, &raw); err != nil {
		return nil, err
	}
	// model 兜底：部分设备/镜像 /info/device 成功但 model 为空——从固件版本
	// 解析（"QL-r1q-2026.v0.8.8.202608110721-v3" → "r1q_v3"），避免设备信息
	// 显示与 public 镜像过滤因 model 缺失而空
	if raw.Model == "" {
		raw.Model = ParseModelFromFirmware(raw.Version)
	}

	return &DeviceInfo{
		DeviceID: raw.DeviceID,
		Model:    raw.Model,
		Version:  raw.Version,
		HwAddr:   raw.HwAddr,
		IP:       raw.IP,
		CPUTemp:  raw.CPUTemp,
		CPULoad:  raw.CPULoad,
		MemTotal: parseNum(raw.MemTotal),
		MemUse:   parseNum(raw.MemUse),
		MMCTotal: parseNum(raw.MMCTotal),
		MMCUse:   parseNum(raw.MMCUse),
		MMCModel: raw.MMCModel,
		Speed:    raw.Speed,
	}, nil
}

// firmwareGenRe 固件版本尾部的版本代标记（"-v3"、"v29" 等）
var firmwareGenRe = regexp.MustCompile(`-v(\d+)$`)

// ParseModelFromFirmware 从固件版本解析机型（/info/device 与 hello 的 model 为空时的兜底）。
// 实测格式（192.0.2.202/205）："QL-r1q-2026.v0.8.8.202608110721-v3"——段2 为机型
// base，尾段 vN 为版本代，拼接得 "r1q_v3"（与 model 字段返回值一致）。
// 段数不足/段2为空/输入为空时返回空串（调用方保持原状，不覆盖已有数据）。
func ParseModelFromFirmware(version string) string {
	if version == "" {
		return ""
	}
	parts := strings.Split(version, "-")
	if len(parts) < 3 || parts[1] == "" {
		return ""
	}
	if m := firmwareGenRe.FindStringSubmatch(version); m != nil {
		return parts[1] + "_v" + m[1]
	}
	return parts[1]
}

func (c *Client) GetNetworkInfo() (*NetworkInfo, error) {
	body, err := c.get("/server/network")
	if err != nil {
		return nil, err
	}
	resp, err := parseAPIResponse(body)
	if err != nil {
		return nil, err
	}
	var wrapper networkInfoWrapper
	if err := json.Unmarshal(resp.Data, &wrapper); err != nil {
		return nil, err
	}
	return &wrapper.Info, nil
}

func (c *Client) RestartDevice() error {
	body, err := c.postJSON("/server/device/reboot", map[string]string{})
	if err != nil {
		return err
	}
	return c.parseMutationResponse(body)
}
