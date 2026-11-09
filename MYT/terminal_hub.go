package main

import (
	"encoding/base64"
	"fmt"
	"sync"

	"github.com/moyunteng/myt/internal/adbterm"
	"github.com/moyunteng/myt/internal/v4term"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// --- 共享终端会话中心（V2 ADB / V4 SDK WS 通用）---
//
// V2 云机容器的交互式终端（v2_service_terminal.go，ADB 协议直连容器 adb
// 端口）与 V4 实例的交互式终端（v4_service_terminal.go，SDK 官方 WS 终端
// /android/:id/terminal）在会话管理上同构：打开会话 → 按 sessionID 写入 /
// 同步窗口大小 / 关闭，输出与退出经 Wails 事件推给前端（事件名沿用 v2 时代
// 语义的通用前缀 adbterm:data / adbterm:exit，前端 useTerminalSession.ts
// 统一监听）。
//
// 两种会话类型经 terminalSession 接口收敛差异，hub 不感知协议细节；
// 会话 ID 前缀区分归属（v2t = ADB / v4t = WS），按前缀路由操作。

// adbTermDataEvent 终端输出事件（前端按 base64 还原原始字节写入 xterm）
type adbTermDataEvent struct {
	SessionID string `json:"sessionId"`
	Stream    int    `json:"stream"` // 1=stdout 2=stderr
	Data      string `json:"data"`   // base64
}

// adbTermExitEvent 终端退出事件
type adbTermExitEvent struct {
	SessionID string `json:"sessionId"`
	Code      int    `json:"code"`
}

// terminalSession 终端会话接口（ADB 与 SDK WS 两种实现的共同操作集）
type terminalSession interface {
	Write(data []byte) error
	Resize(cols, rows int) error
	Close() error
}

// terminalHub 终端会话中心（懒初始化各会话管理器——app 在 setup 后段才注入）。
// 包级单例：V2/V4 两个 service 同属 main 包且共用唯一 Wails app，无多实例场景。
type terminalHub struct {
	app     *application.App
	mu      sync.Mutex
	adbMgr  *adbterm.Manager // V2：ADB 协议会话（ID 前缀 v2t）
	v4wsMgr *v4term.Manager  // V4：SDK WS 终端会话（ID 前缀 v4t）
}

var termHub = &terminalHub{}

// SetApp 注入 Wails app（setup.go setApp 调用）
func (h *terminalHub) SetApp(app *application.App) { h.app = app }

// emitData 推送输出事件（两种会话共用）
func (h *terminalHub) emitData(sessionID string, stream int, data []byte) {
	if h.app == nil {
		return
	}
	h.app.Event.Emit("adbterm:data", adbTermDataEvent{
		SessionID: sessionID,
		Stream:    stream,
		Data:      base64.StdEncoding.EncodeToString(data),
	})
}

// emitExit 推送退出事件（两种会话共用）
func (h *terminalHub) emitExit(sessionID string, code int) {
	if h.app == nil {
		return
	}
	h.app.Event.Emit("adbterm:exit", adbTermExitEvent{SessionID: sessionID, Code: code})
}

// ensureADBManager 取 ADB 会话管理器（首次调用时创建）
func (h *terminalHub) ensureADBManager() *adbterm.Manager {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.adbMgr == nil {
		h.adbMgr = adbterm.NewManager(
			func(sessionID string, stream byte, data []byte) {
				h.emitData(sessionID, int(stream), data)
			},
			func(sessionID string, code int) { h.emitExit(sessionID, code) },
		)
	}
	return h.adbMgr
}

// ensureV4Manager 取 V4 WS 会话管理器（首次调用时创建）
func (h *terminalHub) ensureV4Manager() *v4term.Manager {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.v4wsMgr == nil {
		h.v4wsMgr = v4term.NewManager(
			func(sessionID string, data []byte) { h.emitData(sessionID, 1, data) },
			func(sessionID string, code int) { h.emitExit(sessionID, code) },
		)
	}
	return h.v4wsMgr
}

// OpenADB 建立 ADB shell 会话（V2），返回会话 ID
func (h *terminalHub) OpenADB(addr string, cols, rows int) (string, error) {
	return h.ensureADBManager().Open(addr, cols, rows)
}

// OpenV4 建立 SDK WS 终端会话（V4），返回会话 ID。
// url 形如 ws://host:8088/android/<实例id>/terminal；target 透传
// v4term.Manager.Open（"android" = 安卓 shell，空串 = 容器 shell）。
func (h *terminalHub) OpenV4(url, target, user, password string, cols, rows int) (string, error) {
	if cols <= 0 {
		cols = 80
	}
	if rows <= 0 {
		rows = 24
	}
	return h.ensureV4Manager().Open(url, target, user, password, cols, rows)
}

// isV4Session 按 ID 前缀判断会话类型（v4t = SDK WS / 其余 = ADB）
func isV4Session(sessionID string) bool {
	return len(sessionID) > 3 && sessionID[:3] == "v4t"
}

// Write 发送终端输入（xterm 按键数据）
func (h *terminalHub) Write(sessionID, data string) error {
	if isV4Session(sessionID) {
		mgr := h.v4MgrIfAny()
		if mgr == nil {
			return fmt.Errorf("终端会话不存在或已关闭")
		}
		return mgr.Write(sessionID, []byte(data))
	}
	mgr := h.adbMgrIfAny()
	if mgr == nil {
		return fmt.Errorf("终端会话不存在或已关闭")
	}
	return mgr.Write(sessionID, []byte(data))
}

// Resize 同步终端窗口大小（PTY 尺寸；会话已不存在静默忽略，与原行为一致）
func (h *terminalHub) Resize(sessionID string, cols, rows int) error {
	if isV4Session(sessionID) {
		mgr := h.v4MgrIfAny()
		if mgr == nil {
			return nil
		}
		return mgr.Resize(sessionID, cols, rows)
	}
	mgr := h.adbMgrIfAny()
	if mgr == nil {
		return nil
	}
	return mgr.Resize(sessionID, cols, rows)
}

// Close 关闭终端会话（会话已不存在静默忽略，与原行为一致）
func (h *terminalHub) Close(sessionID string) error {
	if isV4Session(sessionID) {
		mgr := h.v4MgrIfAny()
		if mgr == nil {
			return nil
		}
		return mgr.Close(sessionID)
	}
	mgr := h.adbMgrIfAny()
	if mgr == nil {
		return nil
	}
	return mgr.Close(sessionID)
}

// adbMgrIfAny 取已创建的 ADB 管理器（未创建返回 nil——写入/关闭不隐式建管理器）
func (h *terminalHub) adbMgrIfAny() *adbterm.Manager {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.adbMgr
}

// v4MgrIfAny 取已创建的 V4 WS 管理器（未创建返回 nil）
func (h *terminalHub) v4MgrIfAny() *v4term.Manager {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.v4wsMgr
}
