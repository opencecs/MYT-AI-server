package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/moyunteng/myt/internal/maintenance"
)

// sshIscsiadm 通过 SSH 在设备上执行 iscsiadm 命令（自动加 sudo）
// 注意：iscsiadm 非零退出码不一定代表失败（如已登录时返回15），因此始终返回输出，由调用方判断
func (s *IscsiServiceWrapper) sshIscsiadm(deviceIP, args string) (string, error) {
	pwd := s.devicePassword(deviceIP)
	cmd := fmt.Sprintf("echo %s | sudo -S sh -c %s 2>&1", shellEscape(pwd), shellArg(fmt.Sprintf("LD_LIBRARY_PATH=/mmc/resource/iscsi/lib /mmc/resource/iscsi/iscsiadm %s", args)))
	out, _ := maintenance.SSHExecSimple(deviceIP, cmd)
	return filterSudoOutput(out), nil
}

// sshSudo 通过 SSH 执行 sudo 命令（30秒超时）
func (s *IscsiServiceWrapper) sshSudo(deviceIP, cmd string) (string, error) {
	pwd := s.devicePassword(deviceIP)
	fullCmd := fmt.Sprintf("echo %s | sudo -S sh -c %s 2>&1", shellEscape(pwd), shellArg(cmd))
	out, sshErr := maintenance.SSHExecSimple(deviceIP, fullCmd)
	filtered := filterSudoOutput(out)
	if sshErr != nil && filtered == "" {
		return filtered, sshErr
	}
	return filtered, nil
}

// sshSudoLong 通过 SSH 执行 sudo 命令（自定义超时，供 rsync/格式化等长操作使用）
func (s *IscsiServiceWrapper) sshSudoLong(deviceIP, cmd string, timeout time.Duration) (string, error) {
	pwd := s.devicePassword(deviceIP)
	fullCmd := fmt.Sprintf("echo %s | sudo -S sh -c %s 2>&1", shellEscape(pwd), shellArg(cmd))
	out, sshErr := maintenance.SSHExecWithTimeout(deviceIP, fullCmd, timeout)
	filtered := filterSudoOutput(out)
	if sshErr != nil && filtered == "" {
		return filtered, sshErr
	}
	return filtered, nil
}

// sshSudoLongChecked 执行必须依赖退出码判断成败的命令。
// SSHExecWithTimeout 会在命令输出非空时被上层兼容逻辑吞掉错误，这里显式打印并解析退出码。
func (s *IscsiServiceWrapper) sshSudoLongChecked(deviceIP, cmd string, timeout time.Duration) (string, error) {
	const marker = "__MYT_EXIT__:"
	pwd := s.devicePassword(deviceIP)
	safeCmd := cmd + "; code=$?; echo " + marker + "$code; exit $code"
	fullCmd := fmt.Sprintf("echo %s | sudo -S sh -c %s 2>&1", shellEscape(pwd), shellArg(safeCmd))
	out, sshErr := maintenance.SSHExecWithTimeout(deviceIP, fullCmd, timeout)
	filtered := filterSudoOutput(out)

	var cleaned []string
	exitCode := -1
	for _, line := range strings.Split(filtered, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, marker) {
			if code, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, marker))); err == nil {
				exitCode = code
			}
			continue
		}
		if line != "" {
			cleaned = append(cleaned, line)
		}
	}

	result := strings.Join(cleaned, "\n")
	if exitCode != 0 {
		if exitCode < 0 && sshErr != nil {
			return result, sshErr
		}
		return result, fmt.Errorf("exit %d: %s", exitCode, result)
	}
	return result, nil
}

func (s *IscsiServiceWrapper) sshSudoChecked(deviceIP, cmd string) (string, error) {
	return s.sshSudoLongChecked(deviceIP, cmd, 30*time.Second)
}

// devicePassword 获取设备 sudo 密码
func (s *IscsiServiceWrapper) devicePassword(deviceIP string) string {
	pwd := s.sdkApiSvc.GetDevicePassword(deviceIP)
	if pwd == "" {
		pwd = "myt" // fallback 默认密码
	}
	return pwd
}

// shellEscape 对 shell 参数做单引号包裹，内部单引号替换为 ”'
func shellEscape(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

// shellArg 将 shell 命令参数用单引号包裹
func shellArg(cmd string) string {
	return shellEscape(cmd)
}

// filterSudoOutput 过滤 sudo 密码提示行
func filterSudoOutput(out string) string {
	var lines []string
	for _, line := range strings.Split(out, "\n") {
		l := strings.TrimSpace(line)
		if l != "" && !strings.Contains(strings.ToLower(l), "[sudo]") {
			lines = append(lines, l)
		}
	}
	return strings.Join(lines, "\n")
}
