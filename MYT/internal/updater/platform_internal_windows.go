//go:build windows

package updater

import (
	"os"
	"strings"
)

// isSubPathCheck Windows 下做大小写不敏感比较
func isSubPathCheck(target, base string) bool {
	absTarget, err := absPath(target)
	if err != nil {
		return false
	}
	absBase, err := absPath(base)
	if err != nil {
		return false
	}
	return strings.HasPrefix(strings.ToLower(absTarget), strings.ToLower(absBase)+string(os.PathSeparator))
}

// fallbackExeName Windows 下 exe 带后缀
func fallbackExeName() string {
	return "MYT.exe"
}

// postInstall Windows 无需后处理（exe 文件级签名，替换后不影响）
func postInstall(exeDir string) {}
