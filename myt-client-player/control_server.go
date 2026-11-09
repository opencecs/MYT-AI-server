package main

// control_server.go — 播放器本地控制 HTTP 接口（仅绑 127.0.0.1，主程序/MCP 专用）：
//
//	GET  /screenshot → app.captureRendererFrame() 取帧 → PNG 内存直出；
//	                   纹理硬解帧不可截取（D3D11 SendFrame 只拷非纹理）→ 409
//	POST /close      → RequestClose()（PostMessage(hParent, WM_DESTROY)，
//	                   OnSignalShutdown 同款优雅退出）
//
// 启用条件：-control-port > 0（主程序投屏拉起时注入随机可用端口）。
// 零新依赖：net/http 标准库。

import (
	"encoding/json"
	"fmt"

	"net/http"
	"sync"
)

// startControlServer 启动本地控制 HTTP server（-control-port > 0 时调用）。
func startControlServer(app *App, port int) {
	if port <= 0 {
		return
	}
	go func() {
		mux := http.NewServeMux()

		// 截图防并发：取帧与渲染/解码线程交互，同一时刻只处理一个请求
		var shotMu sync.Mutex
		mux.HandleFunc("GET /screenshot", func(w http.ResponseWriter, r *http.Request) {
			shotMu.Lock()
			defer shotMu.Unlock()
			bgra, w32, h32, rotated := app.captureRendererFrame()
			if len(bgra) == 0 || w32 <= 0 || h32 <= 0 {
				// 按当前渲染模式如实描述：D3D11 硬解纹理帧（IsTexture）在
				// SendFrame 时不拷贝截不到；GDI/软解为渲染帧空窗（可重试）
				msg := "当前帧未就绪（软解渲染帧空窗），请重试"
				if app.D3D11Ren.Load() != nil {
					msg = "纹理硬解帧不可截取，可改用设备端截图接口"
				}
				writeControlJSON(w, http.StatusConflict, map[string]string{"error": msg})
				return
			}
			pngBytes, err := encodeFramePNG(bgra, w32, h32, rotated)
			if err != nil {
				writeControlJSON(w, http.StatusInternalServerError, map[string]string{
					"error": fmt.Sprintf("帧编码失败: %v", err),
				})
				return
			}
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(pngBytes)
		})

		mux.HandleFunc("POST /close", func(w http.ResponseWriter, r *http.Request) {
			if app.RequestClose == nil {
				writeControlJSON(w, http.StatusInternalServerError, map[string]string{
					"error": "关闭通道未注入（UI 层未初始化完成）",
				})
				return
			}
			app.RequestClose()
			writeControlJSON(w, http.StatusOK, map[string]any{"ok": true})
		})

		addr := fmt.Sprintf("127.0.0.1:%d", port)
		logf("[control] control server listening http://%s\n", addr)
		if err := http.ListenAndServe(addr, mux); err != nil {
			logf("[control] control server exited: %v\n", err)
		}
	}()
}

// writeControlJSON 控制接口的 JSON 响应。
func writeControlJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
