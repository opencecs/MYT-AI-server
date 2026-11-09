package maintenance

// tebox 安装流程的容器镜像拉取步骤。
//
// tebox 基座容器由 start-tebox-hw.sh 式命令启动（docker run -d --network host
// --device ... -v /mmc/tebox/... "$IMAGE"），脚本自身不含 docker pull——镜像
// 必须预先存在于设备。tebox 安装流程（InstallTeboxSdk）在执行 install-sdk.sh
// 之前先在这里拉取 dobox 云机镜像，保证后续 CreateInstance 有镜像可用；
// 基座镜像 tebox:1.0.0 同样由流程保证（teboxsdk_base.go：pull dobox 仓库的
// tebox_v<ver> tag 后 docker tag 成 tebox:1.0.0）。
//
// 拉取用 docker 原生 registry 地址（阿里云广州），设备侧只依赖 docker 守护
// 进程与外网连通；本地已有同版本镜像时跳过（幂等，重复安装不重复下载）。

import (
	"fmt"
	"log"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

const (
	// teboxDoboxImage tebox 云机容器镜像（docker pull 目标）
	teboxDoboxImage = "registry.cn-guangzhou.aliyuncs.com/mytos/dobox:tebox_v1.0.0"
	// teboxImagePullTimeout 镜像拉取超时：镜像数百 MB 级，设备外网带宽不可控，
	// 单独放宽到 10 分钟（任务队列整体兜底相应放宽到 20min 覆盖）。
	teboxImagePullTimeout = 10 * time.Minute
)

// pullTeboxDoboxImage SSH 到设备拉取 tebox 云机镜像（阻塞执行）。
// 本地已有同版本镜像（docker image inspect 成功）则跳过；pull 输出逐行经
// progress("pull", ...) 透传（与 install 步骤同型，任务面板实时可见）。
// 失败由调用方按常规步骤处理（中断安装流程）。
//
// docker 守护进程 socket 仅 root 可访问：SSH 登录用户是非 root（user/myt），
// 与同包 myt_panel/tunnel 一律走 `echo 密码 | sudo -S` 模式（install 步骤是
// install-sdk.sh 内部用 TOBOX_SUDO_PASS 自行提权）。没有 sudo 前缀时 docker
// CLI 报 permission denied 直接 exit 1——这正是「手动 root pull 成功、任务
// 队列失败」的根因。
func pullTeboxDoboxImage(sshClient *ssh.Client, progress teboxSdkProgress) error {
	progress("pull", "检查本地镜像")
	out, err := runSSHCmd(sshClient, fmt.Sprintf("echo '%s' | sudo -S docker image inspect %s >/dev/null 2>&1 && echo yes || echo no", sshPassword, teboxDoboxImage))
	if err != nil {
		return fmt.Errorf("检查本地镜像失败: %w", err)
	}
	if strings.Contains(out, "yes") {
		progress("pull", "本地镜像已存在，跳过拉取")
		log.Printf("[teboxsdk] 本地已有 %s，跳过 pull", teboxDoboxImage)
		return nil
	}

	progress("pull", fmt.Sprintf("拉取镜像 %s（可能需要数分钟）", teboxDoboxImage))
	log.Printf("[teboxsdk] 开始拉取镜像: %s", teboxDoboxImage)
	cmd := fmt.Sprintf("echo '%s' | sudo -S docker pull %s 2>&1", sshPassword, teboxDoboxImage)
	var lastLine string
	err = sshExecStreaming(sshClient, cmd, teboxImagePullTimeout, func(line string) {
		lastLine = line
		progress("pull", line)
	})
	if err != nil {
		// exit 1 时 docker 的真实报错（permission denied/manifest unknown 等）
		// 在 stderr 流里，带上最后输出行（与 install 步骤错误格式一致）。
		if detail := truncateForLog(lastLine, 200); detail != "" {
			return fmt.Errorf("拉取镜像 %s 失败: %w（最后输出: %s）", teboxDoboxImage, err, detail)
		}
		return fmt.Errorf("拉取镜像 %s 失败: %w", teboxDoboxImage, err)
	}
	progress("pull", "镜像拉取完成")
	log.Printf("[teboxsdk] 镜像拉取完成: %s", teboxDoboxImage)
	return nil
}
