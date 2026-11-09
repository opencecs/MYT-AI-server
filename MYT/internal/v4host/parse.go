package v4host

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// V4 /health 的响应契约已由 swagger（tobox_player/api.json）确认：只有 {"status":"ok"}，
// **不含任何设备标识字段**。因此：
//   - 设备 key 一律用 IP（同 IP 视为同设备；设备换 IP 会被当成新设备，这是已知限制）
//   - 不再猜测 deviceId / id / serial 等字段（早期按常见字段名递归查找的做法已移除，
//     那会把 /info、/health 里偶然出现的无关字段误当成设备 ID）
//
// 显示名/型号仍做容错查找（devicename/name/model）——当前 /health 不返回这些字段，
// 但一旦服务端补上，客户端零改动即可展示；查不到时调用方退回 UDP 广播里的设备名。

const maxLookupDepth = 3

// nameKeys 显示名候选字段（归一化后，优先级从高到低）
var nameKeys = []string{"devicename", "name", "model"}

// modelKeys 型号候选字段（归一化后）
var modelKeys = []string{"model", "devicemodel"}

// parseInfo 解析 /health 响应体：非 JSON / 非 JSON 对象 = 失败；字段缺失容错。
// DeviceID 恒为 IP（见文件头说明）。
func parseInfo(body []byte, ip string) (*Info, error) {
	var root any
	if err := json.Unmarshal(body, &root); err != nil {
		return nil, fmt.Errorf("/health 响应不是合法 JSON: %w", err)
	}
	obj, ok := root.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("/health 响应不是 JSON 对象")
	}

	return &Info{
		IP:       ip,
		DeviceID: ip, // V4 无设备标识字段：IP 即身份
		// DeviceName / Model 为容错查找（当前契约下通常为空）
		DeviceName:     findString(obj, nameKeys),
		Model:          findString(obj, modelKeys),
		DeviceIDFromIP: true,
		RawBody:        string(body),
	}, nil
}

// findString 按候选 key 优先级查找字符串值。
func findString(obj map[string]any, keys []string) string {
	for _, want := range keys {
		if v := lookupString(obj, want, 0); v != "" {
			return v
		}
	}
	return ""
}

// lookupString 在 obj 中递归查找归一化后等于 want 的字段值。
// 同层优先，逐层下探；同层 key 排序遍历，保证结果稳定（map 迭代本身无序）。
func lookupString(v any, want string, depth int) string {
	if depth > maxLookupDepth {
		return ""
	}
	obj, ok := v.(map[string]any)
	if !ok {
		return ""
	}
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if normalizeKey(k) != want {
			continue
		}
		if s, ok := scalarString(obj[k]); ok && s != "" {
			return s
		}
	}
	for _, k := range keys {
		if s := lookupString(obj[k], want, depth+1); s != "" {
			return s
		}
	}
	return ""
}

// normalizeKey 归一化字段名：忽略大小写、下划线与连字符（device_id / deviceId → deviceid）
func normalizeKey(k string) string {
	k = strings.ToLower(strings.TrimSpace(k))
	k = strings.ReplaceAll(k, "_", "")
	k = strings.ReplaceAll(k, "-", "")
	return k
}

// scalarString 把标量值转成字符串（字符串原样，数字按整数/小数格式化）。
func scalarString(v any) (string, bool) {
	switch val := v.(type) {
	case string:
		return strings.TrimSpace(val), true
	case float64:
		return strconv.FormatFloat(val, 'f', -1, 64), true
	case json.Number:
		return val.String(), true
	case bool:
		return strconv.FormatBool(val), true
	}
	return "", false
}
