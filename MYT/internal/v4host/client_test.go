package v4host

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// healthServer 启动一个 /health 假服务（handler 决定响应）
func healthServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv
}

// 真实契约：/health 只返回 {"status":"ok"}，无设备标识 → DeviceID 恒为 IP
func TestHealthSuccess(t *testing.T) {
	srv := healthServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			t.Errorf("请求路径 = %q, want /health", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	info, err := healthAt(context.Background(), srv.URL+"/health", "192.168.1.10")
	if err != nil {
		t.Fatalf("healthAt 失败: %v", err)
	}
	if info.DeviceID != "192.168.1.10" {
		t.Errorf("DeviceID = %q, want IP（V4 无设备标识字段）", info.DeviceID)
	}
	if !info.DeviceIDFromIP {
		t.Error("DeviceIDFromIP 应恒为 true（IP 作 key）")
	}
	if info.DeviceName != "" || info.Model != "" {
		t.Errorf("契约响应无 name/model，不应解析出值: name=%q model=%q", info.DeviceName, info.Model)
	}
	if !strings.Contains(info.RawBody, "ok") {
		t.Errorf("RawBody 未保留原始响应: %q", info.RawBody)
	}
}

// 字段容错：不再猜测 deviceId/id/serial（V4 契约已确认无此字段），IP 恒为 key；
// 显示名/型号仍做容错查找（服务端将来补字段时零改动生效）。
func TestHealthFieldTolerance(t *testing.T) {
	cases := []struct {
		name     string
		body     string
		wantName string
		wantType string
	}{
		{"status_only", `{"status":"ok"}`, "", ""},
		// 早期误猜的 deviceId/serial 一律忽略，绝不当作设备 key
		{"ignores_device_id", `{"device_id":"S-1","device_name":"Q1_V4"}`, "Q1_V4", ""},
		{"nested_name", `{"code":0,"data":{"name":"M1_V4","model":"c1"}}`, "M1_V4", "c1"},
		{"serial_numeric", `{"serial":12345678,"model":"c1"}`, "c1", "c1"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := healthServer(t, func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(c.body))
			})
			info, err := healthAt(context.Background(), srv.URL+"/health", "192.168.1.20")
			if err != nil {
				t.Fatalf("healthAt 失败: %v", err)
			}
			if info.DeviceID != "192.168.1.20" {
				t.Errorf("DeviceID = %q, want IP 192.168.1.20", info.DeviceID)
			}
			if !info.DeviceIDFromIP {
				t.Error("DeviceIDFromIP 应恒为 true")
			}
			if info.DeviceName != c.wantName {
				t.Errorf("DeviceName = %q, want %q", info.DeviceName, c.wantName)
			}
			if info.Model != c.wantType {
				t.Errorf("Model = %q, want %q", info.Model, c.wantType)
			}
		})
	}
}

func TestHealthFailures(t *testing.T) {
	t.Run("http_500", func(t *testing.T) {
		srv := healthServer(t, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":"boom"}`))
		})
		if _, err := healthAt(context.Background(), srv.URL+"/health", "10.0.0.1"); err == nil {
			t.Fatal("HTTP 500 应返回错误")
		}
	})

	t.Run("non_json", func(t *testing.T) {
		srv := healthServer(t, func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("OK"))
		})
		if _, err := healthAt(context.Background(), srv.URL+"/health", "10.0.0.2"); err == nil {
			t.Fatal("非 JSON 响应应返回错误")
		}
	})

	t.Run("empty_body", func(t *testing.T) {
		srv := healthServer(t, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
		if _, err := healthAt(context.Background(), srv.URL+"/health", "10.0.0.3"); err == nil {
			t.Fatal("空响应体应返回错误")
		}
	})

	t.Run("json_array", func(t *testing.T) {
		srv := healthServer(t, func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`[1,2,3]`))
		})
		if _, err := healthAt(context.Background(), srv.URL+"/health", "10.0.0.4"); err == nil {
			t.Fatal("JSON 数组（非对象）应返回错误")
		}
	})

	t.Run("timeout", func(t *testing.T) {
		srv := healthServer(t, func(w http.ResponseWriter, r *http.Request) {
			select {
			case <-time.After(300 * time.Millisecond):
			case <-r.Context().Done():
			}
		})
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()
		start := time.Now()
		if _, err := healthAt(ctx, srv.URL+"/health", "10.0.0.5"); err == nil {
			t.Fatal("超时应返回错误")
		}
		if elapsed := time.Since(start); elapsed > 250*time.Millisecond {
			t.Errorf("超时未及时返回: %v", elapsed)
		}
	})
}

// truncateBody 只保留前 512 字节
func TestTruncateBody(t *testing.T) {
	long := strings.Repeat("a", rawLogLimit+10)
	got := truncateBody([]byte(long))
	if !strings.HasPrefix(got, strings.Repeat("a", rawLogLimit)) {
		t.Error("截断内容不符")
	}
	if !strings.Contains(got, "truncated") {
		t.Error("截断应带标记")
	}
}
