package config

import (
	"strconv"
	"strings"
)

// ContainerDisplayName 容器名的投屏窗口标题显示名（Go 版，与前端
// frontend/src/utils/containerName.ts 的 containerDisplayName 逐条对齐）：
//
// 两种命名规范都处理：
//  1. 壳子式：`{设备ID}_{坑位号}_{名称段}`（如 c043b1cf..._1_T1001）；
//  2. pc-client 式：`{用户名|时间戳}_{坑位号}_{实例序号}`（如 myname_4_1、1729012345678_4_1），
//     第一段不是 deviceId。
//
// 统一规则：按 `_` 分割，段数 ≥3 时取第三段及以后 join（名称段本身可能含 `_`）；
// 段数 <3 说明名称段缺失，不拆、原样返回。
//
// 坑位号防御：用户输入名可能自带 `_`（如用户名 `my_name` → 容器 `my_name_4_1` 共 4 段），
// 此时第三段是坑位号、直接取 slice(2) 会把「坑位号_实例序号」当显示名。防御：
// 段数 ≥4 且第三段为纯数字且等于 indexNum（说明取到的是坑位号）→ 改取第四段及以后；
// 取出为空则显示原名。注意：① 判定键是第三段而非 slice(2) 结果——否则实例序号恰等于
// indexNum 的正常 3 段名（如 `1729012345678_4_4`）会被误退回原名；② 需段数 ≥4 才触发
// ——3 段名的第三段是实例序号而非坑位号。
//
// 兜底：name 为空、或拆分出的名称段为空时，返回 `T{1000+indexNum}`。
func ContainerDisplayName(name, deviceID string, indexNum int) string {
	_ = deviceID // 与前端签名对齐保留；当前规则不要求前缀匹配 deviceId
	if name == "" {
		return "T" + strconv.Itoa(1000+indexNum)
	}
	parts := strings.Split(name, "_")
	if len(parts) < 3 {
		return name
	}
	if len(parts) >= 4 && allDigits(parts[2]) && parts[2] == strconv.Itoa(indexNum) {
		if inst := strings.Join(parts[3:], "_"); inst != "" {
			return inst
		}
		return name
	}
	if remark := strings.Join(parts[2:], "_"); remark != "" {
		return remark
	}
	return "T" + strconv.Itoa(1000+indexNum)
}

// allDigits 判断 s 是否为非空纯数字串（对应前端 /^\d+$/）。
func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
