//go:build mcp

package main

// mcpPreferredPort 自建 MCP server 首选端口。
//
// 带 mcp tag 的构建中，Wails 自带 MCP server 会在 App.Run() 里无条件抢占
// 9099（listen 失败会直接让 App.Run 报错退出）。本 server 启动早于 App.Run，
// 必须让位：直接用随机端口，实际地址经 MCPService.GetMCPStatus 查询。
const mcpPreferredPort = 0
