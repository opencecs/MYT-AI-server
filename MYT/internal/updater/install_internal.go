package updater

import (
	"archive/zip"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"time"
)

// --- 内部函数 ---

// waitForFileAccessible 等待文件可写入（句柄已释放），用重试替代固定 sleep
func waitForFileAccessible(path string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	interval := 200 * time.Millisecond
	for time.Now().Before(deadline) {
		// 尝试以追加模式打开文件，检测是否还被锁定
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
		if err == nil {
			f.Close()
			return nil
		}
		time.Sleep(interval)
	}
	return fmt.Errorf("等待文件解锁超时: %s", path)
}

// rollbackUpdate 回滚更新：将 .old 恢复为原始文件名
func rollbackUpdate(exeDir, exeName string) {
	oldPath := filepath.Join(exeDir, exeName+".old")
	dst := filepath.Join(exeDir, exeName)

	if _, err := os.Stat(oldPath); err != nil {
		return // 无 .old 文件，无法回滚
	}

	log.Printf("[updater] 尝试回滚: %s → %s", oldPath, dst)
	if err := os.Rename(oldPath, dst); err != nil {
		if moveErr := moveFileReplace(oldPath, dst); moveErr != nil {
			log.Printf("[updater] 回滚失败: %v", moveErr)
		}
	}
}

func waitForProcessExit(pid int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !isProcessRunning(pid) {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("等待进程 %d 退出超时", pid)
}

func unzipAll(zipPath, destDir string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer r.Close()

	var totalSize uint64
	const maxTotalSize uint64 = 500 * 1024 * 1024 // 500MB 上限

	for _, f := range r.File {
		if f.UncompressedSize64 > maxTotalSize {
			return fmt.Errorf("文件过大: %s (%d bytes)", f.Name, f.UncompressedSize64)
		}
		totalSize += f.UncompressedSize64
	}
	if totalSize > maxTotalSize {
		return fmt.Errorf("解压总大小超限: %d bytes", totalSize)
	}

	for _, f := range r.File {
		if f.FileInfo().IsDir() {
			dirPath := filepath.Join(destDir, f.Name)
			if !isSubPath(dirPath, destDir) {
				return fmt.Errorf("非法路径: %s", f.Name)
			}
			os.MkdirAll(dirPath, 0755)
			continue
		}

		outPath := filepath.Join(destDir, f.Name)
		if !isSubPath(outPath, destDir) {
			return fmt.Errorf("非法路径: %s", f.Name)
		}
		os.MkdirAll(filepath.Dir(outPath), 0755)

		rc, err := f.Open()
		if err != nil {
			return fmt.Errorf("打开压缩文件 %s 失败: %w", f.Name, err)
		}

		outFile, err := os.Create(outPath)
		if err != nil {
			rc.Close()
			return fmt.Errorf("创建文件 %s 失败: %w", f.Name, err)
		}

		_, err = io.Copy(outFile, rc)
		outFile.Sync() // 确保数据落盘
		outFile.Close()
		rc.Close()
		if err != nil {
			return fmt.Errorf("写入文件 %s 失败: %w", f.Name, err)
		}
	}
	return nil
}

// isSubPath 检查 target 是否在 base 目录下，防止 zip slip 路径穿越
func isSubPath(target, base string) bool {
	return isSubPathCheck(target, base)
}

// absPath 获取绝对路径，供平台特定 isSubPathCheck 使用
func absPath(p string) (string, error) {
	return filepath.Abs(p)
}

func copyUpdateFiles(srcDir, dstDir, exeName string) error {
	fallbackExe := fallbackExeName()

	return filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}

		relPath, _ := filepath.Rel(srcDir, path)
		if relPath == "." || relPath == "" {
			return nil
		}

		dstPath := filepath.Join(dstDir, relPath)
		baseName := filepath.Base(relPath)
		isExe := baseName == exeName || baseName == fallbackExe

		if isExe {
			finalDst := filepath.Join(dstDir, exeName)
			if err := renameAndCopyExe(path, finalDst); err != nil {
				return err
			}
			return nil
		}
		os.MkdirAll(filepath.Dir(dstPath), 0755)
		return copyFileAtomic(path, dstPath)
	})
}

// renameAndCopyExe 替换正在运行的 exe
// Windows 允许重命名运行中的文件，但不允许 Create 同名文件
// 策略：新 exe 写到 .new → 旧 exe 重命名为 .old → .new 重命名为目标名
// 任何步骤失败都尝试回滚
func renameAndCopyExe(src, dst string) error {
	oldPath := dst + ".old"
	newPath := dst + ".new"

	// 清理上次遗留
	os.Remove(oldPath)
	os.Remove(newPath)

	// 1. 将新 exe 写到 .new 临时文件
	if err := copyFile(src, newPath); err != nil {
		return fmt.Errorf("写入临时文件失败: %w", err)
	}

	// 2. 重命名旧 exe 为 .old（Windows 允许重命名运行中的文件）
	if _, err := os.Stat(dst); err == nil {
		if err := os.Rename(dst, oldPath); err != nil {
			// 重命名失败，尝试用 MoveFileExW 直接替换
			log.Printf("[updater] 重命名旧文件失败，尝试 MoveFileEx: %v", err)
			if moveErr := moveFileReplace(newPath, dst); moveErr != nil {
				os.Remove(newPath)
				return fmt.Errorf("无法替换旧文件: %w", moveErr)
			}
			return nil
		}
	}

	// 3. 重命名 .new 为目标文件名
	if err := os.Rename(newPath, dst); err != nil {
		log.Printf("[updater] 重命名新文件失败，尝试 MoveFileEx: %v", err)
		if moveErr := moveFileReplace(newPath, dst); moveErr != nil {
			// 步骤3失败，回滚：将 .old 恢复为原始文件名
			log.Printf("[updater] 替换失败，尝试回滚 .old")
			if rollbackErr := os.Rename(oldPath, dst); rollbackErr != nil {
				moveFileReplace(oldPath, dst)
			}
			os.Remove(newPath)
			return fmt.Errorf("无法替换文件: %w", moveErr)
		}
	}

	log.Printf("[updater] 文件更新完成: %s", dst)
	return nil
}

// copyFileAtomic 原子性复制文件：先写临时文件，再 rename 替换
func copyFileAtomic(src, dst string) error {
	tmpPath := dst + ".tmp"

	if err := copyFile(src, tmpPath); err != nil {
		os.Remove(tmpPath)
		return err
	}

	if err := os.Rename(tmpPath, dst); err != nil {
		// Rename 失败（可能跨卷），回退为直接覆盖
		os.Remove(tmpPath)
		return copyFile(src, dst)
	}

	return nil
}

func copyFile(src, dst string) error {
	srcInfo, err := os.Stat(src)
	if err != nil {
		return err
	}

	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}

	// 确保数据落盘
	out.Sync()

	return os.Chmod(dst, srcInfo.Mode())
}
