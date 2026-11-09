package main

import (
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/moyunteng/myt/internal/sdkapi"
)

// rsyncIfEmptySSH 如果 iSCSI 磁盘为空则 rsync /mmc/ → /mnt/iscsi/
func (s *IscsiServiceWrapper) rsyncIfEmptySSH(deviceIP string) (bool, error) {
	out, _ := s.sshSudo(deviceIP, "ls /mnt/iscsi/data 2>/dev/null")
	if strings.TrimSpace(out) != "" {
		return false, nil
	}

	_, err := s.sshSudoLong(deviceIP, "rsync -a --delete /mmc/ /mnt/iscsi/", 300*time.Second)
	if err != nil {
		return false, fmt.Errorf("rsync 失败: %w", err)
	}
	return true, nil
}

// rsyncBackToLocalSSH rsync /mnt/iscsi/ → /mmc/（卸载前数据回传）
func (s *IscsiServiceWrapper) rsyncBackToLocalSSH(deviceIP string) (bool, error) {
	out, _ := s.sshSudo(deviceIP, "ls /mnt/iscsi/data 2>/dev/null")
	if strings.TrimSpace(out) == "" {
		return false, nil
	}

	_, err := s.sshSudoLong(deviceIP, "rsync -a /mnt/iscsi/ /mmc/", 300*time.Second)
	if err != nil {
		return false, fmt.Errorf("rsync 回传失败: %w", err)
	}
	return true, nil
}

// migrateContainersSSH 迁移容器（停止→切换模式→重启）
// mode: "iscsi" 切换到 iSCSI 存储, "local" 切换回本地存储
func (s *IscsiServiceWrapper) migrateContainersSSH(deviceIP, mode string) (int, error) {
	// 写入存储模式标记
	if _, err := s.sshSudo(deviceIP, fmt.Sprintf("mkdir -p /etc/myt && echo %s > /etc/myt/storage_mode", mode)); err != nil {
		log.Printf("[iSCSI] 写入存储模式标记失败: %v", err)
	}

	// 通过 SDK API 获取容器列表
	client := s.sdkApiSvc.newClient(deviceIP)
	containers, err := client.ListAndroids()
	if err != nil {
		return 0, fmt.Errorf("获取容器列表失败: %w", err)
	}

	count := 0
	var runningNames []string

	// 停止运行中的容器
	for _, c := range containers {
		if c.Status == "running" {
			runningNames = append(runningNames, c.Name)
			if stopErr := client.StopAndroid(c.Name); stopErr != nil {
				log.Printf("[iSCSI] 停止容器 %s 失败: %v", c.Name, stopErr)
			}
			count++
		}
	}

	// 重新启动容器（存储模式切换后，设备容器管理系统会读取标记自动使用新路径）
	time.Sleep(2 * time.Second)
	for _, name := range runningNames {
		if startErr := client.StartAndroid(name); startErr != nil {
			log.Printf("[iSCSI] 启动容器 %s 失败: %v", name, startErr)
		}
	}

	return count, nil
}

// getIscsiStatusSSH 通过 SSH 查询 iSCSI 存储状态（旧固件 fallback）
func (s *IscsiServiceWrapper) getIscsiStatusSSH(deviceIP string) (*sdkapi.IscsiStorageStatus, error) {
	status := &sdkapi.IscsiStorageStatus{}

	// 存储模式
	modeOut, _ := s.sshSudo(deviceIP, "cat /etc/myt/storage_mode 2>/dev/null")
	status.StorageMode = strings.TrimSpace(modeOut)
	if status.StorageMode != "iscsi" && status.StorageMode != "local" {
		status.StorageMode = "local"
	}
	status.MountPoint = "/mnt/iscsi"

	// 检查挂载
	mountOut, _ := s.sshSudo(deviceIP, "cat /proc/mounts 2>/dev/null")
	for _, line := range strings.Split(mountOut, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 3 && fields[1] == "/mnt/iscsi" {
			status.IsMounted = true
			status.Device = fields[0]
			break
		}
	}

	// 磁盘空间
	if status.IsMounted {
		dfOut, _ := s.sshSudo(deviceIP, "df -m /mnt/iscsi 2>/dev/null | tail -1")
		fields := strings.Fields(dfOut)
		if len(fields) >= 4 {
			status.TotalMB, _ = strconv.ParseInt(fields[1], 10, 64)
			status.UsedMB, _ = strconv.ParseInt(fields[2], 10, 64)
			status.AvailMB, _ = strconv.ParseInt(fields[3], 10, 64)
		}
	}

	return status, nil
}

// resizeIscsiDeviceSSH 设备端扩容：rescan LUN + growpart + resize2fs
func (s *IscsiServiceWrapper) resizeIscsiDeviceSSH(deviceIP string) error {
	// Step 1: 找到 iSCSI 块设备
	mountOut, _ := s.sshSudo(deviceIP, "cat /proc/mounts 2>/dev/null")
	var blockDev string
	for _, line := range strings.Split(mountOut, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[1] == "/mnt/iscsi" {
			// /dev/sdda1 → sdda
			partition := fields[0]
			baseName := strings.TrimPrefix(partition, "/dev/")
			// 去掉分区号后缀
			blockDev = baseName
			for _, suffix := range []string{"1", "p1", "-part1"} {
				if strings.HasSuffix(blockDev, suffix) {
					blockDev = blockDev[:len(blockDev)-len(suffix)]
					break
				}
			}
			break
		}
	}
	if blockDev == "" {
		return fmt.Errorf("未找到 iSCSI 挂载的块设备")
	}

	// Step 2: rescan — 让 initiator 识别新 LUN 大小
	out, err := s.sshSudo(deviceIP, fmt.Sprintf("echo 1 > /sys/block/%s/device/rescan 2>&1", blockDev))
	log.Printf("[iSCSI] rescan %s: out=%q err=%v", blockDev, out, err)

	// Step 3: growpart — 扩分区
	out, err = s.sshSudoChecked(deviceIP, fmt.Sprintf("growpart /dev/%s 1 2>&1 || true", blockDev))
	log.Printf("[iSCSI] growpart /dev/%s 1: out=%q err=%v", blockDev, out, err)

	// Step 4: resize2fs — 扩文件系统
	partition := s.findExistingPartitionSSH(deviceIP, "/dev/"+blockDev)
	if partition == "" {
		return fmt.Errorf("扩分区后未找到分区设备")
	}
	out, err = s.sshSudoLongChecked(deviceIP, fmt.Sprintf("resize2fs %s 2>&1", partition), 120*time.Second)
	log.Printf("[iSCSI] resize2fs %s: out=%q err=%v", partition, out, err)
	if err != nil {
		return fmt.Errorf("resize2fs 失败: %w (%s)", err, out)
	}

	return nil
}
