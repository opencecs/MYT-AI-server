package v4host

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// v4Server 起一个假的 tobox_player REST 服务（记录请求，按 path 返回固定响应）
type v4Server struct {
	srv       *httptest.Server
	paths     []string
	bodies    []string
	authOK    bool
	responder func(path string, body string) (int, string)
}

func newV4Server(t *testing.T, responder func(path, body string) (int, string)) *v4Server {
	t.Helper()
	s := &v4Server{responder: responder}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		s.authOK = ok && user == DefaultUser && pass == DefaultPassword
		raw, _ := io.ReadAll(r.Body)
		s.paths = append(s.paths, r.Method+" "+r.URL.Path)
		s.bodies = append(s.bodies, string(raw))
		status, body := s.responder(r.URL.Path, string(raw))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(s.srv.Close)
	return s
}

// host 返回 ip:port（去掉 scheme）
func (s *v4Server) host() string {
	u := s.srv.URL
	return u[strings.LastIndex(u, "/")+1:]
}

// 列表：{code:0,data:{instances,total}} → 正确解析 snake_case 字段
func TestInstanceClientList(t *testing.T) {
	srv := newV4Server(t, func(path, _ string) (int, string) {
		if path != "/android" {
			t.Errorf("path = %q, want /android", path)
		}
		return 200, `{"code":0,"message":"ok","data":{"total":1,"instances":[
			{"backend":"coloros","id":"coloros-1","name":"coloros-1","variant":"V1","state":"running",
			 "running":true,"pid":19,"adb_port":5556,"cast_port":12002,"control_port":28061,
			 "cpu_cores":6,"memory_mb":8192,"disk_mb":8192,"disk_used_mb":7806}]}}`
	})
	c := NewInstanceClient(srv.host(), "", "")
	res, err := c.List(context.Background())
	if err != nil {
		t.Fatalf("List 失败: %v", err)
	}
	if !srv.authOK {
		t.Error("未携带默认 Basic Auth（admin/myt）")
	}
	if res.Total != 1 || len(res.Instances) != 1 {
		t.Fatalf("列表解析不符: %+v", res)
	}
	it := res.Instances[0]
	if it.Backend != "coloros" || it.ID != "coloros-1" || it.State != "running" || !it.Running {
		t.Errorf("实例字段不符: %+v", it)
	}
	if it.CastPort != 12002 || it.MemoryMB != 8192 || it.DiskUsedMB != 7806 {
		t.Errorf("snake_case 字段未正确映射: %+v", it)
	}
}

// 空列表容错（data.instances 为 null）
func TestInstanceClientListEmpty(t *testing.T) {
	srv := newV4Server(t, func(_, _ string) (int, string) {
		return 200, `{"code":0,"message":"ok","data":{"total":0,"instances":null}}`
	})
	res, err := NewInstanceClient(srv.host(), "", "").List(context.Background())
	if err != nil {
		t.Fatalf("List 失败: %v", err)
	}
	if res.Instances == nil || len(res.Instances) != 0 {
		t.Errorf("空列表应归一为长度 0 的非 nil 切片: %+v", res.Instances)
	}
}

// 启停删重置：body 必须是 {id, backend?}（backend 非空才带）
func TestInstanceClientOps(t *testing.T) {
	srv := newV4Server(t, func(_, _ string) (int, string) {
		return 200, `{"code":0,"message":"ok","data":null}`
	})
	c := NewInstanceClient(srv.host(), "", "")
	ctx := context.Background()

	if err := c.Start(ctx, InstanceRef{ID: "i1", Backend: "be"}); err != nil {
		t.Fatalf("Start 失败: %v", err)
	}
	if err := c.Stop(ctx, InstanceRef{ID: "i1"}); err != nil {
		t.Fatalf("Stop 失败: %v", err)
	}
	if err := c.Delete(ctx, InstanceRef{ID: "i1", Backend: "be"}); err != nil {
		t.Fatalf("Delete 失败: %v", err)
	}
	if err := c.Reset(ctx, InstanceRef{ID: "i1", Backend: "be"}, true); err != nil {
		t.Fatalf("Reset 失败: %v", err)
	}

	wantPaths := []string{"POST /android/start", "POST /android/stop", "POST /android/delete", "POST /android/reset"}
	if len(srv.paths) != len(wantPaths) {
		t.Fatalf("请求次数 = %d, want %d: %v", len(srv.paths), len(wantPaths), srv.paths)
	}
	for i, want := range wantPaths {
		if srv.paths[i] != want {
			t.Errorf("第 %d 个请求 = %q, want %q", i, srv.paths[i], want)
		}
	}

	// body 校验
	var startBody map[string]any
	if err := json.Unmarshal([]byte(srv.bodies[0]), &startBody); err != nil {
		t.Fatalf("start body 非法 JSON: %v", err)
	}
	if startBody["id"] != "i1" || startBody["backend"] != "be" {
		t.Errorf("start body 不符: %v", startBody)
	}
	var stopBody map[string]any
	_ = json.Unmarshal([]byte(srv.bodies[1]), &stopBody)
	if stopBody["id"] != "i1" {
		t.Errorf("stop body 不符: %v", stopBody)
	}
	if _, ok := stopBody["backend"]; ok {
		t.Errorf("backend 为空时不应出现在 body: %v", stopBody)
	}
	var resetBody map[string]any
	_ = json.Unmarshal([]byte(srv.bodies[3]), &resetBody)
	if resetBody["wipe_data"] != true {
		t.Errorf("reset body 应带 wipe_data=true: %v", resetBody)
	}
}

// 业务错误（HTTP 200 + code!=0）要透出 message
func TestInstanceClientBusinessError(t *testing.T) {
	srv := newV4Server(t, func(_, _ string) (int, string) {
		return 200, `{"code":400,"message":"未知后端 \"zzz\"","data":null}`
	})
	err := NewInstanceClient(srv.host(), "", "").Stop(context.Background(), InstanceRef{ID: "i1"})
	if err == nil {
		t.Fatal("code!=0 应返回错误")
	}
	if !strings.Contains(err.Error(), "未知后端") {
		t.Errorf("错误信息未透出 message: %v", err)
	}
}

// 401：明确提示认证失败（Basic Auth 配置问题）
func TestInstanceClient401(t *testing.T) {
	srv := newV4Server(t, func(_, _ string) (int, string) {
		return 401, `{"code":401,"message":"unauthorized","data":null}`
	})
	_, err := NewInstanceClient(srv.host(), "", "").List(context.Background())
	if err == nil {
		t.Fatal("401 应返回错误")
	}
	if !strings.Contains(err.Error(), "认证失败") {
		t.Errorf("401 错误信息应提示认证失败: %v", err)
	}
}

// 空 id 防护
func TestInstanceClientEmptyID(t *testing.T) {
	srv := newV4Server(t, func(_, _ string) (int, string) { return 200, `{"code":0}` })
	if err := NewInstanceClient(srv.host(), "", "").Start(context.Background(), InstanceRef{}); err == nil {
		t.Fatal("空 id 应返回错误")
	}
}

// host 归一：纯 IP 补默认端口、带端口不改、带 scheme 不改
func TestNewInstanceClientHost(t *testing.T) {
	cases := []struct{ in, want string }{
		{"10.10.2.142", "http://10.10.2.142:8088"},
		{"10.10.2.142:9999", "http://10.10.2.142:9999"},
		{"http://10.10.2.142:8088/", "http://10.10.2.142:8088"},
	}
	for _, c := range cases {
		if got := NewInstanceClient(c.in, "", "").baseURL; got != c.want {
			t.Errorf("NewInstanceClient(%q).baseURL = %q, want %q", c.in, got, c.want)
		}
	}
}

// SetClipboard：body {id, text, paste, backend?}，空文本防护
func TestInstanceClientSetClipboard(t *testing.T) {
	srv := newV4Server(t, func(path, _ string) (int, string) {
		if path != "/android/clipboard" {
			t.Errorf("path = %q, want /android/clipboard", path)
		}
		return 200, `{"code":0,"message":"ok","data":null}`
	})
	c := NewInstanceClient(srv.host(), "", "")
	ctx := context.Background()

	if err := c.SetClipboard(ctx, InstanceRef{ID: "i1", Backend: "be"}, "hello 世界", true); err != nil {
		t.Fatalf("SetClipboard 失败: %v", err)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(srv.bodies[0]), &body); err != nil {
		t.Fatalf("body 非法 JSON: %v", err)
	}
	if body["id"] != "i1" || body["text"] != "hello 世界" || body["paste"] != true || body["backend"] != "be" {
		t.Errorf("clipboard body 不符: %v", body)
	}
	// backend 为空：不带 backend 字段
	srv.bodies = nil
	if err := c.SetClipboard(ctx, InstanceRef{ID: "i1"}, "x", false); err != nil {
		t.Fatalf("SetClipboard(无 backend) 失败: %v", err)
	}
	var body2 map[string]any
	if err := json.Unmarshal([]byte(srv.bodies[0]), &body2); err != nil {
		t.Fatalf("body 非法 JSON: %v", err)
	}
	if _, ok := body2["backend"]; ok {
		t.Errorf("backend 为空时不应出现在 body: %v", body2)
	}
	if body2["paste"] != false {
		t.Errorf("paste=false 应显式下发: %v", body2)
	}
	// 空文本防护
	if err := c.SetClipboard(ctx, InstanceRef{ID: "i1"}, "", true); err == nil {
		t.Error("空文本应返回错误")
	}
	// 空 id 防护
	if err := c.SetClipboard(ctx, InstanceRef{}, "x", true); err == nil {
		t.Error("空 id 应返回错误")
	}
}

// SetClipboard 错误透出：404（未知实例）与 500
func TestInstanceClientSetClipboardErrors(t *testing.T) {
	srv := newV4Server(t, func(path, _ string) (int, string) {
		return 404, `{"code":404,"message":"实例不存在","data":null}`
	})
	err := NewInstanceClient(srv.host(), "", "").SetClipboard(context.Background(), InstanceRef{ID: "nope"}, "t", true)
	if err == nil || !strings.Contains(err.Error(), "实例不存在") || !strings.Contains(err.Error(), "404") {
		t.Errorf("404 错误应带状态码与服务端 message: %v", err)
	}

	srv2 := newV4Server(t, func(path, _ string) (int, string) {
		return 500, `{"code":500,"message":"内部错误","data":null}`
	})
	err = NewInstanceClient(srv2.host(), "", "").SetClipboard(context.Background(), InstanceRef{ID: "i1"}, "t", false)
	if err == nil || !strings.Contains(err.Error(), "内部错误") || !strings.Contains(err.Error(), "500") {
		t.Errorf("500 错误应带状态码与服务端 message: %v", err)
	}
}

// Configure：body {id, profile{...}, backend?}
func TestInstanceClientConfigure(t *testing.T) {
	srv := newV4Server(t, func(path, _ string) (int, string) {
		if path != "/android/configure" {
			t.Errorf("path = %q, want /android/configure", path)
		}
		return 200, `{"code":0,"message":"ok","data":null}`
	})
	c := NewInstanceClient(srv.host(), "", "")
	ctx := context.Background()
	p := DeviceProfile{Latitude: 31.23, Longitude: 121.47, Country: "CN",
		OperatorNumeric: "46000", OperatorName: "CMCC", CellID: "12345", PhoneNumber: "13800000000", Root: true}
	if err := c.Configure(ctx, InstanceRef{ID: "i1", Backend: "be"}, p); err != nil {
		t.Fatalf("Configure 失败: %v", err)
	}
	var body struct {
		ID      string        `json:"id"`
		Backend string        `json:"backend"`
		Profile DeviceProfile `json:"profile"`
	}
	if err := json.Unmarshal([]byte(srv.bodies[0]), &body); err != nil {
		t.Fatalf("body 非法 JSON: %v", err)
	}
	if body.ID != "i1" || body.Backend != "be" {
		t.Errorf("configure 顶层字段不符: %+v", body)
	}
	if body.Profile != p {
		t.Errorf("profile 不符: %+v want %+v", body.Profile, p)
	}
	// 空 id 防护
	if err := c.Configure(ctx, InstanceRef{}, p); err == nil {
		t.Error("空 id 应返回错误")
	}
}
