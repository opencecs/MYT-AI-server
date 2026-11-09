//go:build linux

package updater

import (
	"os"
	"strings"
)

// isSubPathCheck Linux 下大小写敏感比较
func isSubPathCheck(target, base string) bool {
	absTarget, err := absPath(target)
	if err != nil {
		return false
	}
	absBase, err := absPath(base)
	if err != nil {
		return false
	}
	return strings.HasPrefix(absTarget, absBase+string(os.PathSeparator))
}

// fallbackExeName Linux 下可执行文件无后缀
func fallbackExeName() string {
	return "MYT"
}

// postInstall Linux 无需后处理（无 bundle 签名机制）
func postInstall(exeDir string) {}
