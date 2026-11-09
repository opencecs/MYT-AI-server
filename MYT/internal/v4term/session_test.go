package v4term

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// mockTermServer 伪造 tobox 终端 WS 服务端：记录上行帧、下发预置输出、
// 按 closeCode/closeReason 收尾（0 表示保持连接直到客户端关闭）。
type mockTermServer struct {
	srv *httptest.Server

	mu          sync.Mutex
	authUser    string // 收到的 Basic Auth user（空=未携带）
	authPass    string
	textFrames  []string // 上行文本帧（原样）
	binaryFrms  [][]byte // 上行二进制帧
	resizeCols  int
	resizeRows  int
	output      []byte // 升级后立即下发的输出（二进制帧）
	hintText    string // 升级后立即下发的提示（文本帧）
	closeCode   int    // 输出下发完延迟 300ms 后的关闭码（0=不主动关）
	closeReason string
	urlPath     string // 收到的请求路径（含 query）
}

func newMockTermServer(t *testing.T, m *mockTermServer) *mockTermServer {
	t.Helper()
	m.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m.mu.Lock()
		m.urlPath = r.URL.String()
		if user, pass, ok := r.BasicAuth(); ok {
			m.authUser, m.authPass = user, pass
		}
		m.mu.Unlock()
		ws, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer ws.CloseNow()
		ctx := r.Context()
		if m.hintText != "" {
			_ = ws.Write(ctx, websocket.MessageText, []byte(m.hintText))
		}
		if len(m.output) > 0 {
			_ = ws.Write(ctx, websocket.MessageBinary, m.output)
		}
		// 读侧 goroutine：持续记录上行帧
		readDone := make(chan struct{})
		go func() {
			defer close(readDone)
			for {
				mt, data, err := ws.Read(ctx)
				if err != nil {
					return
				}
				m.mu.Lock()
				if mt == websocket.MessageText {
					m.textFrames = append(m.textFrames, string(data))
					var f resizeFrame
					if json.Unmarshal(data, &f) == nil && f.Type == "resize" {
						m.resizeCols, m.resizeRows = f.Cols, f.Rows
					}
				} else {
					buf := make([]byte, len(data))
					copy(buf, data)
					m.binaryFrms = append(m.binaryFrms, buf)
				}
				m.mu.Unlock()
			}
		}()
		if m.closeCode != 0 {
			// 留时间让客户端把 stdin/resize 发上来并记录
			time.Sleep(300 * time.Millisecond)
			_ = ws.Close(websocket.StatusCode(m.closeCode), m.closeReason)
			return
		}
		<-readDone
	}))
	t.Cleanup(m.srv.Close)
	m.srv.URL = "ws" + strings.TrimPrefix(m.srv.URL, "http") // http→ws
	return m
}

func (m *mockTermServer) frames() (texts []string, bins [][]byte, cols, rows int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string{}, m.textFrames...),
		append([][]byte{}, m.binaryFrms...), m.resizeCols, m.resizeRows
}

// Dial 走完整链路（二进制输出→onData、stdin 文本帧、resize JSON、服务端关闭→exit）
func TestDialFullDuplex(t *testing.T) {
	m := &mockTermServer{output: []byte("hello term\r\n"), closeCode: 1001, closeReason: "exec exited"}
	srv := newMockTermServer(t, m)

	var got []byte
	exitCh := make(chan int, 1)
	sess, err := Dial(srv.srv.URL, "", "admin", "myt", 100, 30,
		func(data []byte) { got = append(got, data...) },
		func(code int) { exitCh <- code })
	if err != nil {
		t.Fatalf("Dial 失败: %v", err)
	}
	defer sess.Close()

	if err := sess.Write([]byte("ls -la\n")); err != nil {
		t.Fatalf("Write 失败: %v", err)
	}
	if err := sess.Resize(120, 40); err != nil {
		t.Fatalf("Resize 失败: %v", err)
	}

	select {
	case code := <-exitCh:
		if code != 0 {
			t.Errorf("exit code = %d, want 0（1001 正常关闭）", code)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("未收到 exit 回调")
	}
	if string(got) != "hello term\r\n" {
		t.Errorf("onData = %q, want %q", got, "hello term\r\n")
	}
	texts, bins, cols, rows := srv.frames()
	if len(texts) < 2 || texts[0] != "ls -la\n" {
		t.Errorf("上行文本帧 = %v, want 首帧 %q（raw stdin）", texts, "ls -la\n")
	}
	resizeSeen := false
	for _, s := range texts {
		if strings.Contains(s, `"type":"resize"`) {
			resizeSeen = true
		}
	}
	if !resizeSeen {
		t.Errorf("上行缺少 resize 控制帧: %v", texts)
	}
	if cols != 120 || rows != 40 {
		t.Errorf("服务端解析 resize = %dx%d, want 120x40", cols, rows)
	}
	if len(bins) != 0 {
		t.Errorf("不应有二进制上行帧: %v", bins)
	}
	if !strings.Contains(m.urlPath, "cols=100") || !strings.Contains(m.urlPath, "rows=30") {
		t.Errorf("URL 缺少初始尺寸 query: %q", m.urlPath)
	}
}

// target 参数拼 URL：target=android 追加 query；空串保持原 URL 不变
func TestDialTargetParam(t *testing.T) {
	m := &mockTermServer{}
	srv := newMockTermServer(t, m)

	sess, err := Dial(srv.srv.URL, "android", "", "", 80, 24, func([]byte) {}, func(int) {})
	if err != nil {
		t.Fatalf("Dial(target=android) 失败: %v", err)
	}
	sess.Close()
	if !strings.Contains(m.urlPath, "target=android&cols=80&rows=24") {
		t.Errorf("URL 应含 target=android 且接初始尺寸 query: %q", m.urlPath)
	}

	// 空串：不带 target 参数（SDK 默认容器 shell）
	m2 := &mockTermServer{}
	srv2 := newMockTermServer(t, m2)
	sess2, err := Dial(srv2.srv.URL, "", "", "", 80, 24, func([]byte) {}, func(int) {})
	if err != nil {
		t.Fatalf("Dial(target=\"\") 失败: %v", err)
	}
	sess2.Close()
	if strings.Contains(m2.urlPath, "target=") {
		t.Errorf("target 为空串不应带 query 参数: %q", m2.urlPath)
	}
}

// Basic Auth 头正确携带
func TestDialSendsBasicAuth(t *testing.T) {
	m := &mockTermServer{}
	srv := newMockTermServer(t, m)
	sess, err := Dial(srv.srv.URL, "", "admin", "myt", 0, 0, func([]byte) {}, func(int) {})
	if err != nil {
		t.Fatalf("Dial 失败: %v", err)
	}
	sess.Close()
	if m.authUser != "admin" || m.authPass != "myt" {
		t.Errorf("Basic Auth = %q/%q, want admin/myt", m.authUser, m.authPass)
	}
	if strings.Contains(m.urlPath, "cols=") {
		t.Errorf("0 尺寸不应带 query: %q", m.urlPath)
	}
}

// 服务端 1008 关闭（通道/实例错误）→ exit code 1
func TestClose1008MapsToErrorExit(t *testing.T) {
	m := &mockTermServer{hintText: "终端通道自检失败", closeCode: 1008, closeReason: "自检失败"}
	srv := newMockTermServer(t, m)
	exitCh := make(chan int, 1)
	sess, err := Dial(srv.srv.URL, "", "", "", 0, 0, func([]byte) {}, func(code int) { exitCh <- code })
	if err != nil {
		t.Fatalf("Dial 失败: %v", err)
	}
	defer sess.Close()
	select {
	case code := <-exitCh:
		if code != 1 {
			t.Errorf("exit code = %d, want 1（1008 映射异常退出）", code)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("未收到 exit 回调")
	}
}

// 网络断开（服务端直接断 TCP，1006 语义）→ exit code 1
func TestAbruptDropMapsToErrorExit(t *testing.T) {
	m := &mockTermServer{output: []byte("partial")}
	var once sync.Once
	var conns []*websocket.Conn
	var mu sync.Mutex
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		mu.Lock()
		conns = append(conns, ws)
		mu.Unlock()
		_ = ws.Write(r.Context(), websocket.MessageBinary, m.output)
		<-r.Context().Done() // 阻塞至连接被强制断开
	})
	srv := httptest.NewServer(h)
	defer srv.Close()
	kill := func() {
		once.Do(func() {
			mu.Lock()
			for _, c := range conns {
				_ = c.CloseNow() // 直接断 TCP（客户端读侧得 1006）
			}
			mu.Unlock()
			srv.CloseClientConnections()
		})
	}

	exitCh := make(chan int, 1)
	sess, err := Dial("ws"+strings.TrimPrefix(srv.URL, "http"), "", "", "", 0, 0,
		func([]byte) {}, func(code int) { exitCh <- code })
	if err != nil {
		t.Fatalf("Dial 失败: %v", err)
	}
	defer sess.Close()
	kill()
	select {
	case code := <-exitCh:
		if code != 1 {
			t.Errorf("exit code = %d, want 1（1006 网络断）", code)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("未收到 exit 回调")
	}
}

// 升级前 401 → 明确错误（含 message）
func TestDial401(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"code":401,"message":"unauthorized","data":null}`))
	}))
	defer srv.Close()
	_, err := Dial("ws"+strings.TrimPrefix(srv.URL, "http"), "", "", "", 0, 0, func([]byte) {}, func(int) {})
	if err == nil {
		t.Fatal("401 应返回错误")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("错误信息应含 401: %v", err)
	}
}

// 升级前 501（终端功能关闭）→ 明确错误
func TestDial501(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotImplemented)
		_, _ = w.Write([]byte(`{"code":501,"message":"终端功能未启用（config terminal.enabled=false）","data":null}`))
	}))
	defer srv.Close()
	_, err := Dial("ws"+strings.TrimPrefix(srv.URL, "http"), "", "", "", 0, 0, func([]byte) {}, func(int) {})
	if err == nil {
		t.Fatal("501 应返回错误")
	}
	if !strings.Contains(err.Error(), "501") {
		t.Errorf("错误信息应含 501: %v", err)
	}
}

// Manager：Open/Write/Resize/Close 全链路 + 未知会话报错
func TestManagerLifecycle(t *testing.T) {
	m := &mockTermServer{}
	srv := newMockTermServer(t, m)
	var dataCh = make(chan []byte, 4)
	var exitCh = make(chan int, 1)
	mgr := NewManager(
		func(id string, data []byte) { dataCh <- data },
		func(id string, code int) { exitCh <- code },
	)
	id, err := mgr.Open(srv.srv.URL, "", "", "", 80, 24)
	if err != nil {
		t.Fatalf("Open 失败: %v", err)
	}
	if !strings.HasPrefix(id, "v4t") {
		t.Errorf("会话 ID = %q, want v4t 前缀", id)
	}
	if err := mgr.Write(id, []byte("echo hi\n")); err != nil {
		t.Fatalf("Write 失败: %v", err)
	}
	if err := mgr.Resize(id, 90, 25); err != nil {
		t.Fatalf("Resize 失败: %v", err)
	}
	select {
	case d := <-dataCh:
		t.Errorf("无预置输出却收到 %q", d)
	case <-time.After(200 * time.Millisecond):
	}
	if err := mgr.Close(id); err != nil {
		t.Fatalf("Close 失败: %v", err)
	}
	if err := mgr.Write("nope", []byte("x")); err == nil {
		t.Error("未知会话 Write 应报错")
	}
	if err := mgr.Resize("nope", 1, 1); err == nil {
		t.Error("未知会话 Resize 应报错")
	}
	// Close 幂等（会话已删表，静默）
	if err := mgr.Close(id); err != nil {
		t.Errorf("重复 Close 应幂等: %v", err)
	}
}
