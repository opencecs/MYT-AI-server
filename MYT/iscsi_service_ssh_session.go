package main

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/moyunteng/myt/internal/sdkapi"
)

// ─── 发现 / 登录 / 登出 / 列出 ───

func (s *IscsiServiceWrapper) discoverIscsiSSH(deviceIP, portal string) ([]sdkapi.IscsiTargetInfo, error) {
	out, _ := s.sshIscsiadm(deviceIP, fmt.Sprintf("-m discovery -t sendtargets -p %s", portal))

	if strings.Contains(out, "Connection refused") || strings.Contains(out, "Could not perform") {
		return nil, fmt.Errorf("发现失败: 无法连接到 %s", portal)
	}

	var targets []sdkapi.IscsiTargetInfo
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "iSCSI ERROR") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) >= 2 {
			portalAddr := strings.TrimSuffix(parts[0], ",-1")
			targets = append(targets, sdkapi.IscsiTargetInfo{
				IQN:    parts[1],
				Portal: portalAddr,
			})
		}
	}

	if len(targets) == 0 {
		return nil, fmt.Errorf("未发现 iSCSI Target (portal: %s)", portal)
	}
	return targets, nil
}

func (s *IscsiServiceWrapper) loginIscsiSSH(deviceIP, portal, iqn string) error {
	// Step 0: 禁用 discovery 自动登录（防止 discovery 发现新 target 时自动 login）
	s.sshIscsiadm(deviceIP, fmt.Sprintf("-m discoverydb -t sendtargets -p %s --op update -n discovery.sendtargets.iscsi.use_discoveryd -v No", portal))
	// 禁用已有 node 的自动登录
	s.sshIscsiadm(deviceIP, fmt.Sprintf("-m node -p %s --op update -n node.startup -v manual", portal))

	// Step 1: Discovery（仅发现，不自动 login）
	discOut, _ := s.sshIscsiadm(deviceIP, fmt.Sprintf("-m discovery -t sendtargets -p %s", portal))
	log.Printf("[iSCSI] discovery output: %q", discOut)
	if strings.Contains(discOut, "Connection refused") || strings.Contains(discOut, "Could not") {
		return fmt.Errorf("发现失败（Target %s 可能未运行或不可达）: %s", portal, discOut)
	}

	// 禁用所有发现 target 的自动登录，只保留指定的 IQN
	s.sshIscsiadm(deviceIP, fmt.Sprintf("-m node -p %s --op update -n node.startup -v manual", portal))
	s.sshIscsiadm(deviceIP, fmt.Sprintf("-m node -T %s -p %s --op update -n node.startup -v automatic", iqn, portal))

	// Step 2: 清理残留会话（如有）
	sessOut, _ := s.sshIscsiadm(deviceIP, "-m session")
	if strings.Contains(sessOut, iqn) {
		log.Printf("[iSCSI] 检测到残留会话，先 logout")
		s.sshIscsiadm(deviceIP, fmt.Sprintf("-m node -T %s -p %s --logout", iqn, portal))
		time.Sleep(time.Second)
	}

	// Step 3: Login
	out, _ := s.sshIscsiadm(deviceIP, fmt.Sprintf("-m node -T %s -p %s -l", iqn, portal))
	log.Printf("[iSCSI] login output: %q", out)
	if strings.Contains(out, "Could not log into") || strings.Contains(out, "failed") {
		return fmt.Errorf("登录失败: %s", out)
	}

	// Step 4: 验证会话
	time.Sleep(2 * time.Second)
	sessOut, _ = s.sshIscsiadm(deviceIP, "-m session")
	log.Printf("[iSCSI] login 后会话检查: %q", sessOut)
	if strings.Contains(sessOut, "No active sessions") {
		return fmt.Errorf("登录后无活跃会话")
	}
	// 检查是否有非目标 IQN 的意外会话
	for _, line := range strings.Split(sessOut, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || !strings.HasPrefix(line, "tcp:") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) >= 4 && parts[3] != iqn {
			log.Printf("[iSCSI] 警告: 发现非目标会话 %s（预期只有 %s）", parts[3], iqn)
		}
	}

	return nil
}

func (s *IscsiServiceWrapper) logoutIscsiSSH(deviceIP, portal, iqn string) error {
	out, _ := s.sshIscsiadm(deviceIP, fmt.Sprintf("-m node -T %s -p %s --logout", iqn, portal))
	if strings.Contains(out, "No sessions") || strings.Contains(out, "No matching sessions") {
		return nil
	}
	if strings.Contains(out, "Could not logout") {
		return fmt.Errorf("登出失败: %s", out)
	}
	return nil
}

func (s *IscsiServiceWrapper) listIscsiSSH(deviceIP string) ([]sdkapi.IscsiSessionInfo, error) {
	// 优先检查 /proc/mounts（更可靠，即使 iscsiadm session 断了磁盘仍挂着）
	mountOut, _ := s.sshSudo(deviceIP, "cat /proc/mounts 2>/dev/null")
	var sessions []sdkapi.IscsiSessionInfo
	seen := map[string]bool{}
	for _, line := range strings.Split(mountOut, "\n") {
		// /dev/sddX1 /mnt/iscsi ext4 ...
		if !strings.HasPrefix(line, "/dev/sd") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 || fields[1] != "/mnt/iscsi" {
			continue
		}
		device := fields[0]
		if seen[device] {
			continue
		}
		seen[device] = true

		// 尝试从 iscsiadm session 获取 IQN/portal
		iqn, portal := s.getIscsiSessionInfoSSH(deviceIP, device)
		sessions = append(sessions, sdkapi.IscsiSessionInfo{
			IQN:    iqn,
			Portal: portal,
			Device: fmt.Sprintf("iscsi://%s", device),
		})
	}

	// 如果 /proc/mounts 没找到，fallback iscsiadm session
	if len(sessions) == 0 {
		out, _ := s.sshIscsiadm(deviceIP, "-m session")
		for _, line := range strings.Split(out, "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "iSCSI ERROR") {
				continue
			}
			parts := strings.Fields(line)
			if len(parts) >= 3 && strings.HasPrefix(parts[0], "tcp:") {
				portal := strings.TrimSuffix(parts[2], ",-1")
				sessions = append(sessions, sdkapi.IscsiSessionInfo{
					IQN:    parts[3],
					Portal: portal,
				})
			}
		}
	}

	return sessions, nil
}

// getIscsiSessionInfoSSH 从 sysfs 获取 iSCSI session 的 IQN 和 portal
func (s *IscsiServiceWrapper) getIscsiSessionInfoSSH(deviceIP, device string) (iqn, portal string) {
	baseName := strings.TrimPrefix(device, "/dev/")
	// 方式1: 通过 /sys/block/sdX/device 的链接找到 session
	sessOut, _ := s.sshSudo(deviceIP, fmt.Sprintf(
		"basename $(readlink -f /sys/block/%s/device) 2>/dev/null | grep -o 'session[0-9]*' || "+
			"ls /sys/block/%s/device/ 2>/dev/null | grep -o 'session[0-9]*' | head -1",
		baseName, baseName))
	sessID := strings.TrimSpace(sessOut)

	// 方式2: 遍历所有 session 匹含这个设备
	if sessID == "" {
		sessOut, _ = s.sshSudo(deviceIP, fmt.Sprintf(
			"for s in /sys/class/iscsi_session/session*; do "+
				"if ls $s/device/target*/[0-9]*:*/block/%s >/dev/null 2>&1; then "+
				"basename $s; break; fi; done", baseName))
		sessID = strings.TrimSpace(sessOut)
	}

	if sessID == "" {
		return device, ""
	}

	iqnOut, _ := s.sshSudo(deviceIP, fmt.Sprintf("cat /sys/class/iscsi_session/%s/targetname 2>/dev/null", sessID))
	iqn = strings.TrimSpace(iqnOut)
	if iqn == "" {
		iqn = device
	}

	portalOut, _ := s.sshSudo(deviceIP, fmt.Sprintf(
		"cat /sys/class/iscsi_session/%s/portal 2>/dev/null || "+
			"cat /sys/class/iscsi_session/%s/connection*/portal 2>/dev/null || true", sessID, sessID))
	portal = strings.TrimSpace(portalOut)
	return
}
