package cdp

// mock_test.go — 假 CDP 服务端（WS 短连接协议），仅本包测试使用。
// （跨包测试用 cdp/mock 子包：Go 测试二进制不允许 cdp -> cdp/mock -> cdp 的 import cycle。）

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
)

// --- mock CDP（WS 短连接协议） ---

// mockCDP 假 CDP 服务端：/json 返回 target 列表，/ws 按 handler 应答 JSON-RPC。
func mockCDP(t *testing.T, handler func(method string, params json.RawMessage) (any, error)) *Client {
	t.Helper()
	var wsURL string
	mux := http.NewServeMux()
	mux.HandleFunc("/json", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]string{
			{"id": "t1", "type": "page", "title": "test", "url": "http://localhost/", "webSocketDebuggerUrl": wsURL},
		})
	})
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
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
	c := NewClient(dir)
	if _, err := c.WaitPort(3 * time.Second); err != nil {
		t.Fatalf("mock CDP 端口发现失败: %v", err)
	}
	return c
}
