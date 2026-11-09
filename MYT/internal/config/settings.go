package config

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/moyunteng/myt/internal/util"
)

// --- Token & 认证凭证 ---

func (m *Manager) SetToken(token string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.config.Token = token
}

func (m *Manager) GetToken() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.config.Token
}

func (m *Manager) GetAuthCredentials() (username, password string) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.config.AuthUsername, m.config.AuthPassword
}

func (m *Manager) SetAuthCredentials(username, password string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.config.AuthUsername = username
	m.config.AuthPassword = password
	return m.writeConfigLocked()
}

func (m *Manager) ClearAuthCredentials() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.config.AuthUsername = ""
	m.config.AuthPassword = ""
	return m.writeConfigLocked()
}

func (m *Manager) credentialsPath() string {
	return filepath.Join(m.root, "config", "credentials.json")
}

func (m *Manager) GetRememberedCredentials() (username, password string) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var cred struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := m.readJSON(m.credentialsPath(), &cred); err == nil {
		return cred.Username, cred.Password
	}
	return "", ""
}

func (m *Manager) SetRememberedCredentials(username, password string) error {
	cred := struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}{Username: username, Password: password}
	return m.writeJSON(m.credentialsPath(), cred)
}

func (m *Manager) ClearRememberedCredentials() {
	_ = os.Remove(m.credentialsPath())
}

// --- 镜像缓存 mirror_cache.json ---

func (m *Manager) mirrorCachePath() string {
	return filepath.Join(m.root, "data", "mirror_cache.json")
}

func (m *Manager) SetMirrorCache(items []util.MirrorItem) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.writeJSON(m.mirrorCachePath(), items); err != nil {
		log.Printf("写入镜像缓存失败: %v", err)
	}
}

func (m *Manager) GetMirrorCache() []util.MirrorItem {
	m.mu.RLock()
	defer m.mu.RUnlock()
	path := m.mirrorCachePath()
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var items []util.MirrorItem
	if err := json.Unmarshal(data, &items); err != nil {
		return nil
	}
	return items
}

// --- AppConfig 设置项相关方法 ---

func (m *Manager) GetDataStoragePath() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.config.DataStoragePath != "" {
		return m.config.DataStoragePath
	}
	return m.root
}

func (m *Manager) SetDataStoragePath(newPath string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if newPath == "" {
		return nil
	}
	m.config.DataStoragePath = newPath
	return m.writeConfigLocked()
}

func (m *Manager) GetAutoStart() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.config.AutoStart
}

func (m *Manager) SetAutoStart(enabled bool) error {
	m.mu.Lock()
	m.config.AutoStart = enabled
	cfgErr := m.writeConfigLocked()
	m.mu.Unlock()

	if cfgErr != nil {
		return cfgErr
	}
	if enabled {
		return enableAutoStart()
	}
	return disableAutoStart()
}

func (m *Manager) GetCloseBehavior() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.config.CloseBehavior
}

func (m *Manager) SetCloseBehavior(behavior string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.config.CloseBehavior = behavior
	return m.writeConfigLocked()
}

// GetDeployMode 返回部署模式："local"（局域网直连设备）或 "public"（走线上服务转发）
func (m *Manager) GetDeployMode() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.config.DeployMode == "" {
		return "local"
	}
	return m.config.DeployMode
}

// SetDeployMode 设置部署模式并持久化。
// 双列表隔离后，切换模式只改 mode，不清空任何设备数据：
// local/public/v2/v4 各自独立存储（device_list_<mode>.json），
// 切换时由上层按新模式读取对应列表，互不污染、互不丢失。
// 合法值校验：未知值直接拒绝——否则会落到 currentModeLocked 的 local 兜底，
// 却把非法字符串写进 config.json（下次启动模式语义与文件内容不一致）。
func (m *Manager) SetDeployMode(mode string) error {
	switch mode {
	case "local", "public", "v2", "v4":
	default:
		return fmt.Errorf("非法的部署模式: %q", mode)
	}
	m.mu.Lock()
	m.config.DeployMode = mode
	err := m.writeConfigLocked()
	m.mu.Unlock()
	return err
}

func (m *Manager) GetLogLevel() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.config.LogLevel
}

func (m *Manager) SetLogLevel(level string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.config.LogLevel = level
	return m.writeConfigLocked()
}

func (m *Manager) GetAutoCheckUpdate() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.config.AutoCheckUpdate
}

func (m *Manager) SetAutoCheckUpdate(enabled bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.config.AutoCheckUpdate = enabled
	return m.writeConfigLocked()
}

func (m *Manager) GetAutoDownloadUpdate() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.config.AutoDownloadUpdate
}

func (m *Manager) SetAutoDownloadUpdate(enabled bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.config.AutoDownloadUpdate = enabled
	return m.writeConfigLocked()
}

func (m *Manager) GetAutoUpgradeSDK() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.config.AutoUpgradeSDK
}

func (m *Manager) SetAutoUpgradeSDK(enabled bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.config.AutoUpgradeSDK = enabled
	return m.writeConfigLocked()
}

func (m *Manager) GetNoSidebar() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.config.NoSidebar
}

func (m *Manager) SetNoSidebar(enabled bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.config.NoSidebar = enabled
	return m.writeConfigLocked()
}

// GetGpuAccel 返回投屏 GPU 硬件加速开关。新配置默认关闭（初始化时 GpuAccel=false）。
func (m *Manager) GetGpuAccel() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.config.GpuAccel
}

// SetGpuAccel 设置投屏 GPU 硬件加速并持久化。
func (m *Manager) SetGpuAccel(enabled bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.config.GpuAccel = enabled
	return m.writeConfigLocked()
}

// GetSingleCastScale 返回单台投屏窗口大小（倍率，1.0=画面高基准 1140）。
// 无配置（≤0）默认 0.7：竖屏单台画面 448x798（高度对齐壳子画面 798）。
func (m *Manager) GetSingleCastScale() float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.config.SingleCastScale <= 0 {
		return 0.7
	}
	return m.config.SingleCastScale
}

// SetSingleCastScale 设置单台投屏窗口大小并持久化（范围 0.35~1.0，越界裁剪）。
func (m *Manager) SetSingleCastScale(scale float64) error {
	if scale < 0.35 {
		scale = 0.35
	}
	if scale > 1.0 {
		scale = 1.0
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.config.SingleCastScale = scale
	return m.writeConfigLocked()
}

func (m *Manager) GetDevicePasswordsMap() map[string]string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	cp := make(map[string]string)
	for k, v := range m.config.DevicePasswords {
		cp[k] = v
	}
	return cp
}

func (m *Manager) GetDevicePassword(deviceIP string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.config.DevicePasswords[deviceIP]
}

func (m *Manager) SetDevicePasswordInConfig(deviceID, password string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.config.DevicePasswords == nil {
		m.config.DevicePasswords = make(map[string]string)
	}
	if password == "" {
		delete(m.config.DevicePasswords, deviceID)
	} else {
		m.config.DevicePasswords[deviceID] = password
	}
	return m.writeConfigLocked()
}

func (m *Manager) RemoveDevicePasswordFromConfig(deviceID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.config.DevicePasswords, deviceID)
	return m.writeConfigLocked()
}

// ClearAllCache/OpenDirectory/writeConfigLocked/enableAutoStart/disableAutoStart
// 已拆分到 settings_system.go
