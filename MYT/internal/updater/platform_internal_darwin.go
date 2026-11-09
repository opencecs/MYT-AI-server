//go:build darwin

package updater

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// isSubPathCheck macOS 下大小写敏感比较
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

// fallbackExeName macOS 下可执行文件无后缀
func fallbackExeName() string {
	return "MYT"
}

// postInstall macOS 替换 myt 后必须重新签名 .app bundle，否则 CodeResources 哈希
// 不匹配 → codesign --verify 失败 → Gatekeeper 拒绝启动。
// 从 exeDir（Contents/MacOS）向上找 .app 根，用 ad-hoc + entitlements 重新签名。
func postInstall(exeDir string) {
	// exeDir = .../Contents/MacOS，向上两级 = .app bundle 根
	macosDir := exeDir
	contentsDir := filepath.Dir(macosDir)
	appBundle := filepath.Dir(contentsDir)
	if !strings.HasSuffix(appBundle, ".app") {
		return
	}

	// 查找 entitlements（bundle 内或 build/darwin/）
	entitlements := ""
	for _, p := range []string{
		filepath.Join(contentsDir, "entitlements.plist"),
		filepath.Join(appBundle, "..", "..", "build", "darwin", "entitlements.plist"),
	} {
		if _, err := os.Stat(p); err == nil {
			entitlements = p
			break
		}
	}

	args := []string{"--force", "--deep", "--sign", "-"}
	if entitlements != "" {
		args = append(args, "--entitlements", entitlements)
	}
	args = append(args, appBundle)

	cmd := exec.Command("codesign", args...)
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Run(); err != nil {
		// 签名失败不致命（可能 codesign 不存在），记录即可
		// 用户首次打开可能需要右键→打开授权
	} else {
		// 清理 provenance 属性，避免 Gatekeeper 静默拒绝
		exec.Command("xattr", "-cr", appBundle).Run()
	}
}
