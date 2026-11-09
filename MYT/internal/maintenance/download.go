package maintenance

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

func downloadFile(url, filePath string) error {
	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("下载失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("下载失败，HTTP状态码: %d", resp.StatusCode)
	}

	f, err := os.Create(filePath)
	if err != nil {
		return fmt.Errorf("创建文件失败: %w", err)
	}
	defer f.Close()

	written, err := io.Copy(f, resp.Body)
	if err != nil {
		return fmt.Errorf("写入文件失败: %w", err)
	}
	log.Printf("[维护] 下载完成，大小: %d bytes", written)
	return nil
}

// generateAlpineOpenRC 生成 OpenRC 服务文件（command/command_args 按面板形态）。
// 服务注册时按固定服务名 myt-panel 落到 /etc/init.d/myt-panel（形态切换 =
// 重装，旧服务文件被覆盖，无需按形态命名）。
func generateAlpineOpenRC(dir string, form PanelForm) {
	content := fmt.Sprintf(`#!/sbin/openrc-run
name="myt-panel"
description="MYT Cloud Phone Panel"

command=%q
command_args=%q
command_user="root"
command_background=true
pidfile="/run/${RC_SVCNAME}.pid"
directory="/home/user"

output_log="/home/user/logs/myt-panel.log"
error_log="/home/user/logs/myt-panel.log"

respawn_delay=3
respawn_max=0

depend() {
    need net
    after firewall
}

start_pre() {
    mkdir -p /home/user/logs
}
`, form.RemotePath(), form.Args)
	os.WriteFile(filepath.Join(dir, "alpine-openrc"), []byte(content), 0755)
}

// generateDebianSystemd 生成 systemd 服务文件（ExecStart 按面板形态）。
func generateDebianSystemd(dir string, form PanelForm) {
	content := fmt.Sprintf(`[Unit]
Description=MYT Cloud Phone Panel
After=network.target

[Service]
Type=simple
User=root
WorkingDirectory=/home/user
ExecStart=%s

Restart=always
RestartSec=3
LimitNOFILE=65535

[Install]
WantedBy=multi-user.target
`, form.CommandLine())
	os.WriteFile(filepath.Join(dir, "debian-systemd.service"), []byte(content), 0644)
}
