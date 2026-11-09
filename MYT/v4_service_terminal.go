package main

import (
	"fmt"
)

// --- V4 实例交互式终端（SDK 官方 WS 终端接口）---
//
// tobox SDK 提供 `GET ws://<host>:8088/android/<实例id>/terminal`：exec 一个
// TTY shell，stdout 桥接为 WS 二进制帧下发，文本帧上行写为 raw stdin（实测
// 2026-10-22 tobox v3 不解析 JSON stdin），resize 走 JSON 控制帧。`target=`
// 查询参数选 shell 类型：target=android 直接进安卓宿主 shell（uid=2000
// shell，提示符 `PD2266:/ $`，等价 adb shell；实测 getprop 可用）；缺省为
// 容器内 busybox shell（`~ #`，uid=0）。有 Basic Auth（服务端 auth 关闭时
// 忽略）与服务端 exec 生命周期管理，比直连 adbd（原 ADB 方案，adbterm 通道
// 遇 AUTH 包即报错且绕过 8088 统一入口）更符合 SDK 设计，故 V4 终端已从
// ADB 直连整体切换到本方案（原 adb_port 查询逻辑已删除；adbterm 包保留给
// V2 容器终端）。
//
// 当前 V4 终端入口统一走安卓 shell（target=android，adb shell 体验）；容器
// shell 未开放入口，后续如需可加二级菜单（Manager.Open/Dial 已支持 target
// 传空串）。
//
// 会话管理复用共享 terminalHub（terminal_hub.go，会话 ID 前缀 v4t），本文件
// 只保留 V4 特有的「模式校验 → 反查实例 → 拼 WS URL」逻辑。

// v4TermTargetAndroid V4 终端 target 参数：直接进安卓宿主 shell
// （等价 adb shell 体验；传空串则为 SDK 默认容器 shell）
const v4TermTargetAndroid = "android"

// OpenV4Terminal 打开 V4 实例的交互式终端，返回会话 ID。
// hostIP 为宿主机 IP（前端经 getDeviceId 取——V4 以 IP 为设备标识），
// instanceName 为实例名/联合主键（与启停等操作一致，v4ResolveRef 反查实例
// 拿 REST 主键 id——WS 路径参数即实例 id，backend 无独立路径参数，默认
// 后端语义由服务端承担）。
// 默认 target=android：直接进安卓宿主 shell（等价 adb shell 体验）。
// 输出经 adbterm:data 事件推送，退出经 adbterm:exit（与 V2 终端共用前端通道）。
func (s *V4ServiceWrapper) OpenV4Terminal(hostIP, instanceName string, cols, rows int) (string, error) {
	if s.configManager.GetDeployMode() != "v4" {
		return "", fmt.Errorf("当前非 tebox 部署模式")
	}
	hostIP = v4HostIP(hostIP)
	if hostIP == "" {
		return "", fmt.Errorf("主机 IP 为空")
	}
	if instanceName == "" {
		return "", fmt.Errorf("实例名为空")
	}
	ref, err := v4ResolveRef(s.configManager, hostIP, instanceName)
	if err != nil {
		return "", err
	}
	url := fmt.Sprintf("ws://%s:8088/android/%s/terminal", hostIP, ref.ID)
	user, password := v4Creds()
	return termHub.OpenV4(url, v4TermTargetAndroid, user, password, cols, rows)
}

// WriteV4Terminal 发送终端输入（xterm 按键数据）
func (s *V4ServiceWrapper) WriteV4Terminal(sessionID, data string) error {
	return termHub.Write(sessionID, data)
}

// ResizeV4Terminal 同步终端窗口大小（JSON 控制帧调整容器内 TTY 尺寸）
func (s *V4ServiceWrapper) ResizeV4Terminal(sessionID string, cols, rows int) error {
	return termHub.Resize(sessionID, cols, rows)
}

// CloseV4Terminal 关闭终端会话（服务端收到 close 即 Kill exec）
func (s *V4ServiceWrapper) CloseV4Terminal(sessionID string) error {
	return termHub.Close(sessionID)
}
