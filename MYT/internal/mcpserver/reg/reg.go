// Package reg 提供 MCP 工具注册的共享 helper（各子包 cdp/uimap/browser/tools
// 与根包 mcpserver 共用），避免每个包重复实现注册样板。
//
// sink 由调用方注入（根包 Server.recordTool），用于工具计数与诊断。
package reg

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Sink 工具注册回调（根包 Server 实现：计数 + 记录引用）。
type Sink func(t *mcp.Tool)

// Reg 注册一个带类型化参数 schema 的工具（go-sdk 反射 In 结构体生成 inputSchema）。
// handler 返回 error 时由 SDK 打包为 IsError 工具结果（非协议错误）。
// ann 可选传 ToolAnnotations（readOnlyHint/destructiveHint 等提示）。
//
// structuredContent 规范适配（SEP-2106）：MCP 要求 structuredContent 必须是
// JSON 对象（record）。handler 返回数组或标量时统一包一层 {"items": ...}
// （对象/nil 透传不动，字段内容不变只是加壳）；对象返回（官方惯例已是
// {"requests":[...]} 之类）不受影响。content 文本回退由 SDK 生成，同步拿到
// 加壳后的 JSON，外部 AI 两条路径读到的一致。
func Reg[In any](srv *mcp.Server, sink Sink, name, desc string,
	h func(ctx context.Context, args In) (any, error), ann ...*mcp.ToolAnnotations) {
	tool := &mcp.Tool{Name: name, Description: desc}
	if len(ann) > 0 {
		tool.Annotations = ann[0]
	}
	mcp.AddTool(srv, tool,
		func(ctx context.Context, req *mcp.CallToolRequest, in In) (*mcp.CallToolResult, any, error) {
			out, err := h(ctx, in)
			if err != nil {
				return nil, nil, err
			}
			return nil, wrapStructured(out), nil
		})
	if sink != nil {
		sink(tool)
	}
}

// wrapStructured 把 handler 返回值规整为 MCP 允许的 structuredContent 形态：
// 数组/标量包 {"items": v}；map/nil/已包装对象原样透传。
func wrapStructured(out any) any {
	if out == nil {
		return nil
	}
	if _, ok := out.(map[string]any); ok {
		return out
	}
	return map[string]any{"items": out}
}

// RegRaw 注册无结构化输出的工具（如返回图片内容），handler 直接构造结果。
func RegRaw[In any](srv *mcp.Server, sink Sink, name, desc string,
	h func(ctx context.Context, args In) (*mcp.CallToolResult, error), ann ...*mcp.ToolAnnotations) {
	tool := &mcp.Tool{Name: name, Description: desc}
	if len(ann) > 0 {
		tool.Annotations = ann[0]
	}
	mcp.AddTool(srv, tool,
		func(ctx context.Context, req *mcp.CallToolRequest, in In) (*mcp.CallToolResult, any, error) {
			res, err := h(ctx, in)
			if err != nil {
				return nil, nil, err
			}
			return res, nil, nil
		})
	if sink != nil {
		sink(tool)
	}
}

// ReadOnly 只读工具注解。
func ReadOnly() *mcp.ToolAnnotations { return &mcp.ToolAnnotations{ReadOnlyHint: true} }

// Destructive 破坏性工具注解（非只读 + destructiveHint=true）。
func Destructive() *mcp.ToolAnnotations {
	f := true
	return &mcp.ToolAnnotations{ReadOnlyHint: false, DestructiveHint: &f}
}

// Effectful 有副作用但非破坏性（如发起投屏：改变运行状态，但不销毁数据）。
func Effectful() *mcp.ToolAnnotations {
	f := false
	return &mcp.ToolAnnotations{ReadOnlyHint: false, DestructiveHint: &f}
}
