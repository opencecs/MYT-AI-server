package config

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/moyunteng/myt/internal/util"
)

// --- 缓存清理、目录打开、配置写入、开机自启动 ---

// ClearAllCache 清理所有业务缓存，恢复到新安装状态
func (m *Manager) ClearAllCache() error {
	m.mu.Lock()
	// 持锁期间收集需要清理的路径和重置内存状态
	paths := []string{
		m.deviceListLocalPath(),
		m.deviceListPublicPath(),
		m.deviceListV2Path(),
		m.deviceListV4Path(),
		m.deviceNotesPath(),
		m.androidListLocalPath(),
		m.androidListPublicPath(),
		m.androidListV2Path(),
		m.androidListV4Path(),
		m.androidNotesPath(),
		m.slotExpiryPath(),
		m.slotTagsPath(),
		m.credentialsPath(),
	}
	imageDir := filepath.Join(m.root, "data", "images")
	mirrorCache := filepath.Join(m.root, "data", "mirror_cache.json")
	cacheDir := filepath.Join(m.root, "cache")
	logDir := filepath.Join(m.root, "logs")

	m.deviceListLocal = nil
	m.deviceListPublic = nil
	m.deviceListV2 = nil
	m.deviceListV4 = nil
	m.deviceNotes = make(map[string]string)
	m.androidListLocal = nil
	m.androidListPublic = nil
	m.androidListV2 = nil
	m.androidListV4 = nil
	m.androidNotes = make(map[string]string)
	m.slotExpiry = make(map[string][]SlotExpiry)
	m.slotTags = make(map[string][]string)
	// 重置脏标记，防止 flushDirty 将空数据写回重建文件
	m.deviceListLocalDirty.Store(false)
	m.deviceListPublicDirty.Store(false)
	m.deviceListV2Dirty.Store(false)
	m.deviceListV4Dirty.Store(false)
	m.deviceNotesDirty.Store(false)
	m.androidListLocalDirty.Store(false)
	m.androidListPublicDirty.Store(false)
	m.androidListV2Dirty.Store(false)
	m.androidListV4Dirty.Store(false)
	m.androidNotesDirty.Store(false)
	m.slotExpiryDirty.Store(false)
	m.slotTagsDirty.Store(false)

	m.config.DevicePasswords = make(map[string]string)
	m.config.AuthUsername = ""
	m.config.AuthPassword = ""
	m.config.Token = ""
	if err := m.writeConfigLocked(); err != nil {
		m.mu.Unlock()
		return err
	}
	m.mu.Unlock()

	// 释放锁后执行文件 I/O
	for _, f := range paths {
		os.Remove(f)
	}
	os.RemoveAll(imageDir)
	os.Remove(mirrorCache)
	os.RemoveAll(cacheDir)
	if entries, err := os.ReadDir(logDir); err == nil {
		for _, e := range entries {
			os.Truncate(filepath.Join(logDir, e.Name()), 0)
		}
	}

	return nil
}

// OpenDirectory 使用系统命令打开目录
func (m *Manager) OpenDirectory(path string) error {
	return util.OpenDirectory(path)
}

// writeConfigLocked 写入 config.json（调用者已持锁）
func (m *Manager) writeConfigLocked() error {
	path := filepath.Join(m.root, "config", "config.json")
	data, err := json.MarshalIndent(m.config, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化配置失败: %w", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("写入配置失败: %w", err)
	}
	return nil
}

// --- 开机自启动实现 ---

func enableAutoStart() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	switch runtime.GOOS {
	case "darwin":
		// 若在 .app bundle 内运行，推导 bundle 路径作为 Login Item
		appPath := exe
		if idx := strings.LastIndex(exe, ".app/Contents/MacOS/"); idx != -1 {
			appPath = exe[:idx+4] // 包含 ".app"
		}
		safePath := strings.ReplaceAll(appPath, `"`, `\"`)
		script := `tell application "System Events" to make login item at end with properties {path:"` + safePath + `", hidden:false}`
		return exec.Command("osascript", "-e", script).Run()
	case "windows":
		return exec.Command("reg", "add", `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`,
			"/v", "MYT", "/d", exe, "/f").Run()
	}
	return nil
}

func disableAutoStart() error {
	switch runtime.GOOS {
	case "darwin":
		// 尝试删除可能的 Login Item（兼容 .app bundle 名和二进制名）
		for _, name := range []string{"MYT", "myt"} {
			script := `tell application "System Events" to delete login item "` + name + `"`
			_ = exec.Command("osascript", "-e", script).Run()
		}
		return nil
	case "windows":
		return exec.Command("reg", "delete", `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`,
			"/v", "MYT", "/f").Run()
	}
	return nil
}

// --- MCP 调试服务开关 ---

// GetMCPEnabled 返回 MCP 调试服务开关（默认 true，仅开发构建实际生效）。
func (m *Manager) GetMCPEnabled() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.config.MCPEnabled
}

// SetMCPEnabled 设置 MCP 调试服务开关并持久化。生效需重启客户端（启动时读配置设环境变量）。
func (m *Manager) SetMCPEnabled(enabled bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.config.MCPEnabled = enabled
	return m.writeConfigLocked()
}

// --- MCP server 监听地址 ---

// GetMCPHost 返回 MCP server 监听地址（空串 = 未配置，使用默认 127.0.0.1）。
func (m *Manager) GetMCPHost() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.config.MCPHost
}

// SetMCPHost 设置 MCP server 监听地址并持久化（空串 = 恢复默认 127.0.0.1）。
func (m *Manager) SetMCPHost(host string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.config.MCPHost = host
	return m.writeConfigLocked()
}

// --- MCP server 鉴权 token ---

// GetMCPToken 返回 MCP server 鉴权 token（可能为空：尚未生成）。
func (m *Manager) GetMCPToken() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.config.MCPToken
}

// SetMCPToken 覆写 MCP token 并持久化（RegenerateMCPToken 用）。
func (m *Manager) SetMCPToken(token string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.config.MCPToken = token
	return m.writeConfigLocked()
}

// EnsureMCPToken 返回 token，为空时生成随机 token（crypto/rand 32 字节 hex）
// 并落盘。并发安全：双重检查避免重复生成。
func (m *Manager) EnsureMCPToken() string {
	m.mu.RLock()
	if t := m.config.MCPToken; t != "" {
		m.mu.RUnlock()
		return t
	}
	m.mu.RUnlock()

	token, err := generateMCPToken()
	if err != nil {
		// 生成失败兜底：用时间源拼一个（仅本地调试用，可接受弱随机）
		token = fmt.Sprintf("myt-%x", time.Now().UnixNano())
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.config.MCPToken != "" {
		return m.config.MCPToken // 并发下别人已生成
	}
	m.config.MCPToken = token
	if err := m.writeConfigLocked(); err != nil {
		log.Printf("[config] 写入 MCPToken 失败: %v", err)
	}
	return token
}

// generateMCPToken 生成 32 字节随机数的 hex 编码 token。
func generateMCPToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("生成 MCP token 失败: %w", err)
	}
	return hex.EncodeToString(buf), nil
}
