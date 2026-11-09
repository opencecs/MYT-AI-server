// Package v4term 管理多终端会话（V4 实例交互式终端）。
// 会话 ID 由本包生成，上层按 ID 写入 / resize / 关闭；输出与退出经回调
// 转发（上层接 Wails 事件 adbterm:data / adbterm:exit，与 V2 终端共用
// 前端监听通道，前端零改动）。
package v4term

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
)

// Manager 管理多个 V4 终端 WS 会话。
type Manager struct {
	mu       sync.Mutex
	sessions map[string]*Session
	seq      atomic.Uint64

	onData func(sessionID string, data []byte)
	onExit func(sessionID string, code int)
}

// NewManager 创建会话管理器；onData/onExit 由上层转发到前端。
func NewManager(onData func(sessionID string, data []byte),
	onExit func(sessionID string, code int)) *Manager {
	return &Manager{
		sessions: make(map[string]*Session),
		onData:   onData,
		onExit:   onExit,
	}
}

// Open 建立 WS 终端会话并返回会话 ID。url 形如
// ws://host:8088/android/<实例id>/terminal（user/password 可为空）；
// target 透传 Dial（"android" = 安卓 shell，空串 = SDK 默认容器 shell）。
func (m *Manager) Open(url, target, user, password string, cols, rows int) (string, error) {
	sessionID := fmt.Sprintf("v4t%d", m.seq.Add(1))
	sess, err := Dial(url, target, user, password, cols, rows,
		func(data []byte) {
			if m.onData != nil {
				m.onData(sessionID, data)
			}
		},
		func(code int) {
			m.remove(sessionID)
			if m.onExit != nil {
				m.onExit(sessionID, code)
			}
		})
	if err != nil {
		return "", err
	}
	m.mu.Lock()
	m.sessions[sessionID] = sess
	m.mu.Unlock()
	return sessionID, nil
}

// Write 发送输入数据到指定会话
func (m *Manager) Write(sessionID string, data []byte) error {
	sess := m.get(sessionID)
	if sess == nil {
		return errors.New("终端会话不存在或已关闭")
	}
	return sess.Write(data)
}

// Resize 同步指定会话的窗口大小
func (m *Manager) Resize(sessionID string, cols, rows int) error {
	sess := m.get(sessionID)
	if sess == nil {
		return errors.New("终端会话不存在或已关闭")
	}
	return sess.Resize(cols, rows)
}

// Close 关闭指定会话
func (m *Manager) Close(sessionID string) error {
	m.mu.Lock()
	sess := m.sessions[sessionID]
	delete(m.sessions, sessionID)
	m.mu.Unlock()
	if sess == nil {
		return nil
	}
	return sess.Close()
}

// CloseAll 关闭全部会话（应用退出 / 模式切换清理）
func (m *Manager) CloseAll() {
	m.mu.Lock()
	all := make([]*Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		all = append(all, s)
	}
	m.sessions = make(map[string]*Session)
	m.mu.Unlock()
	for _, s := range all {
		_ = s.Close()
	}
}

func (m *Manager) get(sessionID string) *Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sessions[sessionID]
}

func (m *Manager) remove(sessionID string) {
	m.mu.Lock()
	delete(m.sessions, sessionID)
	m.mu.Unlock()
}
