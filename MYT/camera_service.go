//go:build windows

package main

// 摄像头相关服务函数（Windows）。
//
// 历史背景：摄像头推流曾由外挂 WindowsPusher.exe 承担（本文件曾嵌入其二进制并
// 管理推流进程）；2026-08-28 推流整体迁移进 go_legacy 播放器（自研 ffmpeg 推流，
// 见 myt-client-player/camera_push_encoder.go），WindowsPusher 退役。
// 本文件现仅保留：
//   - ensureFFmpegExe：提取精简 ffmpeg（camdemo/ffmpeg_minimal/bin go:embed 源），
//     供投屏窗口的摄像头/麦克风推流使用（screencast_service*.go 调用）
//   - listCameraDevices：ffmpeg dshow 枚举（流媒体 Tab 的摄像头下拉框）
// 流媒体 Tab 的旧推流入口（StartWindowsPusher 等）转为优雅下线提示，保住前端兼容。

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

var (
	ffmpegExeOnce sync.Once
	ffmpegExePath string
	ffmpegExeErr  error
)

// ensureFFmpegExe 确保 ffmpeg.exe 已提取（投屏窗口摄像头/麦克风推流用）
func ensureFFmpegExe() (string, error) {
	ffmpegExeOnce.Do(func() {
		dir := filepath.Join(os.TempDir(), "myt-ffmpeg")
		if err := os.MkdirAll(dir, 0755); err != nil {
			ffmpegExeErr = fmt.Errorf("cannot create ffmpeg temp dir: %w", err)
			return
		}
		if err := extractFfmpegAssets(dir); err != nil {
			ffmpegExeErr = err
			return
		}
		ffmpegExePath = filepath.Join(dir, "ffmpeg.exe")
		fmt.Printf("[stream-cam] ffmpeg extracted to %s", ffmpegExePath)
	})
	return ffmpegExePath, ffmpegExeErr
}

// cameraPushMigratedMsg 统一的旧推流入口下线提示
const cameraPushMigratedMsg = "摄像头推流已迁移至投屏窗口（播放器内置），此入口已下线"

// startWindowsPusher 旧外挂推流入口：WindowsPusher 已随推流重构退役
func startWindowsPusher(args []string) map[string]interface{} {
	return map[string]interface{}{"success": false, "message": cameraPushMigratedMsg}
}

// stopWindowsPusher 旧入口：无活动会话，直接成功
func stopWindowsPusher(id int) map[string]interface{} {
	return map[string]interface{}{"success": true, "message": "会话不存在"}
}

// stopAllWindowsPushers 旧入口：无活动会话，no-op
func stopAllWindowsPushers() {}

// capturePreviewFrame 旧预览抓帧入口：依赖已退役的 WindowsPusher --capture-preview。
// 如需恢复，先给精简 ffmpeg 加 mjpeg 编码器 + image2 muxer（build_ffmpeg.sh）再用
// ffmpeg dshow 抓帧实现。
func capturePreviewFrame(camName string, width, height int) map[string]interface{} {
	return map[string]interface{}{"success": false, "message": cameraPushMigratedMsg}
}

// listCameraDevices 列出可用摄像头设备（ffmpeg dshow 枚举）
func listCameraDevices() map[string]interface{} {
	exePath, err := ensureFFmpegExe()
	if err != nil {
		return map[string]interface{}{"success": false, "message": err.Error(), "cameras": []string{}}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, exePath, "-list_devices", "true", "-f", "dshow", "-i", "dummy")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	_ = cmd.Run()
	cameras := []string{}
	for _, line := range strings.Split(stderr.String(), "\n") {
		idx := strings.Index(line, "\"")
		if idx < 0 {
			continue
		}
		rest := line[idx+1:]
		end := strings.Index(rest, "\"")
		if end < 0 {
			continue
		}
		name := rest[:end]
		if strings.Contains(line, "(video)") && name != "" && !strings.Contains(strings.ToLower(name), "audio") {
			cameras = append(cameras, name)
		}
	}
	return map[string]interface{}{"success": true, "cameras": cameras}
}
