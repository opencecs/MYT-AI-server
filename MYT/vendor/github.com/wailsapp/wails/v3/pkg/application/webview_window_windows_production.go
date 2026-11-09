//go:build windows && !server && production && !devtools

package application

import "github.com/wailsapp/wails/v3/internal/webview2/pkg/edge"

func (w *windowsWebviewWindow) openDevTools() {}

func (w *windowsWebviewWindow) enableDevTools(settings *edge.ICoreWebViewSettings) {
	err := settings.PutAreDevToolsEnabled(true) // 始终开启：关闭会导致 WebView2 渲染异常
	if err != nil {
		globalApplication.handleFatalError(err)
	}
}
