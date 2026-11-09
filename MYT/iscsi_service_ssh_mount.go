package main

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/moyunteng/myt/internal/sdkapi"
)

// ─── Mount 一体化（SSH 回退） ───

func (s *IscsiServiceWrapper) mountIscsiSSH(deviceIP, portal, iqn string, autoInit bool, localIP string) (*sdkapi.IscsiMountResult, error) {
	// Step 1: iSCSI Login（含 discovery + 清理残留会话）
	if err := s.loginIscsiSSH(deviceIP, portal, iqn); err != nil {
		return nil, fmt.Errorf("iSCSI 登录失败: %w", err)
	}

	// Step 2: 通过 /sys/class/iscsi_session 查找块设备（与 SDK 一致）
	device, err := s.findIscsiBlockDeviceSSH(deviceIP, iqn)
	if err != nil {
		return nil, fmt.Errorf("查找块设备失败: %w", err)
	}
	log.Printf("[iSCSI] 块设备: %s", device)

	// Step 3: autoInit — 自动分区 + 格式化（与 SDK 一致：sfdisk/fdisk GPT → mkfs.ext4 分区）
	var partition string
	if autoInit {
		partition, err = s.autoInitDiskSSH(deviceIP, device)
		if err != nil {
			return nil, fmt.Errorf("自动分区格式化失败: %w", err)
		}
	} else {
		partition, err = s.findFirstPartitionSSH(deviceIP, device)
		if err != nil {
			return nil, fmt.Errorf("查找分区失败: %w", err)
		}
	}

	// Step 4: 挂载文件系统
	if err := s.mountIscsiFilesystemSSH(deviceIP, partition); err != nil {
		return nil, fmt.Errorf("挂载文件系统失败: %w", err)
	}

	// Step 5: 写存储模式标记
	s.sshSudo(deviceIP, "mkdir -p /etc/myt && echo iscsi > /etc/myt/storage_mode")
	verifyMode, _ := s.sshSudo(deviceIP, "cat /etc/myt/storage_mode 2>/dev/null")
	log.Printf("[iSCSI] 挂载完成，存储模式: %s", strings.TrimSpace(verifyMode))

	// Step 6: 切换 Docker data-root 到 iSCSI 磁盘（旧固件设备需要手动切换）
	dockerSwitchCmd := "mkdir -p /mnt/iscsi/docker && " +
		"if [ -f /etc/docker/daemon.json ]; then " +
		"cp /etc/docker/daemon.json /etc/docker/daemon.json.bak; fi && " +
		"echo '{\"data-root\": \"/mnt/iscsi/docker\"}' > /etc/docker/daemon.json && " +
		"systemctl restart docker 2>/dev/null || true"
	dockerOut, dockerErr := s.sshSudoLong(deviceIP, dockerSwitchCmd, 60*time.Second)
	if dockerErr != nil {
		log.Printf("[iSCSI] Docker data-root 切换警告: %v (%s)", dockerErr, dockerOut)
	} else {
		log.Printf("[iSCSI] Docker data-root 已切换到 /mnt/iscsi/docker")
	}

	return &sdkapi.IscsiMountResult{
		Device:             device,
		MountPoint:         "/mnt/iscsi",
		StorageMode:        "iscsi",
		MigratedContainers: 0,
		Success:            true,
	}, nil
}

// ─── Umount 一体化（SSH 回退） ───

func (s *IscsiServiceWrapper) umountIscsiSSH(deviceIP, portal, iqn string) (*sdkapi.IscsiUmountResult, error) {
	var errMsgs []string

	// Step 1: 迁移容器回本地
	containerCount, migrateErr := s.migrateContainersSSH(deviceIP, "local")
	if migrateErr != nil {
		errMsgs = append(errMsgs, fmt.Sprintf("容器迁移失败: %v", migrateErr))
	}

	// Step 2: rsync 数据回本地（如果 iSCSI 有数据）
	if _, err := s.rsyncBackToLocalSSH(deviceIP); err != nil {
		errMsgs = append(errMsgs, fmt.Sprintf("rsync回传失败: %v", err))
	}

	// Step 3: 卸载文件系统
	if err := s.umountIscsiFilesystemSSH(deviceIP); err != nil {
		errMsgs = append(errMsgs, fmt.Sprintf("卸载失败: %v", err))
	}

	// Step 4: iSCSI Logout
	if err := s.logoutIscsiSSH(deviceIP, portal, iqn); err != nil {
		errMsgs = append(errMsgs, fmt.Sprintf("登出失败: %v", err))
	}

	// 部分步骤失败时返回 error（而非 Success=false 的 result）：
	// 错误经 err 通道传到任务队列，用户能看到具体失败原因，而不是一个无信息的布尔失败
	if len(errMsgs) > 0 {
		return nil, fmt.Errorf("%s", strings.Join(errMsgs, "; "))
	}
	return &sdkapi.IscsiUmountResult{
		StorageMode:        "local",
		MigratedContainers: containerCount,
		Success:            true,
	}, nil
}

// ─── Mount/Umount 子步骤 ───

// findIscsiBlockDeviceSSH 通过 /sys/class/iscsi_session 匹配 IQN 查找块设备（与 SDK 一致）
func (s *IscsiServiceWrapper) findIscsiBlockDeviceSSH(deviceIP, iqn string) (string, error) {
	// 等待 iscsi session 出现（最多 15 秒）
	for i := 0; i < 15; i++ {
		time.Sleep(time.Second)

		// 遍历 /sys/class/iscsi_session/*/targetname 匹配 IQN
		sessOut, _ := s.sshSudo(deviceIP, "ls /sys/class/iscsi_session/ 2>/dev/null || true")
		sessIDs := strings.Fields(sessOut)
		log.Printf("[iSCSI] findBlockDevice 第%d秒: sessions=%v, 目标IQN=%s", i+1, sessIDs, iqn)

		for _, sessID := range sessIDs {
			targetName, _ := s.sshSudo(deviceIP, fmt.Sprintf("cat /sys/class/iscsi_session/%s/targetname 2>/dev/null", sessID))
			trimmedTarget := strings.TrimSpace(targetName)
			log.Printf("[iSCSI] findBlockDevice: session=%s targetname=%q", sessID, trimmedTarget)

			if trimmedTarget != iqn {
				continue
			}

			// 找到匹配 session，遍历 device/targetX:Y:Z/LUN:block/sdX
			devicePath := fmt.Sprintf("/sys/class/iscsi_session/%s/device", sessID)
			targetOut, _ := s.sshSudo(deviceIP, fmt.Sprintf("ls %s 2>/dev/null", devicePath))
			log.Printf("[iSCSI] findBlockDevice: session=%s device/ 内容=%q", sessID, targetOut)
			for _, te := range strings.Fields(targetOut) {
				if !strings.HasPrefix(te, "target") {
					continue
				}
				lunOut, _ := s.sshSudo(deviceIP, fmt.Sprintf("ls %s/%s 2>/dev/null", devicePath, te))
				log.Printf("[iSCSI] findBlockDevice: session=%s %s/ 内容=%q", sessID, te, lunOut)
				for _, le := range strings.Fields(lunOut) {
					blockOut, _ := s.sshSudo(deviceIP, fmt.Sprintf("ls %s/%s/%s/block 2>/dev/null", devicePath, te, le))
					log.Printf("[iSCSI] findBlockDevice: session=%s %s/%s/block 内容=%q", sessID, te, le, blockOut)
					for _, be := range strings.Fields(blockOut) {
						if strings.HasPrefix(be, "sd") {
							dev := "/dev/" + be
							log.Printf("[iSCSI] 通过 iscsi_session 找到块设备: %s (session=%s)", dev, sessID)
							return dev, nil
						}
					}
				}
			}
		}

		// Fallback: IQN not matched this iteration, try any session with block device
		for _, sessID := range sessIDs {
			devicePath := fmt.Sprintf("/sys/class/iscsi_session/%s/device", sessID)
			targetOut, _ := s.sshSudo(deviceIP, fmt.Sprintf("ls %s 2>/dev/null", devicePath))
			for _, te := range strings.Fields(targetOut) {
				if !strings.HasPrefix(te, "target") {
					continue
				}
				lunOut, _ := s.sshSudo(deviceIP, fmt.Sprintf("ls %s/%s 2>/dev/null", devicePath, te))
				for _, le := range strings.Fields(lunOut) {
					blockOut, _ := s.sshSudo(deviceIP, fmt.Sprintf("ls %s/%s/%s/block 2>/dev/null", devicePath, te, le))
					for _, be := range strings.Fields(blockOut) {
						if strings.HasPrefix(be, "sd") {
							log.Printf("[iSCSI] IQN %s not matched, using fallback block device /dev/%s (session=%s)", iqn, be, sessID)
							return "/dev/" + be, nil
						}
					}
				}
			}
		}
	}

	diag1, _ := s.sshIscsiadm(deviceIP, "-m session")
	diag2, _ := s.sshSudo(deviceIP, "ls /sys/class/iscsi_session/ 2>/dev/null || true")
	diag3, _ := s.sshSudo(deviceIP, "for s in /sys/class/iscsi_session/session*/targetname; do echo \"$s: $(cat $s 2>/dev/null)\"; done 2>/dev/null || true")
	return "", fmt.Errorf("15秒内未找到 iSCSI 块设备 (iqn=%s)\n[session] %s\n[iscsi_session] %s\n[targetnames] %s", iqn, diag1, diag2, diag3)
}
