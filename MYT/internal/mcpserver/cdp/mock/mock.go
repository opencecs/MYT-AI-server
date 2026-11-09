// Package mock 提供测试用假 CDP 服务端（WS 短连接 + 长连接协议通吃）。
// 仅测试代码引用，生产构建不包含。
package mock

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/moyunteng/myt/internal/mcpserver/cdp"
)

// Handler 处理一条 CDP JSON-RPC 命令，返回 result 或错误。
type Handler func(method string, params json.RawMessage) (any, error)

// New 启动假 CDP 服务端（/json 列 target + /ws 应答 JSON-RPC），
// 并返回指向它的 Client（DevToolsActivePort 文件已写好）。
func New(t *testing.T, handler Handler) *cdp.Client {
	t.Helper()
	var wsURL string
	mux := http.NewServeMux()
	mux.HandleFunc("/json", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]string{
			{"id": "t1", "type": "page", "title": "test", "url": "http://localhost/", "webSocketDebuggerUrl": wsURL},
		})
	})
	mux.HandleFunc("/version", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"webSocketDebuggerUrl": wsURL})
	})
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "")
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		for {
			_, data, err := conn.Read(ctx)
			if err != nil {
				return
			}
			var msg struct {
				ID     int64           `json:"id"`
				Method string          `json:"method"`
				Params json.RawMessage `json:"params"`
			}
			if err := json.Unmarshal(data, &msg); err != nil {
				continue
			}
			result, herr := handler(msg.Method, msg.Params)
			resp := map[string]any{"id": msg.ID, "result": result}
			if herr != nil {
				resp = map[string]any{"id": msg.ID, "error": map[string]any{"message": herr.Error()}}
			}
			payload, _ := json.Marshal(resp)
			if err := conn.Write(ctx, websocket.MessageText, payload); err != nil {
				return
			}
		}
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	wsURL = "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"

	// DevToolsActivePort 文件指向 mock 端口
	dir := t.TempDir()
	sub := filepath.Join(dir, "EBWebView")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "DevToolsActivePort"),
		[]byte(strings.TrimPrefix(srv.URL, "http://127.0.0.1:")), 0o644); err != nil {
		t.Fatal(err)
	}
	c := cdp.NewClient(dir)
	if _, err := c.WaitPort(3 * time.Second); err != nil {
		t.Fatalf("mock CDP 端口发现失败: %v", err)
	}
	return c
}
