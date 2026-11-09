package v4host

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// pngBytes 一个最小合法 PNG 头（魔数 + IHDR 起始，仅用于魔数校验）
var pngBytes = append(append([]byte{}, pngMagic...), []byte("rest-of-png")...)

// shotServer 构造一个截图假服务，handler 自行决定响应
func shotServer(t *testing.T, handler http.HandlerFunc) *InstanceClient {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return NewInstanceClient(srv.URL, "admin", "myt")
}

// 成功：PNG 200，路径与 query 符合实测契约
func TestScreenshotSuccessPNG(t *testing.T) {
	var gotPath, gotQuery, gotUser, gotPass string
	c := shotServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		gotUser, gotPass, _ = r.BasicAuth()
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("X-Screenshot-Source", "cast")
		_, _ = w.Write(pngBytes)
	})

	data, ct, err := c.Screenshot(context.Background(), InstanceRef{ID: "coloros-1", Backend: "coloros"}, 0)
	if err != nil {
		t.Fatalf("Screenshot 失败: %v", err)
	}
	if gotPath != "/android/coloros-1/screenshot" {
		t.Errorf("请求路径 = %q, want /android/coloros-1/screenshot", gotPath)
	}
	if gotQuery != "backend=coloros" {
		t.Errorf("query = %q, want backend=coloros", gotQuery)
	}
	if gotUser != "admin" || gotPass != "myt" {
		t.Errorf("Basic Auth = %q/%q, want admin/myt", gotUser, gotPass)
	}
	if ct != "image/png" {
		t.Errorf("Content-Type = %q, want image/png", ct)
	}
	if string(data) != string(pngBytes) {
		t.Errorf("返回字节与响应体不一致: %d vs %d 字节", len(data), len(pngBytes))
	}
}

// 成功：JPEG 也接受（服务端将来放开 format 时零改动）
func TestScreenshotSuccessJPEG(t *testing.T) {
	jpeg := append(append([]byte{}, jpegMagic...), []byte("jpeg-body")...)
	c := shotServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.RawQuery != "" {
			t.Errorf("Backend 为空时不应带 backend query，实际 %q", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(jpeg)
	})
	data, ct, err := c.Screenshot(context.Background(), InstanceRef{ID: "origin-1"}, 0)
	if err != nil {
		t.Fatalf("Screenshot 失败: %v", err)
	}
	if ct != "image/jpeg" {
		t.Errorf("Content-Type = %q, want image/jpeg", ct)
	}
	if string(data) != string(jpeg) {
		t.Error("返回字节与响应体不一致")
	}
}

// 404：实例不存在，错误里要带服务端 message
func TestScreenshotNotFound(t *testing.T) {
	c := shotServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"code":404,"message":"实例 \"nope\" 不存在（后端 coloros）","data":null}`))
	})
	_, _, err := c.Screenshot(context.Background(), InstanceRef{ID: "nope", Backend: "coloros"}, 0)
	if err == nil {
		t.Fatal("404 应返回错误")
	}
	if !strings.Contains(err.Error(), "404") || !strings.Contains(err.Error(), "不存在") {
		t.Errorf("错误信息应含状态码与服务端 message，实际: %v", err)
	}
}

// 503：实例未 running（实测停止态/容器缺失的返回），错误里带 X-Screenshot-Error
func TestScreenshotServiceUnavailable(t *testing.T) {
	c := shotServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Screenshot-Error", "exit status 1: No such container: tebox-1")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"code":503,"message":"adb screencap 失败（guest 未就绪）","data":null}`))
	})
	_, _, err := c.Screenshot(context.Background(), InstanceRef{ID: "origin-1", Backend: "default"}, 0)
	if err == nil {
		t.Fatal("503 应返回错误")
	}
	msg := err.Error()
	if !strings.Contains(msg, "503") || !strings.Contains(msg, "未 running") {
		t.Errorf("错误信息应含 503 与原因说明，实际: %v", err)
	}
	if !strings.Contains(msg, "No such container") {
		t.Errorf("错误信息应透出 X-Screenshot-Error，实际: %v", err)
	}
}

// 429：单实例并发超限
func TestScreenshotTooManyRequests(t *testing.T) {
	c := shotServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"code":429,"message":"截图进行中","data":null}`))
	})
	_, _, err := c.Screenshot(context.Background(), InstanceRef{ID: "x"}, 0)
	if err == nil || !strings.Contains(err.Error(), "429") {
		t.Fatalf("429 应返回并发超限错误，实际: %v", err)
	}
}

// 500：服务端内部错误
func TestScreenshotInternalError(t *testing.T) {
	c := shotServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"code":500,"message":"ffmpeg panic","data":null}`))
	})
	_, _, err := c.Screenshot(context.Background(), InstanceRef{ID: "x"}, 0)
	if err == nil || !strings.Contains(err.Error(), "500") {
		t.Fatalf("500 应返回错误，实际: %v", err)
	}
}

// 超时：服务端挂住 → 必须按 screenshotTimeout 收敛（不能永久阻塞轮询）
func TestScreenshotTimeout(t *testing.T) {
	c := shotServer(t, func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done() // 客户端断开（超时）才返回
	})
	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	_, _, err := c.Screenshot(ctx, InstanceRef{ID: "x"}, 0)
	if err == nil {
		t.Fatal("超时应返回错误")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("超时未及时收敛: 耗时 %v", elapsed)
	}
}

// 200 但响应体是 JSON 错误壳（服务端异常）→ 必须按非图片拒绝，不能当图返回
func TestScreenshotRejectsNonImageBody(t *testing.T) {
	c := shotServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{}}`))
	})
	_, _, err := c.Screenshot(context.Background(), InstanceRef{ID: "x"}, 0)
	if err == nil || !strings.Contains(err.Error(), "不是有效图片") {
		t.Fatalf("非图片响应应报错，实际: %v", err)
	}
}

// 空 id：不发请求直接报错
func TestScreenshotEmptyID(t *testing.T) {
	c := NewInstanceClient("127.0.0.1:1", "", "")
	if _, _, err := c.Screenshot(context.Background(), InstanceRef{}, 0); err == nil {
		t.Fatal("空 id 应返回错误")
	}
}

// width 参数：>0 时带 width=<px>（服务端按源比例缩放输出尺寸），=0 时不带该参数
func TestScreenshotWidthParam(t *testing.T) {
	var gotQuery string
	c := shotServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(pngBytes)
	})

	if _, _, err := c.Screenshot(context.Background(), InstanceRef{ID: "i1", Backend: "be"}, 480); err != nil {
		t.Fatalf("Screenshot 失败: %v", err)
	}
	if gotQuery != "backend=be&width=480" {
		t.Errorf("query = %q, want backend=be&width=480", gotQuery)
	}

	// 不缩放：完全不带 width
	if _, _, err := c.Screenshot(context.Background(), InstanceRef{ID: "i1"}, 0); err != nil {
		t.Fatalf("Screenshot 失败: %v", err)
	}
	if gotQuery != "" {
		t.Errorf("width=0 时不应带任何 query，实际 %q", gotQuery)
	}
}
