package main

import (
	"encoding/json"
	"log"
	"strings"

	"github.com/moyunteng/myt/internal/cloudapi"
	"github.com/moyunteng/myt/internal/config"
	"github.com/moyunteng/myt/internal/sdkapi"
)

// buildPublicDeviceList 构造 public 模式的新设备列表（discoverFromCloud 阶段1 用）。
// 以旧条目为基础只覆盖身份字段——Order/State/DeviceType 及磁盘/内存指标
// （DiskUsed/MemUsed 等，由 WS system/stats 写回）全部随旧条目保留。
// 30s 轮询全量替换若不保留指标字段，存储条会在「替换清零 ↔ 20s stats 回填」间循环闪烁。
// 新设备（不在旧列表）State 置 1（离线），由 eventws WS 心跳确认后升为在线；型号留空由阶段2补全。
//
// 网关 online=false 的设备（设备侧断连/网关心跳超时）不等于「设备已下架」：
// 若它已在旧列表中，保留条目并把 State 置离线——否则一次网关抖动就会把设备从
// 列表里抹掉（随后 5s 落档写盘，用户看到设备消失）。真正下架（网关列表里不再出现）
// 仍会被移除；只有网关返回空列表（total==0）才整体清空。
func buildPublicDeviceList(devices []cloudapi.GatewayDevice, oldList []config.DeviceCache) []config.DeviceCache {
	oldByID := make(map[string]config.DeviceCache, len(oldList))
	for _, d := range oldList {
		oldByID[d.DeviceID] = d
	}
	newList := make([]config.DeviceCache, 0, len(devices))
	for _, dev := range devices {
		item, known := oldByID[dev.DeviceID]
		if !known {
			// 新设备初始离线（State=1）：零值恰好是在线态(0)，会让从未建连的
			// 设备「零值即在线」假活。由 eventws WS 心跳（拨通+snapshot）确认
			// 后经 reportState 升为在线；WS/token 建不了连的设备保持离线显示。
			if !dev.Online {
				// 网关标记离线且从未进过列表：不新增（列表只收「在线过/曾入列表」的设备，
				// 避免把账号下长期离线设备灌进列表）。状态由 WS 心跳确认后再入列。
				continue
			}
			item.State = 1
		} else if !dev.Online {
			// 已在列表但网关标记离线：保留条目，状态置离线（不删数据）
			item.State = 1
		}
		item.DeviceID = dev.DeviceID
		item.IP = "" // public 模式无局域网 IP，走 device_id 转发
		item.DeviceName = dev.DeviceID
		item.Version = ""
		newList = append(newList, item)
	}
	return newList
}

// fetchDeviceTypeFromCloud 公网模式下通过网关透传 /info/device 获取设备主板型号，
// 用于镜像列表过滤（filterContainerMirrorsByDevice 需要 deviceType 前缀匹配）。
//
// 网关 /v1/devices 返回的 type 字段恒为空，设备 SDK /info/device 返回 model 字段
// 形如 "r1q_v3"，去掉 "_vN" 后缀即为设备类型 "r1q"（与镜像 ttype2 标签 "r1q_v3" 前缀匹配）。
//
// 失败时返回空字符串（不阻断发现流程，镜像列表会为空但不会崩溃）。
func fetchDeviceTypeFromCloud(client *cloudapi.Client, deviceID, devicePassword string) string {
	respBody, status, err := client.DeviceAction("GET", "/passthrough/info/device", deviceID, nil, devicePassword)
	if err != nil {
		log.Printf("[discover] public 获取设备型号失败: device=%s err=%v", deviceID, err)
		return ""
	}
	if status != 200 {
		log.Printf("[discover] public 获取设备型号失败: device=%s status=%d", deviceID, status)
		return ""
	}
	var result struct {
		Code int             `json:"code"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return ""
	}
	var info struct {
		Model   string `json:"model"`
		Version string `json:"version"`
	}
	if err := json.Unmarshal(result.Data, &info); err != nil {
		return ""
	}
	// model 兜底：部分设备 model 为空但固件版本有值——从固件版本解析
	// （与 GetDeviceInfo/HandleHello 同源），否则 deviceType 为空导致镜像列表全空
	if info.Model == "" {
		info.Model = sdkapi.ParseModelFromFirmware(info.Version)
	}
	return normalizeDeviceType(info.Model)
}

// normalizeDeviceType 从 model 字段提取设备类型：去掉末尾的 "_vN" 版本后缀。
// "r1q_v3" → "r1q"；"r1q" → "r1q"；"q1_v2" → "q1"。
func normalizeDeviceType(model string) string {
	model = strings.TrimSpace(model)
	if model == "" {
		return ""
	}
	// 去掉末尾的 _vN（N 为数字）
	idx := strings.LastIndex(model, "_v")
	if idx > 0 && idx < len(model)-2 {
		suffix := model[idx+1:]
		if len(suffix) >= 2 && suffix[0] == 'v' && suffix[1] >= '0' && suffix[1] <= '9' {
			return model[:idx]
		}
	}
	return model
}

// getDevicePasswordSafe 安全获取设备密码（无密码时返回空字符串）
func getDevicePasswordSafe(s *DiscoverServiceWrapper, deviceID string) string {
	if s.sdkApiSvc == nil {
		return ""
	}
	return s.sdkApiSvc.GetDevicePassword(deviceID)
}
