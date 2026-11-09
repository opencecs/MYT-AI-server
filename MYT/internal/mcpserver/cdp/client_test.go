package cdp

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// --- DevToolsActivePort 多路径探测 ---

func TestDiscoverPort(t *testing.T) {
	writeFile := func(t *testing.T, path, content string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("EBWebView 子目录优先", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, "EBWebView", "DevToolsActivePort"), "62124\nws-path\n")
		c := NewClient(dir)
		port, err := c.discoverPort()
		if err != nil {
			t.Fatalf("discoverPort: %v", err)
		}
		if port != 62124 {
			t.Fatalf("port = %d, want 62124", port)
		}
	})

	t.Run("根目录回退", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, "DevToolsActivePort"), "12345\n")
		c := NewClient(dir)
		port, err := c.discoverPort()
		if err != nil {
			t.Fatalf("discoverPort: %v", err)
		}
		if port != 12345 {
			t.Fatalf("port = %d, want 12345", port)
		}
	})

	t.Run("两处都有时取 EBWebView", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, "EBWebView", "DevToolsActivePort"), "11111\n")
		writeFile(t, filepath.Join(dir, "DevToolsActivePort"), "22222\n")
		c := NewClient(dir)
		port, err := c.discoverPort()
		if err != nil {
			t.Fatalf("discoverPort: %v", err)
		}
		if port != 11111 {
			t.Fatalf("port = %d, want 11111", port)
		}
	})

	t.Run("都找不到时报错", func(t *testing.T) {
		c := NewClient(t.TempDir())
		if _, err := c.discoverPort(); err == nil {
			t.Fatal("期望报错，实际成功")
		}
	})

	t.Run("未配置数据目录报错", func(t *testing.T) {
		c := NewClient("")
		if _, err := c.discoverPort(); err == nil {
			t.Fatal("期望报错，实际成功")
		}
	})

	t.Run("内容无效报错", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, "EBWebView", "DevToolsActivePort"), "not-a-port\n")
		c := NewClient(dir)
		if _, err := c.discoverPort(); err == nil {
			t.Fatal("期望报错，实际成功")
		}
	})
}

// --- 缓存端口失效重发现 ---

// startFakeCDP 起一个模拟 CDP /json 端点的 httptest server，返回监听端口。
func startFakeCDP(t *testing.T) int {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/json" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]Target{{
			ID:                   "t1",
			Type:                 "page",
			Title:                "测试页",
			URL:                  "http://example.com/",
			WebSocketDebuggerURL: "ws://127.0.0.1:1/devtools/page/t1",
		}})
	}))
	t.Cleanup(srv.Close)
	return srv.Listener.Addr().(*net.TCPAddr).Port
}

// deadPort 找一个当前无人监听的端口（模拟已死进程残留的 CDP 端口）。
func deadPort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	return port
}

// writePortFile 在 dir 下写入 DevToolsActivePort 文件（EBWebView 子目录）。
func writePortFile(t *testing.T, dir string, port int) {
	t.Helper()
	path := filepath.Join(dir, "EBWebView", "DevToolsActivePort")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(fmt.Sprintf("%d\n", port)), 0o644); err != nil {
		t.Fatal(err)
	}
}

// 缓存指向已死端口时，PageTargets 应作废缓存、按文件重新发现并成功。
func TestPageTargetsRediscoverStaleCache(t *testing.T) {
	dir := t.TempDir()
	live := startFakeCDP(t)
	writePortFile(t, dir, live)
	c := NewClient(dir)
	c.port.Store(int32(deadPort(t))) // 模拟缓存残留上一轮进程端口

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	targets, err := c.PageTargets(ctx)
	if err != nil {
		t.Fatalf("PageTargets: %v", err)
	}
	if len(targets) != 1 || targets[0].ID != "t1" {
		t.Fatalf("targets = %+v", targets)
	}
	if c.Port() != live {
		t.Fatalf("缓存端口未更新为活端口：got %d want %d", c.Port(), live)
	}
}

// 缓存指向已死端口时，Ready 应重发现后返回 true；文件里的端口也失效时返回 false。
func TestReadyRediscoverStaleCache(t *testing.T) {
	dir := t.TempDir()
	live := startFakeCDP(t)
	writePortFile(t, dir, live)
	c := NewClient(dir)
	c.port.Store(int32(deadPort(t)))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if !c.Ready(ctx) {
		t.Fatal("Ready = false，期望经重发现后为 true")
	}
	if c.Port() != live {
		t.Fatalf("缓存端口未更新：got %d want %d", c.Port(), live)
	}

	// 文件里的端口也是死的：重发现后仍不可达 → false
	writePortFile(t, dir, deadPort(t))
	c2 := NewClient(dir)
	c2.port.Store(int32(deadPort(t)))
	if c2.Ready(ctx) {
		t.Fatal("Ready = true，期望死端口下为 false")
	}
}

// WaitPort：发现的端口连通性验证失败时应继续轮询，等文件更新为活端口后返回。
func TestWaitPortSkipsStalePortUntilReachable(t *testing.T) {
	dir := t.TempDir()
	live := startFakeCDP(t)
	writePortFile(t, dir, deadPort(t))
	c := NewClient(dir)

	go func() {
		time.Sleep(300 * time.Millisecond) // 模拟 WebView2 启动后重写文件
		writePortFile(t, dir, live)
	}()

	port, err := c.WaitPort(5 * time.Second)
	if err != nil {
		t.Fatalf("WaitPort: %v", err)
	}
	if port != live {
		t.Fatalf("port = %d, want %d", port, live)
	}
	if c.Port() != live {
		t.Fatalf("缓存端口未更新：got %d want %d", c.Port(), live)
	}
}

// WaitPort：端口始终不可达时应超时报错。
func TestWaitPortTimeoutWhenUnreachable(t *testing.T) {
	dir := t.TempDir()
	writePortFile(t, dir, deadPort(t))
	c := NewClient(dir)
	if _, err := c.WaitPort(1 * time.Second); err == nil {
		t.Fatal("期望超时报错，实际成功")
	}
}

// --- CDP 请求 id 生成 ---

// maxJSSafeInteger JS double 可精确表示的最大整数（2^53）。
const maxJSSafeInteger float64 = 9007199254740992

// TestNextCDPReqIDUniqueAndSafe 验证 CDP 请求 id 在并发调用下唯一，
// 且始终 ≤ 2^53（Chromium DevTools 服务端以 JS double 存 id，
// 超出精度范围会导致回显 id 与请求不一致、响应被丢弃）。
func TestNextCDPReqIDUniqueAndSafe(t *testing.T) {
	const workers = 8
	const perWorker = 1000

	var mu sync.Mutex
	seen := make(map[int64]bool, workers*perWorker)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < perWorker; j++ {
				id := nextReqID()
				mu.Lock()
				if seen[id] {
					mu.Unlock()
					t.Errorf("id %d 重复（并发下必须唯一）", id)
					return
				}
				seen[id] = true
				mu.Unlock()
				if id <= 0 {
					t.Errorf("id %d 必须为正", id)
					return
				}
				if float64(id) > maxJSSafeInteger {
					t.Errorf("id %d 超过 JS double 安全整数范围 2^53", id)
					return
				}
			}
		}()
	}
	wg.Wait()
}

// TestNextCDPReqIDIncreases 验证 id 单调严格递增（计数器为包级全局状态，
// 不假设起始值，只验证连续两次调用严格 +1）。
func TestNextCDPReqIDIncreases(t *testing.T) {
	prev := nextReqID()
	for i := 0; i < 100; i++ {
		id := nextReqID()
		if id != prev+1 {
			t.Fatalf("id 应严格递增：prev=%d, got=%d", prev, id)
		}
		prev = id
	}
}

// TestCDPReqIDJSONRoundTrip 验证 id 序列化为 JSON 数字后再按 float64 解析无损
// （模拟 DevTools 服务端回显路径，float64 精度内必须可还原）。
func TestCDPReqIDJSONRoundTrip(t *testing.T) {
	for i := 0; i < 1000; i++ {
		id := nextReqID()
		data, err := json.Marshal(map[string]any{"id": id})
		if err != nil {
			t.Fatalf("marshal id %d: %v", id, err)
		}
		var got struct {
			ID float64 `json:"id"`
		}
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if int64(got.ID) != id {
			t.Fatalf("id %d 经 float64 往返后变为 %v（精度丢失）", id, got.ID)
		}
	}
}
