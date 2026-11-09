package maintenance

import (
	"archive/zip"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pkg/sftp"
)

// v2HostIPResolver V2 序列号→主机 IP 解析回调（setup 注入 v2Svc 的
// 设备列表 + ARP/systeminfo 兜底链）。nil 时非 IP 标识直接报错。
var v2HostIPResolver func(serial string) (string, error)

// SetV2HostIPResolver 注入解析回调（main 包 setup 调用；internal 不能 import main）。
func SetV2HostIPResolver(fn func(serial string) (string, error)) {
	v2HostIPResolver = fn
}

// V2PanelOps V2 模式魔云互联的容器插件操作集（回调注入，实现在 main 包
// v2_service_panel.go：走 81 install/del_plugin + mytControl-link 容器，
// 对齐 V2 体系原生做法；SSH 二进制路径是 V3 专属，V2 主机无 OpenRC/systemd）。
// 当前仅为回滚通道（setup 不再注入，v2PanelOps 为 nil）。
type V2PanelOps struct {
	// Install 安装（经 81 install_plugin，镜像拉取分钟级）
	Install func(deviceIP string) map[string]interface{}
	// Uninstall 卸载（经 81 del_plugin）
	Uninstall func(deviceIP string) map[string]interface{}
	// Check 状态查询（2375 容器 label 比对；installed/version/running/url）
	Check func(deviceIP string) map[string]interface{}
	// Match 判定设备标识是否走 V2 面板路径（DeployMode=="v2"；入参为
	// extractPureIP 归一后的标识）
	Match func(deviceIP string) bool
}

var v2PanelOps *V2PanelOps

// SetV2PanelOps 注入 V2 魔云互联容器插件实现（setup 调用；nil = 无 V2 路径）。
func SetV2PanelOps(ops *V2PanelOps) {
	v2PanelOps = ops
}

// resolveHostIP 把设备标识归一为可直连的主机 IP：
//   - IP 格式（local 模式常规入参）→ 原样返回，零开销直通；
//   - 非 IP（V2 序列号：前端 getDeviceId 传序列号，且魔云互联装卸走任务队列
//     直调本包不经 wrapper）→ 经注入的解析链（设备列表→ARP→81 systeminfo）。
//
// 判定用「非 IP 即解析」而非部署模式：wrapper 与 taskqueue handler 两条路径
// 都收敛于此，不依赖各层 DeployMode 判定一致。
func resolveHostIP(deviceIP string) (string, error) {
	ip := extractPureIP(deviceIP)
	if net.ParseIP(ip) != nil {
		return ip, nil
	}
	if v2HostIPResolver == nil {
		return "", fmt.Errorf("无法解析设备标识 %s（非 IP 且解析服务未初始化）", deviceIP)
	}
	resolved, err := v2HostIPResolver(ip)
	if err != nil || resolved == "" {
		return "", fmt.Errorf("无法解析 V2 主机 IP（%s）: %v", ip, err)
	}
	return resolved, nil
}

// InstallMytPanel 安装魔云互联到指定设备
// 双路径：V2 回滚通道走 81 插件机制；其余走 SSH 推二进制（形态由部署模式
// 决定：v2 → myt-panel-v2 全参数 8181，v3 → myt-panel -port 8081）。
func InstallMytPanel(deviceIP string) map[string]interface{} {
	if v2PanelOps != nil && v2PanelOps.Match(deviceIP) {
		return v2PanelOps.Install(deviceIP)
	}
	ip, err := resolveHostIP(deviceIP)
	if err != nil {
		return errResult("%v", err)
	}
	form := panelFormByMode(ip)

	// 1. 查询最新版本
	downloadURL, latestVersion, sha256Hash, err := checkMytPanelUpdate()
	if err != nil {
		return errResult("查询最新版本失败: %v", err)
	}

	// 2. 下载到本地临时目录
	localDir := filepath.Join(os.TempDir(), "myt-panel-install")
	os.RemoveAll(localDir)
	os.MkdirAll(localDir, 0755)
	defer os.RemoveAll(localDir)

	binaryPath := filepath.Join(localDir, form.Binary)
	if err := downloadFile(downloadURL, binaryPath); err != nil {
		return errResult("下载安装包失败: %v", err)
	}
	// 双格式适配：响应可能直接是二进制，也可能是 zip 包（内含二进制）。
	// zip → 客户端本地解出二进制再上传；直接二进制 → 原样上传。
	binaryPath, err = extractPanelBinaryIfZip(binaryPath)
	if err != nil {
		return errResult("%v", err)
	}
	if sha256Hash != "" {
		os.WriteFile(filepath.Join(localDir, form.Binary+".sha256"), []byte(sha256Hash), 0644)
	}

	// 3. 生成服务配置文件（形态参数进入 command_args/ExecStart）
	deployDir := filepath.Join(localDir, "deploy")
	os.MkdirAll(deployDir, 0755)
	generateAlpineOpenRC(deployDir, form)
	generateDebianSystemd(deployDir, form)

	// 4. SSH 连接设备
	sshClient, err := DialSSH(ip)
	if err != nil {
		return errResult("SSH连接失败: %v", err)
	}
	defer sshClient.Close()

	// 5. 停旧服务 + 清理旧文件（两种形态的二进制名都清，防止模式切换后残留）
	RunSSHCmd(sshClient, fmt.Sprintf("echo '%s' | sudo -S sh -c 'rc-service %s stop 2>/dev/null; systemctl stop %s 2>/dev/null; killall %s 2>/dev/null'",
		sshPassword, form.Binary, form.Binary, form.Binary))
	time.Sleep(1 * time.Second)
	RunSSHCmd(sshClient, fmt.Sprintf("echo '%s' | sudo -S sh -c 'rm -f %s %s.sha256 /home/user/myt-panel /home/user/myt-panel.sha256'",
		sshPassword, form.RemotePath(), form.RemotePath()))
	RunSSHCmd(sshClient, fmt.Sprintf("echo '%s' | sudo -S rm -rf /home/user/deploy", sshPassword))

	// 6. SFTP 上传
	sftpClient, err := sftp.NewClient(sshClient)
	if err != nil {
		return errResult("SFTP连接失败: %v", err)
	}
	defer sftpClient.Close()

	sftpClient.MkdirAll("/home/user/deploy")
	if err := SftpUploadFile(sftpClient, binaryPath, form.RemotePath(), 0755); err != nil {
		return errResult("上传%s失败: %v", form.Binary, err)
	}
	sha256Path := filepath.Join(localDir, form.Binary+".sha256")
	if _, err := os.Stat(sha256Path); err == nil {
		SftpUploadFile(sftpClient, sha256Path, form.RemotePath()+".sha256", 0644)
	}
	if err := SftpUploadDir(sftpClient, deployDir, "/home/user/deploy"); err != nil {
		return errResult("上传部署脚本失败: %v", err)
	}

	// 7. 检测 OS 并注册服务
	osType, _ := detectDeviceOS(sshClient)
	log.Printf("[维护] 设备 %s OS: %s", ip, osType)

	if osType == "alpine" {
		err = installAlpineService(sshClient, form)
	} else {
		err = installDebianService(sshClient, form)
	}
	if err != nil {
		return errResult("%v", err)
	}

	// 8. 健康检查
	panelURL := form.URL(ip)
	for i := 0; i < 5; i++ {
		time.Sleep(2 * time.Second)
		output, _ := RunSSHCmd(sshClient, form.ListenGrep())
		if strings.Contains(output, fmt.Sprintf(":%d", form.Port)) {
			log.Printf("[维护] 魔云互联安装成功: %s", panelURL)
			return map[string]interface{}{
				"success": true,
				"message": fmt.Sprintf("魔云互联 v%s 安装成功", latestVersion),
				"url":     panelURL,
			}
		}
		if osType == "alpine" {
			RunSSHCmd(sshClient, fmt.Sprintf("echo '%s' | sudo -S rc-service %s start 2>/dev/null", sshPassword, form.Binary))
		} else {
			RunSSHCmd(sshClient, fmt.Sprintf("echo '%s' | sudo -S systemctl start %s 2>/dev/null", sshPassword, form.Binary))
		}
	}

	return map[string]interface{}{
		"success": true,
		"message": fmt.Sprintf("魔云互联 v%s 安装完成，服务启动中，请稍后访问 %s", latestVersion, panelURL),
		"url":     panelURL,
	}
}

// extractPanelBinaryIfZip 安装包格式适配：filename 以 .zip 结尾（或内容是
// ZIP 魔数）→ 本地解压取内含的主二进制；否则直接当二进制原样返回。
// 选择逻辑对齐 v2sdk.go 的 extractV2SdkBinary：优先找名为 myt-panel 的条目，
// 找不到就取第一个非目录、非隐藏的条目当主二进制。
func extractPanelBinaryIfZip(path string) (string, error) {
	isZip, err := isZipFile(path)
	if err != nil {
		return "", err
	}
	if !isZip {
		return path, nil
	}
	log.Printf("[维护] 安装包为 zip 格式，本地解压取主二进制")
	outPath := filepath.Join(filepath.Dir(path), "myt-panel-extracted")
	if err := extractZipBinary(path, outPath); err != nil {
		return "", err
	}
	return outPath, nil
}

// isZipFile 靠魔数 PK\x03\x04 判断文件是否 ZIP（服务端 filename 不可靠时兜底）。
func isZipFile(path string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()
	sig := make([]byte, 4)
	if _, err := io.ReadFull(f, sig); err != nil {
		return false, err
	}
	return sig[0] == 'P' && sig[1] == 'K' && sig[2] == 0x03 && sig[3] == 0x04, nil
}

// extractZipBinary 从 zip 内解出主二进制写到 outPath：
// 先找名为 myt-panel 的条目（含 myt-panel-v2）；zip 内无该名条目则取第一个
// 非目录非隐藏条目（单文件 zip 的常规形态）。
func extractZipBinary(zipPath, outPath string) error {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("打开 zip 安装包失败: %w", err)
	}
	defer zr.Close()

	var pick *zip.File
	for _, f := range zr.File {
		base := filepath.Base(f.Name)
		if strings.HasPrefix(base, "myt-panel") {
			pick = f
			break
		}
	}
	if pick == nil {
		for _, f := range zr.File {
			if f.FileInfo().IsDir() || strings.HasPrefix(filepath.Base(f.Name), ".") {
				continue
			}
			pick = f
			break
		}
	}
	if pick == nil {
		return fmt.Errorf("zip 安装包内未找到可用的二进制条目")
	}

	rc, err := pick.Open()
	if err != nil {
		return fmt.Errorf("解压二进制失败: %w", err)
	}
	defer rc.Close()
	out, err := os.Create(outPath)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, rc); err != nil {
		out.Close()
		os.Remove(outPath)
		return fmt.Errorf("解压二进制失败: %w", err)
	}
	return out.Close()
}

// UninstallMytPanel 卸载魔云互联（V2 回滚通道走 81 del_plugin；其余 SSH 清理）。
// 清理对两种形态的二进制名都生效（v2/v3 模式切换后旧残留也能清掉）。
func UninstallMytPanel(deviceIP string) map[string]interface{} {
	if v2PanelOps != nil && v2PanelOps.Match(deviceIP) {
		return v2PanelOps.Uninstall(deviceIP)
	}
	ip, err := resolveHostIP(deviceIP)
	if err != nil {
		return errResult("%v", err)
	}
	log.Printf("[维护] 开始卸载魔云互联: %s", ip)

	sshClient, err := DialSSH(ip)
	if err != nil {
		return errResult("SSH连接失败: %v", err)
	}
	defer sshClient.Close()

	osType, _ := detectDeviceOS(sshClient)

	if osType == "alpine" {
		steps := []struct{ desc, cmd string }{
			{"停止服务", fmt.Sprintf("echo '%s' | sudo -S rc-service myt-panel stop 2>/dev/null || true", sshPassword)},
			{"移除开机启动", fmt.Sprintf("echo '%s' | sudo -S rc-update del myt-panel default 2>/dev/null || true", sshPassword)},
			{"删除服务文件", fmt.Sprintf("echo '%s' | sudo -S rm -f /etc/init.d/myt-panel", sshPassword)},
		}
		for _, step := range steps {
			output, e := sshExecWithOutput(sshClient, step.cmd)
			if e != nil && !strings.Contains(output, "not found") && !strings.Contains(output, "not installed") {
				log.Printf("[维护] %s: %v (%s)", step.desc, e, strings.TrimSpace(output))
			}
		}
	} else {
		steps := []struct{ desc, cmd string }{
			{"停止服务", fmt.Sprintf("echo '%s' | sudo -S systemctl stop myt-panel 2>/dev/null || true", sshPassword)},
			{"禁用服务", fmt.Sprintf("echo '%s' | sudo -S systemctl disable myt-panel 2>/dev/null || true", sshPassword)},
			{"删除服务文件", fmt.Sprintf("echo '%s' | sudo -S rm -f /etc/systemd/system/myt-panel.service", sshPassword)},
			{"重载systemd", fmt.Sprintf("echo '%s' | sudo -S systemctl daemon-reload", sshPassword)},
		}
		for _, step := range steps {
			output, e := sshExecWithOutput(sshClient, step.cmd)
			if e != nil && !strings.Contains(output, "not found") {
				log.Printf("[维护] %s: %v (%s)", step.desc, e, strings.TrimSpace(output))
			}
		}
	}

	// 两种形态的二进制/校验文件 + inittab 旧 respawn 条目一并清理
	RunSSHCmd(sshClient, fmt.Sprintf("echo '%s' | sudo -S sh -c 'rm -f /home/user/myt-panel /home/user/myt-panel.sha256 /home/user/myt-panel-v2 /home/user/myt-panel-v2.sha256'", sshPassword))
	RunSSHCmd(sshClient, fmt.Sprintf("echo '%s' | sudo -S sh -c 'grep -v \"myt-panel\" /etc/inittab > /tmp/inittab.new 2>/dev/null && cat /tmp/inittab.new > /etc/inittab && rm -f /tmp/inittab.new || true'", sshPassword))
	RunSSHCmd(sshClient, fmt.Sprintf("echo '%s' | sudo -S rm -rf /home/user/deploy /home/user/logs", sshPassword))

	return map[string]interface{}{
		"success": true,
		"message": "魔云互联卸载成功",
	}
}

// CheckMytPanelInstalled 检查魔云互联是否已安装（V2 回滚通道查容器 label；
// SSH 路径按形态端口查监听 + 按形态二进制查存在性）
//
// SSH 路径的判定链（修复误报：此前端口不监听一律判「未安装」——设备重启后
// nohup 进程丢失，但二进制还在，用户会被误导去重装）：
//   - 形态端口监听           → installed=true, running=true
//   - 二进制存在但未监听     → installed=true, running=false（已安装未运行，可启动）
//   - 二进制也不存在         → installed=false
func CheckMytPanelInstalled(deviceIP string) map[string]interface{} {
	if v2PanelOps != nil && v2PanelOps.Match(deviceIP) {
		return v2PanelOps.Check(deviceIP)
	}
	ip, err := resolveHostIP(deviceIP)
	if err != nil {
		return map[string]interface{}{"installed": false, "message": err.Error()}
	}
	form := panelFormByMode(ip)
	sshClient, err := DialSSH(ip)
	if err != nil {
		return map[string]interface{}{"installed": false}
	}
	defer sshClient.Close()

	output, _ := sshExecWithOutput(sshClient, form.ListenGrep())
	if strings.Contains(output, fmt.Sprintf(":%d", form.Port)) {
		return map[string]interface{}{
			"installed": true,
			"running":   true,
			"url":       form.URL(ip),
		}
	}
	// 未监听：test -f 区分「已安装未运行」与「真未安装」。
	output, _ = sshExecWithOutput(sshClient, fmt.Sprintf("test -f %s && echo YES || echo NO", form.RemotePath()))
	binaryExists := strings.Contains(output, "YES")
	return map[string]interface{}{
		"installed": binaryExists,
		"running":   false,
		"url":       form.URL(ip),
	}
}

// StartMytPanel 拉起设备上已安装但未运行的魔云互联面板（重启后 nohup 进程
// 丢失、respawn 条目未生效前的手动补救）：SSH nohup 后台拉起 + 健康检查。
// v2 与 v3 均走本 SSH 路径（v2 已并入 SSH 二进制方案，81 容器插件仅作回滚
// 通道，见 setup.go 注入处注释）；二进制不存在的未安装设备返回明确错误。
func StartMytPanel(deviceIP string) map[string]interface{} {
	ip, err := resolveHostIP(deviceIP)
	if err != nil {
		return errResult("%v", err)
	}
	form := panelFormByMode(ip)
	log.Printf("[维护] 手动启动魔云互联面板: %s（形态 %s）", ip, form.Binary)

	sshClient, err := DialSSH(ip)
	if err != nil {
		return errResult("SSH连接失败: %v", err)
	}
	defer sshClient.Close()

	// 前置校验：二进制不存在时 nohup 拉不起任何东西，直接报未安装。
	output, _ := sshExecWithOutput(sshClient, fmt.Sprintf("test -f %s && echo YES || echo NO", form.RemotePath()))
	if !strings.Contains(output, "YES") {
		return errResult("设备上未找到 %s，请先安装魔云互联", form.RemotePath())
	}

	// 幂等：已在运行（形态端口监听）直接返回成功，避免重复拉起。
	output, _ = sshExecWithOutput(sshClient, form.ListenGrep())
	panelURL := form.URL(ip)
	if strings.Contains(output, fmt.Sprintf(":%d", form.Port)) {
		return map[string]interface{}{"success": true, "message": "魔云互联已在运行", "url": panelURL}
	}

	startCmd := nohupStartCmd(form)
	if _, err := sshExecWithOutput(sshClient, startCmd); err != nil {
		return errResult("启动失败: %v", err)
	}
	// 健康检查（安装链路同款：轮询形态端口监听）
	for i := 0; i < 5; i++ {
		time.Sleep(2 * time.Second)
		output, _ := sshExecWithOutput(sshClient, form.ListenGrep())
		if strings.Contains(output, fmt.Sprintf(":%d", form.Port)) {
			log.Printf("[维护] 魔云互联手动启动成功: %s", panelURL)
			return map[string]interface{}{"success": true, "message": "魔云互联启动成功", "url": panelURL}
		}
	}
	return errResult("启动指令已执行，但 10 秒内未检测到 %d 监听，请查看设备日志 %s/logs/myt-panel.log", form.Port, panelRemoteDir)
}
