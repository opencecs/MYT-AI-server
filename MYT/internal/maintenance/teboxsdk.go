package maintenance

// tebox_player SDK（tobox_player）远程安装——V3 本地模式设备右键「tebox 安装」。
//
// 与 v2sdk.go（V2 盒子 SDK）同型的 SSH 部署链路，但安装动作完全由设备端
// install-sdk.sh 完成（tobox_player 仓库 scripts/install-sdk.sh，go:embed 内嵌）：
//
//	SSH 连设备（user/myt，ssh_utils）→ SFTP 上传脚本到 /tmp →
//	TOBOX_SUDO_PASS=myt bash /tmp/install-sdk.sh --zip /tmp/tebox-sdk.zip
//	→ 轮询 http://{ip}:8088/health 确认就绪
//
// 基座容器两步（teboxsdk_base.go）：确保基座镜像 tebox:1.0.0 存在（本地没有
// 则从 mytos/dobox 仓库 pull 后 tag——tebox:1.0.0 = dobox:tebox_v1.0.0 同镜像），
// install-sdk.sh 装完 tobox_player
// 后执行 start-tebox-hw.sh 启动基座容器（先基座后 player——CreateInstance
// 依赖基座 gRPC 50051），verify 再加验 50051 TCP 连通。
//
// zip 来源选型：客户端从 newapi 拉包后 SFTP 上传（不走设备侧 newapi 直下）。
// 理由：install-sdk.sh 的 newapi 路径要求设备有 curl/wget + sha256sum 且外网
// 可达，V3 ARM 设备外网连通性不可保证；客户端统一拉包（~8MB）可控、可校验
// （zip 内 hash/version 三件套核验），设备侧零依赖。
//
// zip 候选顺序（供未来扩展）：显式 zipPath 参数 > 本地缓存 sdkDir/tebox-sdk.zip
// > newapi tebox_sdk 类型下载。

import (
	_ "embed"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/moyunteng/myt/internal/sdkver"
	"github.com/pkg/sftp"
)

//go:embed scripts/install-sdk.sh
var teboxInstallScript []byte

const (
	// teboxSdkAPIBase newapi 接口基地址（tebox_sdk 类型，与 install-sdk.sh 的
	// NEWAPI_BASE 一致）
	teboxSdkAPIBase = "https://newapi.moyunteng.com/api/v1"
	// teboxSdkType newapi 上 tobox_player SDK 的类型标识
	teboxSdkType = "tebox_sdk"
	// teboxSdkFilename newapi 侧 zip 文件名（download-url 的 filename 参数）
	teboxSdkFilename = "tebox-sdk.zip"
	// teboxRemoteScript 设备端安装脚本路径（/tmp，重启即清，不留垃圾）
	teboxRemoteScript = "/tmp/install-sdk.sh"
	// teboxRemoteZip 设备端安装包路径
	teboxRemoteZip = "/tmp/tebox-sdk.zip"
	// teboxHealthPort tobox_player SDK HTTP 端口（install-sdk.sh 默认 --port 8088）
	teboxHealthPort = 8088
	// teboxInstallOverallTimeout 安装整体兜底超时：脚本内部各步有超时（newapi
	// 取包/下载 30s、健康验证 30s），但 SSH 会话本身可能挂死——任务队列 worker
	// 层还有一道 20min 兜底，这里收紧到 10min 与 sdkInstall 链路一致。
	teboxInstallOverallTimeout = 10 * time.Minute
	// teboxHealthPollTimeout /health 就绪轮询上限：脚本自身已轮询 30s，这里
	// 再给 60s 余量（冷启动二进制加载/守护循环拉起）。
	teboxHealthPollTimeout = 60 * time.Second
	// teboxVerifyPollTimeout verify 阶段轮询总上限：/health 60s + 基座 gRPC
	// 50051 60s（teboxsdk_base.go 的 teboxGRPCPollTimeout，窗口对齐）。
	teboxVerifyPollTimeout = teboxHealthPollTimeout + teboxGRPCPollTimeout
)

// teboxSdkProgress 安装进度回调（stage: prepare/pull/load/upload/install/base/verify）
type teboxSdkProgress func(stage, message string)

// CheckTeboxSdkHealth 探测设备 8088 /health 是否就绪（tobox_player SDK）。
// 判定：HTTP 200 + 响应体含 "ok"（/health 返回 {"status":"ok"}，宽松匹配
// 避免对字段顺序/引号格式过度约束）。
func CheckTeboxSdkHealth(deviceIP string) bool {
	resp, err := httpGetFast(fmt.Sprintf("http://%s:%d/health", extractPureIP(deviceIP), teboxHealthPort))
	if err != nil {
		return false
	}
	if resp.StatusCode != 200 {
		return false
	}
	return strings.Contains(resp.Body, "ok")
}

// TeboxSdkStatus 设备 tebox_player SDK 安装状态查询（前端「tebox 安装」弹窗展示）。
// installed = 8088 /health 就绪。
func TeboxSdkStatus(deviceIP string) map[string]interface{} {
	installed := CheckTeboxSdkHealth(deviceIP)
	return map[string]interface{}{
		"installed": installed,
		"port":      teboxHealthPort,
	}
}

// FetchTeboxSdkZip 从 newapi 获取最新 tebox-sdk.zip（原始 zip 保留，供上传）。
// 流程对齐 FetchV2SdkZip：查 tebox_sdk 最新版本 → download-url → 下载。
// 返回 zip 本地路径（调用方负责删除）与版本号。
func FetchTeboxSdkZip(progress teboxSdkProgress) (string, string, error) {
	if progress == nil {
		progress = func(string, string) {}
	}
	progress("prepare", "查询 tebox SDK 最新版本")
	version, err := sdkver.FetchLatestVersionOfType(teboxSdkType)
	if err != nil {
		return "", "", fmt.Errorf("查询 tebox_sdk 最新版本失败: %w", err)
	}
	if strings.ContainsAny(version, "/\\?&#% \t\r\n\"'") {
		return "", "", fmt.Errorf("SDK 版本号含非法字符: %q", version)
	}
	log.Printf("[teboxsdk] tebox_sdk 最新版本: v%s", version)

	url := fmt.Sprintf("%s/sdk/download-url?sdk_type=%s&version=%s&filename=%s",
		teboxSdkAPIBase, teboxSdkType, version, teboxSdkFilename)
	progress("prepare", "获取下载地址")
	dlURL, err := fetchV2SdkDownloadURL(url) // 复用 v2 的 download-url 解析（响应结构同构）
	if err != nil {
		return "", "", err
	}

	progress("prepare", "下载 SDK 包（约 8MB）")
	zipPath := tempFilePath("tebox-sdk", version, ".zip")
	if err := downloadFile(dlURL, zipPath); err != nil {
		osRemove(zipPath)
		return "", "", fmt.Errorf("下载 SDK 包失败: %w", err)
	}
	return zipPath, version, nil
}

// InstallTeboxSdk 对一台 V3 ARM 设备安装 tobox_player SDK（完整链路，阻塞执行）。
//
// 步骤：SSH 连接 → 拉取 dobox 云机镜像（tebox 基座容器用，本地已有则跳过）→
// 确保基座镜像 tebox:1.0.0 存在（本地没有则从 mytos/dobox 仓库 pull 后 tag，
// 已有则跳过）→
// 上传 install-sdk.sh + tebox-sdk.zip → 以 TOBOX_SUDO_PASS 执行脚本 →
// 执行 start-tebox-hw.sh 启动基座容器 → 轮询 /health + 50051 gRPC 就绪 →
// 清理设备端临时文件。
// 幂等：脚本自身幂等（已装且健康则跳过；版本防降级）；重复安装安全。
func InstallTeboxSdk(deviceIP string, zipPath string, progress teboxSdkProgress) error {
	if progress == nil {
		progress = func(string, string) {}
	}
	ip := extractPureIP(deviceIP)
	log.Printf("[teboxsdk] 开始安装 tobox_player SDK: %s", ip)

	progress("prepare", "连接设备")
	sshClient, err := dialSSH(ip)
	if err != nil {
		return fmt.Errorf("SSH 连接失败: %w", err)
	}
	defer sshClient.Close()
	sftpClient, err := sftp.NewClient(sshClient)
	if err != nil {
		return fmt.Errorf("SFTP 连接失败: %w", err)
	}
	defer sftpClient.Close()

	// 拉取 dobox 云机镜像（tebox 基座容器启动依赖，脚本无 docker pull 所以
	// 安装流程前置补上）。失败按常规步骤处理：中断安装并报错（设备外网不可达
	// 时 SDK 本体安装大概率也会失败）。
	progress("pull", "准备拉取云机镜像")
	if err := pullTeboxDoboxImage(sshClient, progress); err != nil {
		return fmt.Errorf("镜像拉取失败: %w", err)
	}

	// load 基座镜像 tebox:1.0.0（本地没有则从 mytos/dobox 仓库 pull 后 tag，
	// 已有则跳过——tebox:1.0.0 = dobox:tebox_v1.0.0 同一镜像，见 teboxsdk_base.go
	// 头注释）。pull 失败报错说明设备外网与 registry 仓库可用性检查方向。
	progress("load", "准备基座镜像")
	if err := loadTeboxBaseImage(sshClient, progress); err != nil {
		return fmt.Errorf("基座镜像加载失败: %w", err)
	}

	// 上传安装脚本（zip 非空才一并上传：zip=="" 表示让脚本自己走 newapi 取包）
	progress("upload", "上传安装脚本")
	if err := sftpWriteBytes(sftpClient, teboxInstallScript, teboxRemoteScript, 0755); err != nil {
		return fmt.Errorf("上传安装脚本失败: %w", err)
	}
	if zipPath != "" {
		progress("upload", "上传 SDK 安装包")
		if err := sftpUploadFile(sftpClient, zipPath, teboxRemoteZip, 0644); err != nil {
			return fmt.Errorf("上传 SDK 安装包失败: %w", err)
		}
	}

	// 执行安装：TOBOX_SUDO_PASS 无人值守提权（脚本内部非 root 时用它 re-exec sudo；
	// 免密 sudo 的设备 TOBOX_SUDO_PASS 不生效也无妨——脚本走免密分支）。
	// TOBOX_SUDO_PASS 用设备 SSH 密码（V3 设备 user/myt，与 v2sdk 的 SUDO_PASS 语义一致）。
	// 输出流式回调进度（脚本带 [N/8] 步骤标记，逐行透传）。
	progress("install", "执行安装脚本")
	cmd := fmt.Sprintf("TOBOX_SUDO_PASS='%s' bash %s --zip %s 2>&1", sshPassword, teboxRemoteScript, teboxRemoteZip)
	if zipPath == "" {
		cmd = fmt.Sprintf("TOBOX_SUDO_PASS='%s' bash %s 2>&1", sshPassword, teboxRemoteScript)
	}
	var lastLine string
	err = sshExecStreaming(sshClient, cmd, teboxInstallOverallTimeout, func(line string) {
		lastLine = line
		progress("install", line)
	})
	// 清理设备端临时文件（失败不阻断——/tmp 重启即清）
	runSSHCmd(sshClient, fmt.Sprintf("rm -f %s %s", teboxRemoteScript, teboxRemoteZip))
	if err != nil {
		detail := truncateForLog(lastLine, 200)
		if detail != "" {
			return fmt.Errorf("%w（最后输出: %s）", err, detail)
		}
		return err
	}

	// 启动基座容器（start-tebox-hw.sh 上传后提权执行——install-sdk.sh 只装
	// tobox_player 本体，基座容器 tebox:1.0.0 由脚本负责；先基座后 player，
	// SDK 的 CreateInstance 依赖基座 gRPC 50051）。失败中断安装并报错。
	progress("base", "准备启动基座容器")
	if err := runTeboxBase(sftpClient, sshClient, progress); err != nil {
		return fmt.Errorf("基座容器启动失败: %w", err)
	}

	// 健康验证：脚本 [8/8] 已在设备内验证过 /health，这里从客户端侧再确认一遍
	// （8088 对外监听——设备内 curl 通过而端口未绑 0.0.0.0 的场景兜底）；
	// /health 就绪后再加验基座容器 gRPC 50051 TCP 连通（基座起得慢，给 60s
	// 重试窗口，与 /health 轮询上限对齐）。
	progress("verify", "验证服务")
	deadline := time.Now().Add(teboxHealthPollTimeout)
	healthOK := false
	for time.Now().Before(deadline) {
		if CheckTeboxSdkHealth(ip) {
			healthOK = true
			break
		}
		time.Sleep(time.Second)
	}
	if !healthOK {
		return fmt.Errorf("安装脚本执行成功但 %ds 内 8088 /health 仍未就绪（详见设备 %s/sdk.log）",
			int(teboxHealthPollTimeout.Seconds()), "/home/user/tobox_player")
	}
	progress("verify", "验证基座 gRPC（50051）")
	if err := pollTeboxGRPC(ip, teboxGRPCPollTimeout, progress); err != nil {
		return err
	}
	progress("verify", "tobox_player SDK（8088）与基座 gRPC（50051）均已就绪")
	log.Printf("[teboxsdk] %s 安装成功（/health + 50051 就绪）", ip)
	return nil
}

// InstallTeboxSdkFromCloud 一站式入口：从 newapi 取包 + 安装（zip 来源默认链路）。
// 任务队列 single handler / 右键菜单直调均走这里。
func InstallTeboxSdkFromCloud(deviceIP string, progress teboxSdkProgress) error {
	if progress == nil {
		progress = func(string, string) {}
	}
	zipPath, version, err := FetchTeboxSdkZip(progress)
	if err != nil {
		return err
	}
	defer osRemove(zipPath)
	log.Printf("[teboxsdk] %s 使用云端包 tebox-sdk v%s", extractPureIP(deviceIP), version)
	return InstallTeboxSdk(deviceIP, zipPath, progress)
}
