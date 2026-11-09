//go:build darwin

package main

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// ListLocalDisks 获取本机磁盘分区列表
// macOS 只返回 Macintosh HD（用户主磁盘），Drive 为用户可写的主目录
func (s *IscsiServiceWrapper) ListLocalDisks() ([]LocalDiskInfo, error) {
	n, err := syscall.Getfsstat(nil, 0)
	if err != nil {
		return nil, err
	}
	buf := make([]syscall.Statfs_t, n)
	if _, err := syscall.Getfsstat(buf, 0); err != nil {
		return nil, err
	}

	var result []LocalDiskInfo
	seen := map[string]bool{}

	for _, fs := range buf {
		fstyp := trimNullInt8(fs.Fstypename[:])
		mount := trimNullInt8(fs.Mntonname[:])

		if isVirtualFSType(fstyp) {
			continue
		}

		total := uint64(fs.Blocks) * uint64(fs.Bsize)
		free := uint64(fs.Bavail) * uint64(fs.Bsize)
		if total == 0 {
			continue
		}

		// Macintosh HD：根挂载点 /，Drive 指向 Application Support（规范存储路径）
		if mount == "/" {
			dir := getDarwinAppSupportDir()
			if seen[dir] {
				continue
			}
			seen[dir] = true
			result = append(result, LocalDiskInfo{
				Drive:      dir,
				Label:      "Macintosh HD",
				TotalBytes: total,
				FreeBytes:  free,
			})
			continue
		}

		// 外置磁盘：只取 /Volumes/ 下非 DMG 挂载的物理卷
		if !strings.HasPrefix(mount, "/Volumes/") {
			continue
		}
		// 跳过 DMG/应用挂载（通常容量小且是只读安装包）
		if fs.Blocks*uint64(fs.Bsize) < 1*1024*1024*1024 {
			continue
		}
		if seen[mount] {
			continue
		}
		seen[mount] = true
		result = append(result, LocalDiskInfo{
			Drive:      mount,
			Label:      filepath.Base(mount),
			TotalBytes: total,
			FreeBytes:  free,
		})
	}
	return result, nil
}

func getDarwinAppSupportDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "/Users"
	}
	return filepath.Join(home, "Library", "Application Support", "com.moyunteng.v3ai")
}

func isVirtualFSType(fstyp string) bool {
	switch fstyp {
	case "devfs", "autofs", "tmpfs", "devpts", "procfs",
		"fusefs", "nfs", "smbfs", "afpfs", "cifs":
		return true
	}
	return false
}

func trimNullInt8(b []int8) string {
	for i, c := range b {
		if c == 0 {
			return string(bytesFromInt8(b[:i]))
		}
	}
	return string(bytesFromInt8(b))
}

func bytesFromInt8(s []int8) []byte {
	out := make([]byte, len(s))
	for i, v := range s {
		out[i] = byte(v)
	}
	return out
}

func isVirtualNic(name, mac string) bool {
	virtualPrefixes := []string{
		"awdl", "utun", "anpi", "bridge",
		"llw", "lo", "en2", "en3",
	}
	for _, p := range virtualPrefixes {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	if mac == "" || mac == "00:00:00:00:00:00" {
		return true
	}
	return false
}
