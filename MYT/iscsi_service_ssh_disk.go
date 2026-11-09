package main

import (
	"fmt"
	"log"
	"strings"
	"time"
)

// autoInitDiskSSH 自动初始化 iSCSI 磁盘（与 SDK 一致：sfdisk/fdisk GPT 分区 → mkfs.ext4 分区）
func (s *IscsiServiceWrapper) autoInitDiskSSH(deviceIP, device string) (string, error) {
	// Step 1: 检查分区是否已存在（兼容 /dev/sdX1 和 /dev/sdX-part1 命名）
	partition := s.findExistingPartitionSSH(deviceIP, device)
	if partition != "" {
		log.Printf("[iSCSI] 分区 %s 已存在，跳过分区", partition)
	} else {
		// Step 2: GPT 分区（优先 sfdisk，fallback fdisk，最后 parted）
		log.Printf("[iSCSI] 对 %s 执行 GPT 分区", device)

		sfdiskPath, _ := s.sshSudo(deviceIP, "which sfdisk 2>/dev/null")
		fdiskPath, _ := s.sshSudo(deviceIP, "which fdisk 2>/dev/null")
		partedPath, _ := s.sshSudo(deviceIP, "which parted 2>/dev/null")

		if strings.TrimSpace(sfdiskPath) != "" {
			// sfdisk: 写入脚本文件避免引号/转义问题
			script := `cat > /tmp/_myt_part.sfdisk << 'SFEOF'
label: gpt
, L
SFEOF
sfdisk -q %s < /tmp/_myt_part.sfdisk && rm -f /tmp/_myt_part.sfdisk`
			out, err := s.sshSudo(deviceIP, fmt.Sprintf(script, device))
			log.Printf("[iSCSI] sfdisk %s: out=%q err=%v", device, out, err)
			if err != nil {
				return "", fmt.Errorf("sfdisk 分区失败: %w (%s)", err, out)
			}
		} else if strings.TrimSpace(fdiskPath) != "" {
			// fdisk: 写入脚本文件避免引号/转义问题（与 SDK 测试通过的输入一致）
			script := `cat > /tmp/_myt_part.fdisk << 'FEOF'
g
n
1


w
FEOF
fdisk %s < /tmp/_myt_part.fdisk && rm -f /tmp/_myt_part.fdisk`
			out, err := s.sshSudo(deviceIP, fmt.Sprintf(script, device))
			log.Printf("[iSCSI] fdisk %s: out=%q err=%v", device, out, err)
			if err != nil {
				return "", fmt.Errorf("fdisk 分区失败: %w (%s)", err, out)
			}
		} else if strings.TrimSpace(partedPath) != "" {
			out, err := s.sshSudo(deviceIP, fmt.Sprintf("parted -s %s mklabel gpt mkpart primary 0%% 100%% 2>&1", device))
			log.Printf("[iSCSI] parted %s: out=%q err=%v", device, out, err)
			if err != nil {
				return "", fmt.Errorf("parted 分区失败: %w (%s)", err, out)
			}
		} else {
			return "", fmt.Errorf("自动分区失败: 需要 sfdisk、fdisk 或 parted，请安装其一")
		}

		// Step 3: 通知内核重读分区表 + 等待分区设备出现
		s.sshSudo(deviceIP, fmt.Sprintf("partprobe %s 2>/dev/null || true", device))
		time.Sleep(time.Second)

		// 诊断：fdisk -l 查看分区表是否写入成功
		fdiskList, _ := s.sshSudo(deviceIP, fmt.Sprintf("fdisk -l %s 2>&1 | tail -5", device))
		log.Printf("[iSCSI] fdisk -l %s: %s", device, fdiskList)

		// 再等一秒让 udev 创建设备节点
		time.Sleep(time.Second)

		lsOut, _ := s.sshSudo(deviceIP, fmt.Sprintf("ls %s* 2>/dev/null || true", device))
		log.Printf("[iSCSI] 分区后 %s*: %s", device, lsOut)

		partition = s.findExistingPartitionSSH(deviceIP, device)
		if partition == "" {
			// 最后尝试: 直接用 lsblk 查看分区
			lsblkOut, _ := s.sshSudo(deviceIP, fmt.Sprintf("lsblk %s 2>/dev/null || true", device))
			return "", fmt.Errorf("分区后未找到分区设备 (ls %s*: %s, lsblk: %s)", device, lsOut, lsblkOut)
		}
		log.Printf("[iSCSI] 分区完成: %s", partition)
	}

	// Step 4: mkfs.ext4 格式化分区（与 SDK 一致：lazy init 跳过全盘 inode 写入，大磁盘秒级完成）
	out, err := s.sshSudoLongChecked(deviceIP,
		fmt.Sprintf("mkfs.ext4 -F -E lazy_itable_init=1,lazy_journal_init=1 %s", partition),
		600*time.Second)
	log.Printf("[iSCSI] mkfs.ext4 %s: out=%q err=%v", partition, out, err)
	if err != nil {
		return "", fmt.Errorf("格式化失败: %w (%s)", err, out)
	}

	// 验证文件系统（兼容不同 blkid 版本的输出格式）
	probe, _ := s.sshSudo(deviceIP, fmt.Sprintf("blkid %s 2>/dev/null || true", partition))
	if !strings.Contains(probe, `TYPE="ext4"`) && !strings.Contains(probe, "TYPE=ext4") {
		return "", fmt.Errorf("格式化后未检测到 ext4 文件系统: %s", probe)
	}

	return partition, nil
}

// findExistingPartitionSSH 查找设备的第一个分区（兼容不同命名：sdX1, sdXp1, sdX-part1）
func (s *IscsiServiceWrapper) findExistingPartitionSSH(deviceIP, device string) string {
	// 常见分区命名模式
	candidates := []string{
		device + "1",      // /dev/sddb → /dev/sddb1
		device + "p1",     // /dev/nvme0n1 → /dev/nvme0n1p1
		device + "-part1", // 部分 virtio 设备
	}
	for _, c := range candidates {
		out, _ := s.sshSudo(deviceIP, fmt.Sprintf("test -b %s && echo exists", c))
		if strings.TrimSpace(out) == "exists" {
			return c
		}
	}
	// fallback: ls 设备前缀找第一个子设备
	out, _ := s.sshSudo(deviceIP, fmt.Sprintf("ls %s* 2>/dev/null | head -2", device))
	for _, line := range strings.Fields(out) {
		if line != device && strings.HasPrefix(line, device) {
			return line
		}
	}
	return ""
}

// findFirstPartitionSSH 查找设备的第一个分区，无分区则返回整盘设备
func (s *IscsiServiceWrapper) findFirstPartitionSSH(deviceIP, device string) (string, error) {
	// 检查是否有分区设备
	out, _ := s.sshSudo(deviceIP, fmt.Sprintf("ls %s* 2>/dev/null", device))
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line != "" && line != device && strings.HasPrefix(line, device) {
			return line, nil
		}
	}
	// 无分区，直接用整盘设备
	return device, nil
}

// mountIscsiFilesystemSSH 挂载 iSCSI 文件系统到 /mnt/iscsi
func (s *IscsiServiceWrapper) mountIscsiFilesystemSSH(deviceIP, partition string) error {
	s.sshSudo(deviceIP, "mkdir -p /mnt/iscsi")

	// 确认分区设备存在
	out, _ := s.sshSudo(deviceIP, fmt.Sprintf("ls %s 2>&1", partition))
	if strings.Contains(out, "No such file") {
		return fmt.Errorf("分区设备 %s 不存在，请先分区", partition)
	}

	// BusyBox mount 不支持 -t，直接 mount 让内核自动检测文件系统
	mountCmd := fmt.Sprintf("mount %s /mnt/iscsi", partition)

	out, err := s.sshSudoChecked(deviceIP, mountCmd)
	if err != nil {
		probe, _ := s.sshSudo(deviceIP, fmt.Sprintf("blkid %s 2>/dev/null || file -sL %s 2>/dev/null || true", partition, partition))
		dmesg, _ := s.sshSudo(deviceIP, "dmesg | tail -20 2>/dev/null || true")
		return fmt.Errorf("mount 失败: %w\n[probe] %s\n[dmesg] %s", err, probe, dmesg)
	}
	if strings.Contains(strings.ToLower(out), "error") || strings.Contains(strings.ToLower(out), "failed") {
		return fmt.Errorf("mount 失败: %s", out)
	}
	return nil
}

// umountIscsiFilesystemSSH 卸载 /mnt/iscsi
func (s *IscsiServiceWrapper) umountIscsiFilesystemSSH(deviceIP string) error {
	out, err := s.sshSudo(deviceIP, "umount /mnt/iscsi")
	if err != nil {
		return fmt.Errorf("umount 失败: %w", err)
	}
	if strings.Contains(strings.ToLower(out), "error") || strings.Contains(strings.ToLower(out), "busy") {
		return fmt.Errorf("umount 失败: %s", out)
	}
	return nil
}
