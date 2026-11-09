//go:build windows

package clientreport

import (
	"strconv"
	"strings"

	"golang.org/x/sys/windows/registry"
)

// OSName 返回客户端操作系统标识（例：Windows 11 Pro 23H2）。
//
// 读注册表而非 runtime.GOOS：上报的 os 字段用于统计客户端系统分布，
// 只写 "windows" 没有区分度。取值失败时退化为 "Windows"，不影响上报。
func OSName() string {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE,
		`SOFTWARE\Microsoft\Windows NT\CurrentVersion`, registry.QUERY_VALUE)
	if err != nil {
		return "Windows"
	}
	defer k.Close()

	product, _, _ := k.GetStringValue("ProductName")
	display, _, _ := k.GetStringValue("DisplayVersion")
	build, _, _ := k.GetStringValue("CurrentBuildNumber")

	// Windows 11 的 ProductName 注册表值仍是 "Windows 10 Pro"（系统已知行为），
	// 用 build 号纠正：>= 22000 即 Windows 11。
	if n, err := strconv.Atoi(build); err == nil && n >= 22000 {
		product = strings.Replace(product, "Windows 10", "Windows 11", 1)
	}

	parts := make([]string, 0, 2)
	if product != "" {
		parts = append(parts, product)
	}
	if display != "" {
		parts = append(parts, display)
	} else if build != "" {
		parts = append(parts, "Build "+build)
	}
	if len(parts) == 0 {
		return "Windows"
	}
	return strings.Join(parts, " ")
}
