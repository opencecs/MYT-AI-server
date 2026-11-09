package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"time"

	"github.com/moyunteng/myt/internal/config"
	"github.com/moyunteng/myt/internal/mcpserver"
)

// MCPService 客户端自建 MCP server 的 Wails Service 层：
// 状态查询 / 热启停 / token 轮换。server 本体在 internal/mcpserver。
type MCPService struct {
	cm             *config.Manager
	srv            *mcpserver.Server
	deps           *mcpserver.BizDeps // 业务工具依赖（换地址重建 server 时复用）
	webviewDataDir string             // WebView2 用户数据目录（换地址重建 server 时复用）
}

// MCPStatus MCP server 状态（设置页展示用）
type MCPStatus struct {
	Enabled  bool   `json:"enabled"`  // 配置开关
	Running  bool   `json:"running"`  // server 实际监听中
	Host     string `json:"host"`     // 配置的监听地址（空/127.0.0.1 = 仅本机）
	URL      string `json:"url"`      // 实际 URL（含端口，端口被占时非 9099；0.0.0.0 时已替换为本机 IP）
	Token    string `json:"token"`    // 鉴权 token
	CDPReady bool   `json:"cdpReady"` // CDP 端口可用（UI 级工具可用的前提）
	CDPPort  int    `json:"cdpPort"`  // CDP 调试端口（0 = 未知）
	Tools    int    `json:"tools"`    // 已注册工具数
}

// NewMCPService 创建 MCP service 并组装业务工具依赖（deps 由各 Wrapper 提供）。
func NewMCPService(cm *config.Manager, rootDir string, deps *mcpserver.BizDeps) *MCPService {
	s := &MCPService{
		cm:             cm,
		deps:           deps,
		webviewDataDir: webviewDataDir(rootDir),
	}
	s.srv = mcpserver.New(mcpserver.Options{
		Host:           cm.GetMCPHost(),  // 监听地址（空 = 127.0.0.1；启动时生效）
		PreferredPort:  mcpPreferredPort, // mcp tag 构建时让位 Wails 自带 MCP（见 mcp_port_*.go）
		WebviewDataDir: s.webviewDataDir,
	}, deps)
	return s
}

// webviewDataDir WebView2 用户数据目录（DevToolsActivePort 文件所在，与 setup.go newWailsApp 一致）
func webviewDataDir(rootDir string) string {
	return fmt.Sprintf("%s/cache/webview2", rootDir)
}

// GetMCPStatus 返回 MCP server 当前状态。
func (s *MCPService) GetMCPStatus() MCPStatus {
	cdpPort, cdpReady := s.srv.CDPStatus()
	return MCPStatus{
		Enabled:  s.cm.GetMCPEnabled(),
		Running:  s.srv.Running(),
		Host:     s.cm.GetMCPHost(),
		URL:      s.srv.URL(),
		Token:    s.cm.GetMCPToken(),
		CDPReady: cdpReady,
		CDPPort:  cdpPort,
		Tools:    s.srv.ToolCount(),
	}
}

// SetMCPHost 设置监听地址并持久化：server 正在运行时自动 Stop→Start 使新地址生效
// （Stop 与 Start 之间有短暂不可用窗口）；未运行则只保存，下次启动生效。
// host 传空串恢复默认 127.0.0.1。
func (s *MCPService) SetMCPHost(host string) error {
	wasRunning := s.srv.Running()
	if wasRunning {
		s.srv.Stop() // 停止后才能换地址（Server.opts 只在 Start 时读取）
	}
	if err := s.cm.SetMCPHost(host); err != nil {
		if wasRunning {
			// 配置写失败：按旧地址拉回来，保持运行状态与配置一致
			if startErr := s.startServer(); startErr != nil {
				return fmt.Errorf("写入配置失败: %v，且按旧地址重启失败: %v", err, startErr)
			}
		}
		return err
	}
	// 用新地址重建 server（Options.Host 在 New 时固化，换地址需重新构造）
	s.srv = mcpserver.New(mcpserver.Options{
		Host:           host,
		PreferredPort:  mcpPreferredPort,
		WebviewDataDir: s.webviewDataDir,
	}, s.deps)
	if wasRunning {
		return s.startServer()
	}
	return nil
}

// startServer 以当前配置启动 server（token 取自配置，确保非回环地址校验通过）。
func (s *MCPService) startServer() error {
	s.srv.SetToken(s.cm.EnsureMCPToken())
	if err := s.srv.Start(); err != nil {
		return err
	}
	s.waitCDPInBackground()
	return nil
}

// SetMCPEnabled 热启停 MCP server：开 → EnsureMCPToken + 启动监听（含 CDP 端口探测）；
// 关 → 优雅停止。幂等：重复开/关不报错。配置即时持久化。
//
// 注意：CDP 参数是窗口创建时生效的——客户端启动时开关为关、运行中才打开的场景，
// CDP 不可用（UI 级工具返回「界面控制需重启客户端后可用」），业务级工具不受影响。
func (s *MCPService) SetMCPEnabled(enabled bool) error {
	if !enabled {
		s.srv.Stop()
		return s.cm.SetMCPEnabled(false)
	}
	if err := s.startServer(); err != nil {
		return err
	}
	return s.cm.SetMCPEnabled(true)
}

// RegenerateMCPToken 换 token：重新生成并持久化，server 不重启但下次请求即用新 token。
func (s *MCPService) RegenerateMCPToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("生成 token 失败: %w", err)
	}
	token := hex.EncodeToString(buf)
	if err := s.cm.SetMCPToken(token); err != nil {
		return "", err
	}
	s.srv.SetToken(token)
	return token, nil
}

// StartIfEnabled 配置开启时启动 server（main 启动流程调用；幂等）。
func (s *MCPService) StartIfEnabled() error {
	if !s.cm.GetMCPEnabled() {
		return nil
	}
	if err := s.SetMCPEnabled(true); err != nil {
		return err
	}
	log.Printf("[mcp] MCP server 随客户端启动: %s", s.srv.URL())
	return nil
}

// waitCDPInBackground 后台轮询 DevToolsActivePort 拿 CDP 端口（最多等 30s），
// 拿到后 UI 级工具即可用。
func (s *MCPService) waitCDPInBackground() {
	go func() {
		port, err := s.srv.WaitCDPReady(30 * time.Second)
		if err != nil {
			log.Printf("[mcp] CDP 端口未就绪（UI 级工具不可用）: %v", err)
			return
		}
		log.Printf("[mcp] CDP 就绪，端口 %d", port)
	}()
}
