package maintenance

// tebox 安装流程的基座容器两步：pull 基座镜像 + run 基座容器。
//
// 基座镜像统一从 mytos/dobox 仓库拉取（tebox:1.0.0 = dobox:tebox_v1.0.0
// 同一镜像，同 image ID）：本地无 tebox:1.0.0 时 pull dobox 仓库镜像后
// docker tag 成 tebox:1.0.0，不依赖设备本地任何镜像包文件。
//
// run 步骤按上游 start-tebox-hw.sh（go:embed 原样上传后执行）：容器已存在
// 也按脚本行为 rm -f 删掉重建（不改脚本逻辑）。脚本内 docker 无 sudo 前缀
// ——经 `echo 密码 | sudo -S bash <脚本>` 整体提权执行。

import (
	_ "embed"
	"fmt"
	"log"
	"net"
	"strings"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

const (
	// teboxVersion tebox 基座版本：tebox:1.0.0 ↔ dobox:tebox_v1.0.0 对应
	// （未来升版时两个镜像名都由此拼接，改这一处即可）。
	teboxVersion = "1.0.0"
	// teboxBaseImage 基座容器镜像名（start-tebox-hw.sh 的 IMAGE 默认值）
	teboxBaseImage = "tebox:" + teboxVersion
	// teboxBaseSourceImage 基座镜像的仓库来源（mytos/dobox 仓库，pull 后
	// tag 成 teboxBaseImage；tebox_v<ver> 为该仓库对 tebox 版本的 tag 约定）
	teboxBaseSourceImage = "registry.cn-guangzhou.aliyuncs.com/mytos/dobox:tebox_v" + teboxVersion
	// teboxPullTimeout 基座镜像 pull 超时：镜像数百 MB 级，设备外网带宽不可控，
	// 放宽到 10 分钟（与 dobox 镜像拉取同窗，任务队列整体兜底 20min 覆盖）。
	teboxPullTimeout = 10 * time.Minute
	// teboxRunTimeout 基座容器启动超时（docker run 秒级，2min 纯兜底）
	teboxRunTimeout = 2 * time.Minute
	// teboxGRPCPort 基座容器 gRPC 端口（TEBOX_GRPC_ADDR=0.0.0.0:50051）
	teboxGRPCPort = 50051
)

//go:embed scripts/start-tebox-hw.sh
var teboxBaseScript []byte

// teboxBaseScriptRemote 基座容器脚本上传到设备的路径（/tmp，重启即清）
const teboxBaseScriptRemote = "/tmp/start-tebox-hw.sh"

// loadTeboxBaseImage 确保设备上存在基座镜像 tebox:1.0.0（阻塞执行）。
//   - 镜像已存在 → 跳过（幂等，不覆盖已有镜像）
//   - 不存在 → 从 mytos/dobox 仓库 pull（tebox:1.0.0 = dobox:tebox_v1.0.0
//     同一镜像）后 docker tag 成 tebox:1.0.0
//   - pull 失败 → 报错（设备外网与 registry 仓库可用性）
//   - pull 输出逐行经 progress("load", ...) 透传（与 pull 步骤同型）
func loadTeboxBaseImage(sshClient *ssh.Client, progress teboxSdkProgress) error {
	progress("load", "检查基座镜像")
	out, err := runSSHCmd(sshClient, fmt.Sprintf("echo '%s' | sudo -S docker image inspect %s >/dev/null 2>&1 && echo yes || echo no", sshPassword, teboxBaseImage))
	if err != nil {
		return fmt.Errorf("检查基座镜像失败: %w", err)
	}
	if strings.Contains(out, "yes") {
		progress("load", "基座镜像已存在，跳过拉取")
		log.Printf("[teboxsdk] 本地已有 %s，跳过基座镜像拉取", teboxBaseImage)
		return nil
	}

	progress("load", fmt.Sprintf("拉取基座镜像 %s（可能需要数分钟）", teboxBaseSourceImage))
	log.Printf("[teboxsdk] 开始拉取基座镜像: %s", teboxBaseSourceImage)
	cmd := fmt.Sprintf("echo '%s' | sudo -S docker pull %s 2>&1", sshPassword, teboxBaseSourceImage)
	var lastLine string
	err = sshExecStreaming(sshClient, cmd, teboxPullTimeout, func(line string) {
		lastLine = line
		progress("load", line)
	})
	if err != nil {
		// exit 1 时 docker 的真实报错（permission denied/manifest unknown 等）
		// 在 stderr 流里，带上最后输出行（与 pull 步骤错误格式一致）。
		detail := truncateForLog(lastLine, 200)
		if detail != "" {
			return fmt.Errorf("基座镜像拉取失败，请检查设备外网与 registry 仓库可用性（拉取 %s）: %w（最后输出: %s）",
				teboxBaseSourceImage, err, detail)
		}
		return fmt.Errorf("基座镜像拉取失败，请检查设备外网与 registry 仓库可用性（拉取 %s）: %w",
			teboxBaseSourceImage, err)
	}

	progress("load", fmt.Sprintf("标记镜像为 %s", teboxBaseImage))
	if _, err := runSSHCmd(sshClient, fmt.Sprintf("echo '%s' | sudo -S docker tag %s %s", sshPassword, teboxBaseSourceImage, teboxBaseImage)); err != nil {
		return fmt.Errorf("标记基座镜像 %s 为 %s 失败: %w", teboxBaseSourceImage, teboxBaseImage, err)
	}
	progress("load", "基座镜像就绪")
	log.Printf("[teboxsdk] 基座镜像就绪: %s（来自 %s）", teboxBaseImage, teboxBaseSourceImage)
	return nil
}

// runTeboxBase 容器启动与 gRPC 就绪探测：先按脚本重启容器，再等 50051 可连。

// teboxGRPCPollTimeout 50051 gRPC 就绪轮询上限：与 /health 轮询（60s）对齐
// （基座容器冷启动需加载运行时，给同等重试窗口）。
const teboxGRPCPollTimeout = 60 * time.Second

// runTeboxBase 启动 tebox 基座容器（阻塞执行）：上传 start-tebox-hw.sh →
// `echo 密码 | sudo -S bash <脚本>` 提权执行（脚本 docker rm -f 先行——容器
// 已存在也删掉重建，按脚本原样不改逻辑）→ 输出逐行透传。
func runTeboxBase(sftpClient *sftp.Client, sshClient *ssh.Client, progress teboxSdkProgress) error {
	progress("base", "上传基座启动脚本")
	if err := sftpWriteBytes(sftpClient, teboxBaseScript, teboxBaseScriptRemote, 0755); err != nil {
		return fmt.Errorf("上传基座启动脚本失败: %w", err)
	}
	progress("base", "启动基座容器")
	log.Printf("[teboxsdk] 启动基座容器（%s）", teboxBaseScriptRemote)
	cmd := fmt.Sprintf("echo '%s' | sudo -S bash %s 2>&1", sshPassword, teboxBaseScriptRemote)
	var lastLine string
	err := sshExecStreaming(sshClient, cmd, teboxRunTimeout, func(line string) {
		lastLine = line
		progress("base", line)
	})
	// 清理设备端临时脚本（失败不阻断——/tmp 重启即清）
	runSSHCmd(sshClient, fmt.Sprintf("rm -f %s", teboxBaseScriptRemote))
	if err != nil {
		if detail := truncateForLog(lastLine, 200); detail != "" {
			return fmt.Errorf("启动基座容器失败: %w（最后输出: %s）", err, detail)
		}
		return fmt.Errorf("启动基座容器失败: %w", err)
	}
	progress("base", "基座容器已启动")
	return nil
}

// CheckTeboxGRPC 探测设备基座容器 gRPC 端口 50051 是否可连（TCP 层即可）。
func CheckTeboxGRPC(deviceIP string) bool {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("%s:%d", extractPureIP(deviceIP), teboxGRPCPort), 2*time.Second)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// pollTeboxGRPC 轮询设备 50051 gRPC 就绪（基座容器起得慢，给重试窗口）。
func pollTeboxGRPC(deviceIP string, timeout time.Duration, progress teboxSdkProgress) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if CheckTeboxGRPC(deviceIP) {
			return nil
		}
		time.Sleep(time.Second)
	}
	return fmt.Errorf("%ds 内基座容器 gRPC 端口 %d 仍未就绪（详见设备 %s/sdk.log）",
		int(timeout.Seconds()), teboxGRPCPort, "/home/user/tobox_player")
}
