package maintenance

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

const (
	sshUser     = "user"
	sshPassword = "myt"
	sshPort     = 22
)

// errResult 维护操作的统一失败返回（taskqueue handler 只读 success/message 两键）。
// ErrResult 导出版供 main 包的 v2 面板实现复用（同构返回结构）。
func errResult(format string, args ...interface{}) map[string]interface{} {
	return map[string]interface{}{
		"success": false,
		"message": fmt.Sprintf(format, args...),
	}
}

// ErrResult errResult 的导出别名（跨包复用）。
func ErrResult(format string, args ...interface{}) map[string]interface{} {
	return errResult(format, args...)
}

func dialSSH(deviceIP string) (*ssh.Client, error) {
	config := &ssh.ClientConfig{
		User:            sshUser,
		Auth:            []ssh.AuthMethod{ssh.Password(sshPassword)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         10 * time.Second,
	}
	addr := fmt.Sprintf("%s:%d", deviceIP, sshPort)
	return ssh.Dial("tcp", addr, config)
}

// DialSSH 导出，供 agent 包跨包调用
func DialSSH(deviceIP string) (*ssh.Client, error) {
	return dialSSH(deviceIP)
}

func runSSHCmd(client *ssh.Client, cmd string) (string, error) {
	return sshExecWithOutput(client, cmd)
}

// RunSSHCmd 导出，供 agent 包跨包调用
func RunSSHCmd(client *ssh.Client, cmd string) (string, error) {
	return runSSHCmd(client, cmd)
}

func sshExecWithOutput(client *ssh.Client, cmd string) (string, error) {
	session, err := client.NewSession()
	if err != nil {
		return "", fmt.Errorf("创建SSH session失败: %w", err)
	}
	defer session.Close()

	done := make(chan struct{})
	var output []byte
	var execErr error
	go func() {
		output, execErr = session.CombinedOutput(cmd)
		close(done)
	}()

	select {
	case <-done:
		if execErr != nil {
			// 命令级日志：命令 + 输出 + 退出码。之前只报「执行命令失败: Process
			// exited with status 1」，现场无法定位是哪条命令、sudo 说了什么。
			var exitCode int
			var exitErr *ssh.ExitError
			if errors.As(execErr, &exitErr) {
				exitCode = exitErr.ExitStatus()
			}
			log.Printf("[ssh] 命令失败 exit=%d cmd=%q output=%q", exitCode, cmd, truncateForLog(string(output), 400))
			return string(output), fmt.Errorf("执行命令失败(exit=%d): %w", exitCode, execErr)
		}
		return string(output), nil
	case <-time.After(5 * time.Minute):
		session.Close()
		return "", fmt.Errorf("执行命令超时")
	}
}

// truncateForLog 日志截断（防 sudo 提示/大输出刷屏）。
func truncateForLog(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func detectDeviceOS(client *ssh.Client) (string, error) {
	output, err := sshExecWithOutput(client, "cat /etc/os-release 2>/dev/null | grep -i alpine || echo 'NOT_ALPINE'")
	if err == nil && strings.Contains(strings.ToLower(output), "alpine") {
		return "alpine", nil
	}
	output2, err2 := sshExecWithOutput(client, "which apk 2>/dev/null || echo 'NO_APK'")
	if err2 == nil && strings.Contains(output2, "apk") && !strings.Contains(output2, "NO_APK") {
		return "alpine", nil
	}
	return "debian", nil
}

func sftpUploadFile(client *sftp.Client, localPath, remotePath string, perm os.FileMode) error {
	localFile, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf("打开本地文件失败 %s: %w", localPath, err)
	}
	defer localFile.Close()

	remoteFile, err := client.Create(remotePath)
	if err != nil {
		return fmt.Errorf("创建远程文件失败 %s: %w", remotePath, err)
	}
	defer remoteFile.Close()

	// ReadFrom 无内在超时：TCP 半开（设备掉电/网络中断未 RST）会永久阻塞——
	// 曾致安装任务永远「处理中」且 installMu 永不释放（其他安装任务全被拒）。
	// 后台 goroutine 拷贝 + 定时器守护，超时强返回（SFTP 连接随之作废，由
	// 调用方关闭 ssh client 兜底）。
	done := make(chan error, 1)
	go func() {
		_, err := remoteFile.ReadFrom(localFile)
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			return fmt.Errorf("写入远程文件失败 %s: %w", remotePath, err)
		}
	case <-time.After(10 * time.Minute):
		return fmt.Errorf("SFTP 上传超时（10 分钟）: %s", remotePath)
	}
	client.Chmod(remotePath, perm)
	return nil
}

// SftpUploadFile 导出，供 agent 包跨包调用
func SftpUploadFile(client *sftp.Client, localPath, remotePath string, perm os.FileMode) error {
	return sftpUploadFile(client, localPath, remotePath, perm)
}

// SftpUploadDir 导出，供 agent 包跨包调用
func SftpUploadDir(client *sftp.Client, localDir, remoteDir string) error {
	return sftpUploadDir(client, localDir, remoteDir)
}

func sftpUploadDir(client *sftp.Client, localDir, remoteDir string) error {
	client.MkdirAll(remoteDir)
	entries, err := os.ReadDir(localDir)
	if err != nil {
		return fmt.Errorf("读取本地目录失败: %w", err)
	}
	for _, entry := range entries {
		lp := localDir + "/" + entry.Name()
		rp := remoteDir + "/" + entry.Name()
		if entry.IsDir() {
			if err := sftpUploadDir(client, lp, rp); err != nil {
				return err
			}
		} else {
			perm := os.FileMode(0644)
			if strings.HasSuffix(entry.Name(), ".sh") {
				perm = 0755
			}
			if err := sftpUploadFile(client, lp, rp, perm); err != nil {
				return err
			}
		}
	}
	return nil
}

func extractPureIP(ip string) string {
	if ip == "" {
		return ip
	}
	lastColon := strings.LastIndex(ip, ":")
	if lastColon == -1 {
		return ip
	}
	afterColon := ip[lastColon+1:]
	if isAllDigits(afterColon) {
		return ip[:lastColon]
	}
	return ip
}

// ExtractPureIP 导出，供 main/taskqueue 包跨包调用（tebox 安装锁键等）
func ExtractPureIP(ip string) string {
	return extractPureIP(ip)
}

func isAllDigits(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return len(s) > 0
}

// SSHExecWithTimeout 可自定义超时的 SSH 执行（供 rsync/格式化等长操作使用）
func SSHExecWithTimeout(deviceIP, command string, timeout time.Duration) (string, error) {
	ip := extractPureIP(deviceIP)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	config := &ssh.ClientConfig{
		User:            sshUser,
		Auth:            []ssh.AuthMethod{ssh.Password(sshPassword)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         10 * time.Second,
	}
	addr := fmt.Sprintf("%s:%d", ip, sshPort)

	dialDone := make(chan struct{})
	var client *ssh.Client
	var dialErr error
	go func() {
		client, dialErr = ssh.Dial("tcp", addr, config)
		close(dialDone)
	}()

	select {
	case <-ctx.Done():
		return "", fmt.Errorf("SSH连接超时")
	case <-dialDone:
		if dialErr != nil {
			return "", fmt.Errorf("SSH连接失败: %w", dialErr)
		}
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return "", fmt.Errorf("创建SSH session失败: %w", err)
	}
	defer session.Close()

	var stdout strings.Builder
	session.Stdout = &stdout
	session.Stderr = &stdout

	done := make(chan struct{})
	var waitErr error
	go func() {
		waitErr = session.Run(command)
		close(done)
	}()

	select {
	case <-ctx.Done():
		return stdout.String(), fmt.Errorf("命令执行超时")
	case <-done:
	}

	return strings.TrimSpace(stdout.String()), waitErr
}

// SSHExecSimple 简单 SSH 执行（30秒超时，供日志清理等使用）
func SSHExecSimple(deviceIP, command string) (string, error) {
	return SSHExecWithTimeout(deviceIP, command, 30*time.Second)
}

// installAlpineService 注册 Alpine 系统服务；失败降级 nohup 拉起（V2 主机等
// 精简系统无 rc-update/rc-service——os-release 检测可能仍判 alpine）。
// form 由调用方（InstallMytPanel，host IP 已解析）传入——nohup 兜底/respawn
// 的 v2 启动参数需要 -host-ip <真实IP>，此处不能自行按空 IP 构造形态。
func installAlpineService(sshClient *ssh.Client, form PanelForm) error {
	steps := []struct{ desc, cmd string }{
		{"复制服务文件", fmt.Sprintf("echo '%s' | sudo -S sh -c 'cp /home/user/deploy/alpine-openrc /etc/init.d/myt-panel && sed -i \"s/\\\\r$//\" /etc/init.d/myt-panel && chmod +x /etc/init.d/myt-panel'", sshPassword)},
		{"创建日志目录", fmt.Sprintf("echo '%s' | sudo -S mkdir -p /home/user/logs", sshPassword)},
		{"注册开机启动", fmt.Sprintf("echo '%s' | sudo -S rc-update add myt-panel default 2>/dev/null || true", sshPassword)},
		{"启动服务", fmt.Sprintf("echo '%s' | sudo -S rc-service myt-panel start", sshPassword)},
	}
	if err := runInstallSteps(sshClient, steps); err != nil {
		return fallbackNohupStart(sshClient, form, "alpine(rc-update/rc-service 不可用)", err)
	}
	return nil
}

// installDebianService 注册 Debian 系统服务；失败降级 nohup 拉起（V2 主机等
// 精简系统无 systemd——os-release 检测可能仍判 debian）。form 同上由调用方传入。
func installDebianService(sshClient *ssh.Client, form PanelForm) error {
	steps := []struct{ desc, cmd string }{
		{"复制服务文件", fmt.Sprintf("echo '%s' | sudo -S sh -c 'cp /home/user/deploy/debian-systemd.service /etc/systemd/system/myt-panel.service && sed -i \"s/\\\\r$//\" /etc/systemd/system/myt-panel.service'", sshPassword)},
		{"重载systemd", fmt.Sprintf("echo '%s' | sudo -S systemctl daemon-reload", sshPassword)},
		{"注册开机启动", fmt.Sprintf("echo '%s' | sudo -S systemctl enable myt-panel", sshPassword)},
		{"创建日志目录", fmt.Sprintf("echo '%s' | sudo -S mkdir -p /home/user/logs", sshPassword)},
		{"启动服务", fmt.Sprintf("echo '%s' | sudo -S systemctl start myt-panel", sshPassword)},
	}
	if err := runInstallSteps(sshClient, steps); err != nil {
		return fallbackNohupStart(sshClient, form, "debian(systemctl 不可用)", err)
	}
	return nil
}

// fallbackNohupStart 服务注册失败时的 nohup 兜底拉起（V2 主机实测无 OpenRC/
// systemd；SDK 部署链路 v2sdk.go 同款模式：sudo nohup 后台拉起，进程自杀或断电
// 后不自动重启、重启后需重新安装——与 SDK 兜底的自启语义一致）。
// nohup 拉起成功后追加 busybox inittab respawn 注册（registerInittabRespawn），
// 覆盖 nohup 兜底「设备重启后面板丢失」的缺口。
// 日志路径与 OpenRC/systemd 配置一致（/home/user/logs/myt-panel.log）。
// 启动命令与 kill/pkill 模式按面板形态取（v2: myt-panel-v2 全参数）。
func fallbackNohupStart(sshClient *ssh.Client, form PanelForm, cause string, regErr error) error {
	log.Printf("[维护] 服务注册失败（%s）: %v，降级 nohup 拉起", cause, regErr)
	killCmd := fmt.Sprintf(
		"echo '%s' | sudo -S sh -c 'pkill -f \"(^|/)%s$\" 2>/dev/null; true'", sshPassword, form.Binary)
	RunSSHCmd(sshClient, killCmd) // 无旧进程返回 1 正常，不阻断
	startCmd := nohupStartCmd(form)
	if _, err := sshExecWithOutput(sshClient, startCmd); err != nil {
		return fmt.Errorf("服务注册失败且 nohup 兜底拉起失败（%s）: %w", cause, regErr)
	}
	// respawn 注册失败不影响安装结果（只读 fs 等场景容忍），仅告警。
	if err := registerInittabRespawn(sshClient, form); err != nil {
		log.Printf("[维护] inittab respawn 注册失败（设备重启后面板不会自启）: %v", err)
	}
	log.Printf("[维护] nohup 兜底拉起成功 + inittab respawn 已注册（当前进程由 nohup 管，设备重启后 init 接管）")
	return nil
}

// nohupStartCmd 面板 nohup 后台拉起命令（/home/user 下相对路径执行）。
// v3: ./myt-panel -port 8081；v2: ./myt-panel-v2 -host-ip <ip> -sdk-port 81 -port 8181。
func nohupStartCmd(form PanelForm) string {
	return fmt.Sprintf(
		"cd /home/user && echo '%s' | sudo -S -p '' sh -c 'nohup %s >> /home/user/logs/myt-panel.log 2>&1 &' 2>&1",
		sshPassword, form.RelCommandLine())
}

// registerInittabRespawn 向 /etc/inittab 追加 busybox respawn 条目
// 「::respawn:<形态启动命令>」，保障设备重启后面板自启。
//
// 生效与防冲突取舍：
//   - busybox init 只在自身启动（或收到 SIGHUP，行为随版本不一）时重读
//     inittab。这里**不做 kill -HUP 1 热加载**：当前会话进程刚被 nohup 拉起，
//     若 init 立即接管 respawn 会再拉一份，端口冲突第二个实例退出（无害
//     但日志噪音）；respawn 条目写盘即够——我们主要保的就是重启场景，重启后
//     init 必然重读 inittab 接管。
//   - 幂等 + 旧条目替换：专查本形态二进制（v2 查 myt-panel-v2，v3 查
//     myt-panel 且排除 v2 行）；若存在**另一形态**的旧条目（如设备此前装过
//     v3 口径的 myt-panel -port 8081，现切 v2），先 sed 删旧行再追加——避免
//     双 respawn 抢同一日志/行为混乱。删除是宽匹配（含 'myt-panel' 的行），
//     因为旧条目格式不可枚举。
func registerInittabRespawn(sshClient *ssh.Client, form PanelForm) error {
	checkCmd := fmt.Sprintf("grep -q '%s' /etc/inittab 2>/dev/null && echo EXISTS || echo ABSENT", form.Binary)
	output, err := sshExecWithOutput(sshClient, checkCmd)
	if err == nil && strings.Contains(output, "EXISTS") {
		log.Printf("[维护] /etc/inittab 已有 %s respawn 条目，跳过注册", form.Binary)
		return nil
	}
	// 删除另一形态的旧条目（含 'myt-panel' 但不含本形态二进制名的行）；
	// v3 形态下 myt-panel 是 v2 条目 myt-panel-v2 的子串，grep -v 反向排除
	// 会把 v3 自己的行也留下——用词边界锚定：先按完整二进制名匹配删。
	// v2 旧条目（myt-panel-v2 ...）在 v3 模式下同样被此宽删清掉。
	legacyCmd := fmt.Sprintf(
		"echo '%s' | sudo -S sh -c \"sed -i '/myt-panel/d' /etc/inittab 2>/dev/null; true\"", sshPassword)
	if _, err := sshExecWithOutput(sshClient, legacyCmd); err != nil {
		log.Printf("[维护] 清理 /etc/inittab 旧 myt-panel 条目失败（继续追加）: %v", err)
	}
	appendCmd := fmt.Sprintf(
		"echo '%s' | sudo -S sh -c 'echo \"::respawn:%s\" >> /etc/inittab'",
		sshPassword, form.CommandLine())
	if _, err := sshExecWithOutput(sshClient, appendCmd); err != nil {
		return fmt.Errorf("追加 /etc/inittab 失败: %w", err)
	}
	// 回读校验：sudo 无输出，grep 命中即视为写入成功（同时覆盖只读 fs
	// 的场景——echo >> 失败时 grep 不会命中，不依赖上一条命令的退出码）。
	verify, err := sshExecWithOutput(sshClient, checkCmd)
	if err != nil || !strings.Contains(verify, "EXISTS") {
		return fmt.Errorf("/etc/inittab 写入后回读校验未命中（可能只读 fs）")
	}
	log.Printf("[维护] 已注册 busybox inittab respawn: ::respawn:%s", form.CommandLine())
	return nil
}

func runInstallSteps(sshClient *ssh.Client, steps []struct{ desc, cmd string }) error {
	for _, step := range steps {
		output, err := sshExecWithOutput(sshClient, step.cmd)
		if err != nil {
			errMsg := fmt.Sprintf("%s失败", step.desc)
			clean := strings.TrimSpace(strings.ReplaceAll(output, "[sudo] password for user:", ""))
			if clean != "" {
				errMsg = fmt.Sprintf("%s失败: %s", step.desc, clean)
			}
			log.Printf("[维护] %s", errMsg)
			return fmt.Errorf("%s", errMsg)
		}
		log.Printf("[维护] %s成功", step.desc)
	}
	return nil
}

// SSHBatchExec 一次SSH连接执行多条命令，返回每条命令的输出
// cmds: 命令标签 -> 命令内容，返回: 标签 -> 输出
func SSHBatchExec(deviceIP string, cmds map[string]string) (map[string]string, error) {
	ip := extractPureIP(deviceIP)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	config := &ssh.ClientConfig{
		User:            sshUser,
		Auth:            []ssh.AuthMethod{ssh.Password(sshPassword)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         10 * time.Second,
	}
	addr := fmt.Sprintf("%s:%d", ip, sshPort)

	dialDone := make(chan struct{})
	var client *ssh.Client
	var dialErr error
	go func() {
		client, dialErr = ssh.Dial("tcp", addr, config)
		close(dialDone)
	}()

	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("SSH连接超时")
	case <-dialDone:
		if dialErr != nil {
			return nil, fmt.Errorf("SSH连接失败: %w", dialErr)
		}
	}
	defer client.Close()

	// 用分号拼接所有命令，带标签前缀
	var parts []string
	for tag, cmd := range cmds {
		parts = append(parts, fmt.Sprintf("echo \"===%s===\"; %s", tag, cmd))
	}
	combined := strings.Join(parts, "; ")

	session, err := client.NewSession()
	if err != nil {
		return nil, fmt.Errorf("创建SSH session失败: %w", err)
	}
	defer session.Close()

	var stdout strings.Builder
	session.Stdout = &stdout
	session.Stderr = &stdout

	done := make(chan struct{})
	var waitErr error
	go func() {
		waitErr = session.Run(combined)
		close(done)
	}()

	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("命令执行超时")
	case <-done:
	}

	if waitErr != nil && stdout.Len() == 0 {
		return nil, fmt.Errorf("批量命令执行失败: %w", waitErr)
	}

	// 解析输出，按标签分割
	output := stdout.String()
	results := make(map[string]string)
	for tag := range cmds {
		marker := "===" + tag + "==="
		start := strings.Index(output, marker)
		if start == -1 {
			results[tag] = ""
			continue
		}
		start += len(marker)
		end := len(output)
		for t2 := range cmds {
			m2 := "===" + t2 + "==="
			if idx := strings.Index(output[start:], m2); idx >= 0 && start+idx < end {
				end = start + idx
			}
		}
		results[tag] = strings.TrimSpace(output[start:end])
	}

	return results, nil
}
