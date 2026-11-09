//go:build darwin

package clientreport

import (
	"os/exec"
	"strings"
)

// OSName 返回客户端操作系统标识（例：macOS 14.5）。
// sw_vers 不可用时退化为 "macOS"，不影响上报。
func OSName() string {
	out, err := exec.Command("sw_vers", "-productVersion").Output()
	if err != nil {
		return "macOS"
	}
	version := strings.TrimSpace(string(out))
	if version == "" {
		return "macOS"
	}
	return "macOS " + version
}
