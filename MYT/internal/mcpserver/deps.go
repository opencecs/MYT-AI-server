package mcpserver

import "github.com/moyunteng/myt/internal/mcpserver/tools"

// deps.go — 业务依赖类型转发：BizDeps 及其接口声明在 tools 子包，
// 此处类型别名保持 main 包的 mcpserver.BizDeps 引用不变。
type (
	// BizDeps 业务级工具背后的服务能力（main 包各 ServiceWrapper 隐式实现）。
	BizDeps = tools.BizDeps
)
