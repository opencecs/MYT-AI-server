package updater

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// updateMu 防止并发更新
var updateMu sync.Mutex

// VerifyChecksum 校验文件 SHA256，checksum 为纯 hex 字符串；为空则跳过
func VerifyChecksum(filePath, checksum string) error {
	if checksum == "" {
		log.Printf("[updater] 跳过校验（后端未返回 checksum）")
		return nil
	}

	var hash string
	if algo, after, found := strings.Cut(checksum, ":"); found {
		if algo != "sha256" {
			return fmt.Errorf("不支持的校验格式: %s", checksum)
		}
		hash = after
	} else {
		hash = checksum
	}

	f, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("打开文件失败: %w", err)
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return fmt.Errorf("计算校验失败: %w", err)
	}

	actual := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(actual, hash) {
		return fmt.Errorf("校验失败: 期望 %s, 实际 %s", hash, actual)
	}

	log.Printf("[updater] SHA256 校验通过")
	return nil
}

// PerformInstall 解压并替换文件
func PerformInstall(zipPath string) error {
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("获取可执行文件路径失败: %w", err)
	}
	exeDir := filepath.Dir(exePath)
	exeName := filepath.Base(exePath)

	// 在安装目录所在卷创建临时目录，避免跨卷 Rename 失败
	tmpDir, err := os.MkdirTemp(exeDir, "myt-update-*")
	if err != nil {
		return fmt.Errorf("创建临时目录失败: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	if err := unzipAll(zipPath, tmpDir); err != nil {
		return fmt.Errorf("解压失败: %w", err)
	}

	// 写入更新状态标记（断电恢复用）
	statusFile := filepath.Join(exeDir, ".myt-updating")
	os.WriteFile(statusFile, []byte(exeName), 0644)

	if err := copyUpdateFiles(tmpDir, exeDir, exeName); err != nil {
		// 替换失败，尝试回滚
		rollbackUpdate(exeDir, exeName)
		os.Remove(statusFile)
		return fmt.Errorf("替换文件失败: %w", err)
	}

	// 替换成功，删除状态标记
	os.Remove(statusFile)

	if err := os.Chmod(exePath, 0755); err != nil {
		log.Printf("[updater] 设置可执行权限失败: %v", err)
	}

	// 平台特定后处理：macOS 需要重新签名 bundle（替换 myt 后签名失效）
	postInstall(exeDir)

	return nil
}

// CleanupOldFiles 清理上次更新遗留的 .old/.new 文件，应在启动时调用
func CleanupOldFiles() {
	exePath, err := os.Executable()
	if err != nil {
		return
	}
	for _, suffix := range []string{".old", ".new"} {
		p := exePath + suffix
		if _, err := os.Stat(p); err != nil {
			continue
		}
		log.Printf("[updater] 清理旧文件: %s", p)
		for i := 1; i <= 10; i++ {
			if err := os.Remove(p); err == nil {
				break
			}
			time.Sleep(500 * time.Millisecond)
		}
	}
}

// CheckIncompleteUpdate 检查是否有未完成的更新（断电恢复），应在启动时调用
func CheckIncompleteUpdate() {
	exePath, err := os.Executable()
	if err != nil {
		return
	}
	exeDir := filepath.Dir(exePath)
	statusFile := filepath.Join(exeDir, ".myt-updating")

	data, err := os.ReadFile(statusFile)
	if err != nil {
		return // 无状态文件，正常
	}

	exeName := strings.TrimSpace(string(data))
	oldPath := filepath.Join(exeDir, exeName+".old")
	newPath := filepath.Join(exeDir, exeName+".new")

	// 情况1: .new 存在，.old 也存在 → 步骤3未完成，尝试恢复
	if _, err := os.Stat(newPath); err == nil {
		if _, err := os.Stat(oldPath); err == nil {
			log.Printf("[updater] 检测到未完成更新，尝试恢复 .new → %s", exeName)
			dst := filepath.Join(exeDir, exeName)
			if err := os.Rename(newPath, dst); err != nil {
				if moveErr := moveFileReplace(newPath, dst); moveErr != nil {
					log.Printf("[updater] 恢复失败: %v", moveErr)
				}
			}
		}
	}

	// 情况2: .old 存在但 .new 不存在 → 步骤3已完成但 .old 未清理，恢复旧文件
	if _, err := os.Stat(oldPath); err == nil {
		dst := filepath.Join(exeDir, exeName)
		if _, err := os.Stat(dst); err != nil {
			// 目标文件不存在，用 .old 恢复
			log.Printf("[updater] 目标文件丢失，从 .old 恢复")
			os.Rename(oldPath, dst)
		}
	}

	os.Remove(statusFile)
}

// AcquireUpdateLock 获取更新锁，防止并发更新
func AcquireUpdateLock() bool {
	return updateMu.TryLock()
}

// ReleaseUpdateLock 释放更新锁
func ReleaseUpdateLock() {
	updateMu.Unlock()
}

// RunUpdateMode --update 模式入口：等待父进程退出 → 解压替换 → 清理 → 启动新版本
func RunUpdateMode(zipPath string, parentPID int) {
	log.Printf("[updater] --update 模式启动, zip=%s, parentPID=%d", zipPath, parentPID)

	// 等待父进程退出
	if parentPID > 0 {
		if err := waitForProcessExit(parentPID, 60*time.Second); err != nil {
			showUpdateError(fmt.Sprintf("更新失败: 等待原程序退出超时"))
			log.Fatalf("[updater] 等待父进程退出失败: %v", err)
		}
		// 父进程退出后，等待文件句柄释放（杀毒软件等可能短暂持有）
		exePath, _ := os.Executable()
		if err := waitForFileAccessible(exePath, 10*time.Second); err != nil {
			log.Printf("[updater] 等待文件解锁超时，尝试继续: %v", err)
		}
	}

	if err := PerformInstall(zipPath); err != nil {
		showUpdateError(fmt.Sprintf("更新失败: %v", err))
		log.Fatalf("[updater] 安装失败: %v", err)
	}

	// 清理 zip 临时目录
	zipDir := filepath.Dir(zipPath)
	if strings.HasPrefix(filepath.Base(zipDir), "myt-update-") {
		os.RemoveAll(zipDir)
	}

	// 启动新版本
	exePath, _ := os.Executable()
	cmd := exec.Command(exePath, "--restart-from-update")
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.Stdin = nil
	if err := cmd.Start(); err != nil {
		showUpdateError(fmt.Sprintf("启动新版本失败: %v", err))
		log.Fatalf("[updater] 启动新版本失败: %v", err)
	}

	log.Printf("[updater] 更新完成，退出更新进程")
	os.Exit(0)
}

// RestartApp 启动新进程并退出当前应用
func RestartApp() error {
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("获取程序路径失败: %w", err)
	}

	cmd := exec.Command(exePath, os.Args[1:]...)
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.Stdin = nil
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动新进程失败: %w", err)
	}

	os.Exit(0)
	return nil
}

// StartNormalUpdate 无需提权时启动 --update 子进程
func StartNormalUpdate(zipPath string, parentPID int) error {
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("获取可执行文件路径失败: %w", err)
	}
	cmd := exec.Command(exePath, "--update", "--zip", zipPath, "--pid", fmt.Sprintf("%d", parentPID))
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.Stdin = nil
	return cmd.Start()
}
