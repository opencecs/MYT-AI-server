package main

import (
	"context"
	"fmt"
	"time"
)

// --- V2 云机交互式终端（ADB 直连容器 adb 端口）---
//
// 背景：V2 体系的 81 /and_api/v1/shell 是一次性 HTTP exec（命令结束才返回输出），
// logcat / top 这类流式命令会被挂住。容器的 adb 端口（容器 5555 → 主机 5000+index，
// dc list 的 adb 字段）支持原生 ADB 协议——实测握手免认证、root 权限、支持 shell_v2
// （stdout/stderr 分离 + 退出码 + 窗口大小），据此提供真交互式终端。
//
// 会话管理收敛到共享 terminalHub（terminal_hub.go，V4 同用），本文件只保留
// V2 特有的「解析设备 → 查容器 ADB 端口」逻辑；事件名为通用的 adbterm:data /
// adbterm:exit（原 v2term:* 已废弃，前端 useTerminalSession.ts 同步改）。

// OpenV2Terminal 打开云机容器的交互式终端，返回会话 ID。
// 输出经 adbterm:data 事件推送，退出经 adbterm:exit。
func (s *V2ServiceWrapper) OpenV2Terminal(deviceID, containerName string, cols, rows int) (string, error) {
	dev, err := s.resolveDevice(deviceID)
	if err != nil {
		return "", err
	}
	if containerName == "" {
		return "", fmt.Errorf("容器名为空")
	}
	port, err := s.adbPortOf(dev.IP, containerName)
	if err != nil {
		return "", err
	}
	return termHub.OpenADB(fmt.Sprintf("%s:%d", dev.IP, port), cols, rows)
}

// WriteV2Terminal 发送终端输入（xterm 按键数据）
func (s *V2ServiceWrapper) WriteV2Terminal(sessionID, data string) error {
	return termHub.Write(sessionID, data)
}

// ResizeV2Terminal 同步终端窗口大小（PTY 尺寸，shell_v2 生效）
func (s *V2ServiceWrapper) ResizeV2Terminal(sessionID string, cols, rows int) error {
	return termHub.Resize(sessionID, cols, rows)
}

// CloseV2Terminal 关闭终端会话
func (s *V2ServiceWrapper) CloseV2Terminal(sessionID string) error {
	return termHub.Close(sessionID)
}

// adbPortOf 查容器 ADB 映射端口：81 dc list 的 adb 字段（= 主机 5000+index）。
// 走实时查询而非本地缓存——容器重建后端口会变，缓存可能过期。
func (s *V2ServiceWrapper) adbPortOf(ip, containerName string) (int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	entries, err := s.client.ListContainersViaSDK(ctx, ip)
	if err != nil {
		return 0, fmt.Errorf("查询容器列表失败: %w", err)
	}
	for _, e := range entries {
		if e.Name != containerName {
			continue
		}
		if e.Adb <= 0 {
			return 0, fmt.Errorf("容器 %s 未映射 ADB 端口（无法建立交互式终端）", containerName)
		}
		return e.Adb, nil
	}
	return 0, fmt.Errorf("未找到容器 %s", containerName)
}
