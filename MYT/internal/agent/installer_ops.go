package agent

import (
	"fmt"
	"log"
	"strings"

	"github.com/moyunteng/myt/internal/maintenance"
)

// ---------- Uninstall ----------

// Uninstall 卸载设备上的 Hermes Agent（6 步）
func Uninstall(deviceIP string, cb ProgressCallback) (*InstallResult, error) {
	p := makeProgress("uninstall", uninstallSteps)

	sshClient, err := maintenance.DialSSH(deviceIP)
	if err != nil {
		return nil, fmt.Errorf("SSH连接失败: %w", err)
	}
	defer sshClient.Close()

	// Step 1: 停止服务
	updateStep(&p, 0, StepRunning, "", cb)
	maintenance.RunSSHCmd(sshClient,
		"echo 'myt' | sudo -S rc-service hermes-dashboard stop 2>&1 || true")
	updateStep(&p, 0, StepSuccess, "", cb)

	// Step 2: 删除自启动
	updateStep(&p, 1, StepRunning, "", cb)
	maintenance.RunSSHCmd(sshClient,
		"echo 'myt' | sudo -S rc-update del hermes-dashboard default 2>&1 || true; echo 'myt' | sudo -S rm -f /etc/init.d/hermes-dashboard 2>&1 || true")
	updateStep(&p, 1, StepSuccess, "", cb)

	// Step 3: pip uninstall
	updateStep(&p, 2, StepRunning, "", cb)
	output, _ := maintenance.RunSSHCmd(sshClient,
		fmt.Sprintf("%s/bin/pip uninstall hermes-agent -y 2>&1 || true", venvDir))
	updateStep(&p, 2, StepSuccess, strings.TrimSpace(output), cb)

	// Step 4: 删除安装目录
	updateStep(&p, 3, StepRunning, "", cb)
	maintenance.RunSSHCmd(sshClient, fmt.Sprintf("rm -rf %s", installDir))
	maintenance.RunSSHCmd(sshClient, fmt.Sprintf("rm -rf %s", remotePkgDir))
	updateStep(&p, 3, StepSuccess, "", cb)

	// Step 5: 删除配置
	updateStep(&p, 4, StepRunning, "", cb)
	maintenance.RunSSHCmd(sshClient, fmt.Sprintf("rm -rf %s", hermesHome))
	updateStep(&p, 4, StepSuccess, "", cb)

	// Step 6: 清理命令链接
	updateStep(&p, 5, StepRunning, "", cb)
	maintenance.RunSSHCmd(sshClient, "rm -f /home/user/.local/bin/hermes /home/user/.local/bin/hermes-dashboard 2>&1 || true")
	updateStep(&p, 5, StepSuccess, "", cb)

	finishProgress(&p, cb)
	log.Printf("[agent] 卸载成功: %s", deviceIP)
	return &InstallResult{Success: true, Message: "卸载成功"}, nil
}

// ---------- 状态查询 ----------

// GetAgentStatus 通过 pip show 查询版本，Dashboard 端口探测运行状态
func GetAgentStatus(deviceIP string) (*AgentStatus, error) {
	output, err := maintenance.SSHExecSimple(deviceIP,
		fmt.Sprintf("%s/bin/pip show hermes_agent 2>/dev/null || echo 'NOT_INSTALLED'", venvDir))
	if err != nil {
		return &AgentStatus{Installed: false, Status: "unknown"}, nil
	}

	if strings.Contains(output, "NOT_INSTALLED") {
		return &AgentStatus{Installed: false, Version: "", Status: "unknown"}, nil
	}

	version := ""
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, "Version:") {
			version = strings.TrimSpace(strings.TrimPrefix(line, "Version:"))
			break
		}
	}
	if version == "" {
		return &AgentStatus{Installed: false, Status: "unknown"}, nil
	}

	status := "stopped"
	statusOut, err := maintenance.SSHExecSimple(deviceIP,
		fmt.Sprintf("curl -s -m 5 http://127.0.0.1:%s/api/status 2>&1 || echo 'DOWN'", dashboardPort))
	if err == nil && !strings.Contains(statusOut, "DOWN") && statusOut != "" {
		status = "running"
	}

	return &AgentStatus{Installed: true, Version: version, Status: status}, nil
}
