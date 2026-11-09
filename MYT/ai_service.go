package main

import (
	"fmt"

	"github.com/moyunteng/myt/internal/config"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// AIServiceWrapper 包装 AI 编辑助手服务。
//
// 说明：原实现通过 ai.opencecs.com 登录并调用 /v1/chat/completions 完成
// 机型编辑对话。现 ai.opencecs.com 链路已移除，AIChat 保留方法签名作为
// 空壳，前端 AIEditModal 调用时会返回"功能暂未接入"。后续接入新的 AI
// 后端时，在 AIChat 内重新实现调用逻辑即可。
type AIServiceWrapper struct {
	cm      *config.Manager
	rootDir string
	app     *application.App
}

// NewAIServiceWrapper 创建 AI 服务包装。
func NewAIServiceWrapper(cm *config.Manager, rootDir string) *AIServiceWrapper {
	return &AIServiceWrapper{
		cm:      cm,
		rootDir: rootDir,
	}
}

// SetApp 注入 Wails 应用实例（保留以兼容 setup.go 装配）。
func (s *AIServiceWrapper) SetApp(app *application.App) { s.app = app }

// AIChatRequest 前端发起 AI 对话的参数。
type AIChatRequest struct {
	ModelName string `json:"modelName"`
	Message   string `json:"message"`
}

// AIChatResponse AI 对话返回的结果。
type AIChatResponse struct {
	Content    string `json:"content"`
	ParamsJSON string `json:"paramsJSON,omitempty"`
}

// AIChat 执行 AI 对话（空壳：ai.opencecs.com 链路已移除，待接入新后端）。
func (s *AIServiceWrapper) AIChat(reqJSON string) (*AIChatResponse, error) {
	return nil, fmt.Errorf("AI 编辑功能暂未接入，请使用手动编辑")
}
