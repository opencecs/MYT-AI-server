package maintenance

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// dialServerSSH 连接「用户自己的公网服务器」：账号密码由表单项提供，
// 与设备端固定凭据（dialSSH）区分开；同时支持 password / keyboard-interactive 两种认证。
func dialServerSSH(host string, port int, user, password string) (*ssh.Client, error) {
	config := &ssh.ClientConfig{
		User: user,
		Auth: []ssh.AuthMethod{
			ssh.Password(password),
			ssh.KeyboardInteractive(func(user, instruction string, questions []string, echos []bool) ([]string, error) {
				answers := make([]string, len(questions))
				for i := range answers {
					answers[i] = password
				}
				return answers, nil
			}),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         15 * time.Second,
	}
	addr := fmt.Sprintf("%s:%d", host, port)
	client, err := ssh.Dial("tcp", addr, config)
	if err != nil {
		return nil, fmt.Errorf("SSH连接 %s 失败: %w", addr, err)
	}
	return client, nil
}

// InstallTunnelServer 安装公网穿透服务端(frps)到用户指定的服务器
func InstallTunnelServer(serverIP string, srvUser string, srvPassword string, srvPort int, bindPort int, dashboardPort int, dashboardUser string, dashboardPassword string) map[string]interface{} {
	// 清理服务器地址中的协议前缀
	serverIP = strings.TrimPrefix(serverIP, "http://")
	serverIP = strings.TrimPrefix(serverIP, "https://")
	serverIP = strings.TrimRight(serverIP, "/")

	log.Printf("[公网穿透服务端] 开始安装: %s", serverIP)

	if serverIP == "" {
		return errResult("服务器地址不能为空")
	}
	if srvUser == "" {
		srvUser = "root"
	}
	if srvPassword == "" {
		return errResult("SSH密码不能为空")
	}
	if srvPort == 0 {
		srvPort = 22
	}
	if bindPort == 0 {
		bindPort = 7000
	}
	if dashboardPort == 0 {
		dashboardPort = 7500
	}
	if dashboardUser == "" {
		dashboardUser = "admin"
	}
	if dashboardPassword == "" {
		dashboardPassword = "admin"
	}

	// 1. 下载frps
	localDir := filepath.Join(os.TempDir(), "frps-install")
	os.RemoveAll(localDir)
	os.MkdirAll(localDir, 0755)
	defer os.RemoveAll(localDir)

	// 从API下载合并tar.gz包（包含frpc-arm64和frps-amd64）
	localTarPath := filepath.Join(localDir, "frps.tar.gz")
	log.Printf("[公网穿透服务端] 下载: %s", frpcDownloadURL)
	if err := downloadFile(frpcDownloadURL, localTarPath); err != nil {
		return errResult("下载frps失败: %v", err)
	}
	if err := extractFRPSFromTarGz(localTarPath, localDir); err != nil {
		return errResult("解压frps失败: %v", err)
	}

	// 2. 生成 frps.toml 配置
	configContent := generateFrpsConfig(bindPort, dashboardPort, dashboardUser, dashboardPassword)
	configPath := filepath.Join(localDir, "frps.toml")
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		return errResult("生成配置失败: %v", err)
	}

	// 3. 生成 systemd 服务文件
	deployDir := filepath.Join(localDir, "deploy")
	os.MkdirAll(deployDir, 0755)
	generateFrpsSystemdService(deployDir)

	// 4. SSH连接到用户服务器
	sshClient, err := dialServerSSH(serverIP, srvPort, srvUser, srvPassword)
	if err != nil {
		return errResult("%v", err)
	}
	defer sshClient.Close()

	// 停止已有frps服务
	runSSHCmd(sshClient, fmt.Sprintf("echo '%s' | sudo -S systemctl stop frps 2>/dev/null; echo '%s' | sudo -S killall frps 2>/dev/null; true", srvPassword, srvPassword))
	time.Sleep(2 * time.Second)
	// 确保端口已释放，强制杀掉占用端口的进程
	runSSHCmd(sshClient, fmt.Sprintf("echo '%s' | sudo -S fuser -k %d/tcp 2>/dev/null; echo '%s' | sudo -S fuser -k %d/tcp 2>/dev/null; true", srvPassword, bindPort, srvPassword, dashboardPort))
	time.Sleep(1 * time.Second)

	// 清理旧文件
	runSSHCmd(sshClient, fmt.Sprintf("echo '%s' | sudo -S rm -f /usr/local/bin/frps /etc/frps/frps.toml", srvPassword))
	runSSHCmd(sshClient, fmt.Sprintf("echo '%s' | sudo -S rm -rf /tmp/deploy-frps", srvPassword))

	// 5. SFTP上传所有文件
	sftpClient, err := sftp.NewClient(sshClient)
	if err != nil {
		return errResult("SFTP连接失败: %v", err)
	}
	defer sftpClient.Close()

	sftpClient.MkdirAll("/tmp/deploy-frps")
	if err := sftpUploadFile(sftpClient, filepath.Join(localDir, "frps"), "/tmp/deploy-frps/frps", 0755); err != nil {
		return errResult("上传frps失败: %v", err)
	}
	if err := sftpUploadFile(sftpClient, configPath, "/tmp/deploy-frps/frps.toml", 0644); err != nil {
		return errResult("上传配置文件失败: %v", err)
	}
	if err := sftpUploadDir(sftpClient, deployDir, "/tmp/deploy-frps"); err != nil {
		return errResult("上传服务文件失败: %v", err)
	}

	// 6. 注册并启动服务
	steps := []struct {
		desc string
		cmd  string
	}{
		{"移动二进制", fmt.Sprintf("echo '%s' | sudo -S cp /tmp/deploy-frps/frps /usr/local/bin/frps && echo '%s' | sudo -S chmod +x /usr/local/bin/frps", srvPassword, srvPassword)},
		{"创建配置目录", fmt.Sprintf("echo '%s' | sudo -S mkdir -p /etc/frps", srvPassword)},
		{"移动配置文件", fmt.Sprintf("echo '%s' | sudo -S cp /tmp/deploy-frps/frps.toml /etc/frps/frps.toml", srvPassword)},
		{"复制服务文件", fmt.Sprintf("echo '%s' | sudo -S sh -c 'cp /tmp/deploy-frps/frps.service /etc/systemd/system/frps.service && sed -i \"s/\\r$//\" /etc/systemd/system/frps.service'", srvPassword)},
		{"重载systemd", fmt.Sprintf("echo '%s' | sudo -S systemctl daemon-reload", srvPassword)},
		{"注册开机启动", fmt.Sprintf("echo '%s' | sudo -S systemctl enable frps", srvPassword)},
		{"启动服务", fmt.Sprintf("echo '%s' | sudo -S systemctl start frps", srvPassword)},
	}
	for _, step := range steps {
		output, err := sshExecWithOutput(sshClient, step.cmd)
		if err != nil {
			log.Printf("[公网穿透服务端] %s: %v (%s)", step.desc, err, strings.TrimSpace(output))
		}
	}

	// 7. 等待启动并验证
	time.Sleep(3 * time.Second)
	frspRunning := false
	for i := 0; i < 3; i++ {
		time.Sleep(2 * time.Second)
		psOutput, _ := sshExecWithOutput(sshClient, "ps aux | grep /usr/local/bin/frps | grep -v grep")
		if strings.TrimSpace(psOutput) != "" {
			frspRunning = true
			break
		}
	}
	if !frspRunning {
		// 输出诊断信息
		runOutput, _ := sshExecWithOutput(sshClient, fmt.Sprintf("echo '%s' | sudo -S timeout 5 /usr/local/bin/frps -c /etc/frps/frps.toml 2>&1 || true", srvPassword))
		log.Printf("[公网穿透服务端] 运行frps输出: %s", runOutput)
		configOutput, _ := sshExecWithOutput(sshClient, "cat /etc/frps/frps.toml")
		log.Printf("[公网穿透服务端] 配置文件: %s", configOutput)
		return errResult("frps启动失败\n运行输出: %s", strings.TrimSpace(runOutput))
	}

	// 检查端口是否监听
	portOutput, _ := sshExecWithOutput(sshClient, fmt.Sprintf("ss -tlnp | grep ':%d '", bindPort))
	if strings.TrimSpace(portOutput) == "" {
		log.Printf("[公网穿透服务端] 端口 %d 未监听", bindPort)
		return errResult("frps端口 %d 未监听，可能启动失败", bindPort)
	}

	dashboardURL := fmt.Sprintf("http://%s:%d", serverIP, dashboardPort)
	log.Printf("[公网穿透服务端] 安装成功: %s, 绑定端口: %d, 管理面板: %s", serverIP, bindPort, dashboardURL)

	return map[string]interface{}{
		"success":       true,
		"message":       fmt.Sprintf("公网穿透服务端安装成功，服务器: %s，绑定端口: %d，管理面板: %s", serverIP, bindPort, dashboardURL),
		"serverAddr":    serverIP,
		"bindPort":      bindPort,
		"dashboardURL":  dashboardURL,
		"dashboardUser": dashboardUser,
	}
}

// UninstallTunnelServer 卸载公网穿透服务端(frps)
func UninstallTunnelServer(serverIP string, srvUser string, srvPassword string, srvPort int) map[string]interface{} {
	log.Printf("[公网穿透服务端] 开始卸载: %s", serverIP)

	if srvUser == "" {
		srvUser = "root"
	}
	if srvPort == 0 {
		srvPort = 22
	}

	sshClient, err := dialServerSSH(serverIP, srvPort, srvUser, srvPassword)
	if err != nil {
		return errResult("%v", err)
	}
	defer sshClient.Close()

	steps := []struct {
		desc string
		cmd  string
	}{
		{"停止服务", fmt.Sprintf("echo '%s' | sudo -S systemctl stop frps 2>/dev/null || true", srvPassword)},
		{"禁用服务", fmt.Sprintf("echo '%s' | sudo -S systemctl disable frps 2>/dev/null || true", srvPassword)},
		{"删除服务文件", fmt.Sprintf("echo '%s' | sudo -S rm -f /etc/systemd/system/frps.service", srvPassword)},
		{"重载systemd", fmt.Sprintf("echo '%s' | sudo -S systemctl daemon-reload", srvPassword)},
		{"删除二进制", fmt.Sprintf("echo '%s' | sudo -S rm -f /usr/local/bin/frps", srvPassword)},
		{"删除配置", fmt.Sprintf("echo '%s' | sudo -S rm -rf /etc/frps", srvPassword)},
		{"清理临时文件", fmt.Sprintf("echo '%s' | sudo -S rm -rf /tmp/deploy-frps", srvPassword)},
	}

	for _, step := range steps {
		output, err := sshExecWithOutput(sshClient, step.cmd)
		if err != nil && !strings.Contains(output, "not found") {
			log.Printf("[公网穿透服务端] %s: %v (%s)", step.desc, err, strings.TrimSpace(output))
		}
	}

	log.Printf("[公网穿透服务端] 卸载成功")
	return map[string]interface{}{"success": true, "message": "公网穿透服务端卸载成功"}
}

// generateFrpsConfig 生成 frps.toml 配置内容
func generateFrpsConfig(bindPort int, dashboardPort int, dashboardUser string, dashboardPassword string) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("bindPort = %d\n", bindPort))

	// 允许自动分配的端口范围，避免与低端口冲突
	sb.WriteString("\nallowPorts = [\n")
	sb.WriteString("  { start = 10000, end = 60000 }\n")
	sb.WriteString("]\n")

	// Dashboard
	sb.WriteString("\n[webServer]\n")
	sb.WriteString("addr = \"0.0.0.0\"\n")
	sb.WriteString(fmt.Sprintf("port = %d\n", dashboardPort))
	sb.WriteString(fmt.Sprintf("user = \"%s\"\n", dashboardUser))
	sb.WriteString(fmt.Sprintf("password = \"%s\"\n", dashboardPassword))

	return sb.String()
}

// generateFrpsSystemdService 生成 frps systemd 服务文件
func generateFrpsSystemdService(dir string) {
	content := `[Unit]
Description=FRP Server - Public Network Tunnel
After=network.target

[Service]
Type=simple
User=root
WorkingDirectory=/etc/frps
ExecStart=/usr/local/bin/frps -c /etc/frps/frps.toml

Restart=always
RestartSec=3
LimitNOFILE=65535

[Install]
WantedBy=multi-user.target
`
	os.WriteFile(filepath.Join(dir, "frps.service"), []byte(content), 0644)
}
