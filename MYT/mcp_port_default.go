//go:build !mcp

package main

// mcpPreferredPort 自建 MCP server 首选端口。
//
// 不带 mcp tag 的构建中 Wails 自带 MCP 不存在（App.Run 不会起 9099），
// 本 server 独占 9099；被占则由 mcpserver 回退随机端口。
const mcpPreferredPort = 9099
