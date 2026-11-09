package main

import "fmt"

// ─── Connection state ────────────────────────────────────────────────────────
// 连接状态常量 + 文案。从 keyboard.go 提取（原本与键盘逻辑无关，属于晦涩性坏味道）。

const (
	ConnConnecting   = 1
	ConnConnected    = 2
	ConnDisconnected = 3
	ConnError        = 4
	ConnRetrying     = 5
	ConnTimeout      = 6 // max retries reached, user must decide
	ConnKicked       = 7 // session taken over by another client
)

// connStateText 返回投屏窗口里显示的连接状态文案。
func connStateText(state int, retry int) string {
	switch state {
	case ConnConnecting:
		if retry == 0 {
			return "▶ 连接中..."
		}
		return fmt.Sprintf("✖ 连接失败，第%d次重试中...", retry)
	case ConnConnected:
		return ""
	case ConnDisconnected:
		return "✖ 连接断开，重连中..."
	case ConnError:
		return "✖ 连接失败"
	case ConnRetrying:
		return fmt.Sprintf("✖ 连接失败，第%d次重试中...", retry+1)
	case ConnTimeout:
		return "✖ 连接失败"
	case ConnKicked:
		return "✖ 连接被其他客户端占用，点击重连"
	default:
		return ""
	}
}
