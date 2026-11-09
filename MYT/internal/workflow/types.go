package workflow

import "context"

// Step 单个采集步骤
type Step struct {
	ID    string // 唯一标识 "dmesg"、"logcat"
	Label string // 前端显示 "内核日志分析"
	Run   func(ctx context.Context) (string, error)
}

// ToolDef ReAct 工具定义
type ToolDef struct {
	Name        string
	Description string
	Parameters  string // JSON Schema 字符串
	Handler     func(args map[string]any) (string, error)
}

// ToolCall AI 返回的工具调用
type ToolCall struct {
	ID        string
	Name      string
	Arguments string // JSON 字符串
}

// Workflow 工作流定义（使用者只需填充此结构）
type Workflow struct {
	ID           string
	Title        string
	Steps        []Step
	Tools        []ToolDef // 空=纯采集不分析
	SystemPrompt string
	BuildContext func(results map[string]string) string
	MaxIter      int // 默认 10
}

// WorkflowResult 执行结果
type WorkflowResult struct {
	Content     string            // AI 最终回答
	StepResults map[string]string // 采集成功的步骤 id→output
	StepErrors  map[string]string // 采集失败的步骤 id→error
}

// ProgressEvent 进度事件数据
type ProgressEvent struct {
	Workflow string `json:"workflow"`
	Step     string `json:"step"`
	Label    string `json:"label"`
	Status   string `json:"status"` // "done" | "running" | "failed"
	Failed   bool   `json:"failed"`
	Time     string `json:"time"`   // RFC3339 时间戳
}

// ChatRequest AI 对话请求（带 Function Calling）
type ChatRequest struct {
	SystemPrompt string
	Messages     []ChatMessage
	Tools        []ToolDef
}

// ChatMessage 对话消息
type ChatMessage struct {
	Role       string     // "system" | "user" | "assistant" | "tool"
	Content    string
	ToolCalls  []ToolCall // assistant 消息可能携带
	ToolCallID string     // tool 消息需要
}

// ChatResponse AI 对话响应
type ChatResponse struct {
	Content   string     // AI 文本回复
	ToolCalls []ToolCall // AI 请求的工具调用（空=最终回答）
}

// AIChatClient AI对话客户端接口（解耦 workflow 与 main 包）
type AIChatClient interface {
	// SimpleChat 单轮对话（不支持 Function Calling）
	SimpleChat(ctx context.Context, systemPrompt, userMsg string) (string, error)
	// ChatWithTools 带 Function Calling 的对话
	ChatWithTools(ctx context.Context, req *ChatRequest) (*ChatResponse, error)
}
