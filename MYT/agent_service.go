package main

import (
	"fmt"
	"log"
	"sync"

	"github.com/moyunteng/myt/internal/agent"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// AgentServiceWrapper Agent 维护（卸载/状态查询）的 Wails Service
type AgentServiceWrapper struct {
	app *application.App

	installMu    sync.Mutex
	installingIP map[string]bool
}

func NewAgentServiceWrapper() *AgentServiceWrapper {
	return &AgentServiceWrapper{installingIP: make(map[string]bool)}
}

func (s *AgentServiceWrapper) SetApp(app *application.App) { s.app = app }

func (s *AgentServiceWrapper) emitProgress(progress agent.InstallProgress) {
	if s.app != nil {
		s.app.Event.Emit("agent:install-progress", progress)
	}
}

func (s *AgentServiceWrapper) progressCallback(deviceIP string) agent.ProgressCallback {
	return func(p agent.InstallProgress) { p.DeviceIP = deviceIP; s.emitProgress(p) }
}

func (s *AgentServiceWrapper) tryLockInstall(deviceIP string) bool {
	s.installMu.Lock()
	defer s.installMu.Unlock()
	if s.installingIP[deviceIP] {
		return false
	}
	s.installingIP[deviceIP] = true
	return true
}

func (s *AgentServiceWrapper) unlockInstall(deviceIP string) {
	s.installMu.Lock()
	delete(s.installingIP, deviceIP)
	s.installMu.Unlock()
}

// ---------- 状态查询 ----------

func (s *AgentServiceWrapper) GetAgentStatus(deviceIP string) (*agent.AgentStatus, error) {
	return agent.GetAgentStatus(deviceIP)
}

// ---------- 卸载 ----------

func (s *AgentServiceWrapper) UninstallAgent(deviceIP string) error {
	if !s.tryLockInstall(deviceIP) {
		return fmt.Errorf("设备 %s 正在卸载中，请勿重复操作", deviceIP)
	}
	go func() {
		defer s.unlockInstall(deviceIP)
		result, err := agent.Uninstall(deviceIP, s.progressCallback(deviceIP))
		if err != nil {
			log.Printf("[agent] 卸载失败: %v", err)
		} else if !result.Success {
			log.Printf("[agent] 卸载失败: %s", result.Message)
		}
	}()
	return nil
}
