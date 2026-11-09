package v4host

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// 创建实例：body 只带显式传的字段（omitempty），响应解析出 instance
func TestInstanceClientCreate(t *testing.T) {
	srv := newV4Server(t, func(path, _ string) (int, string) {
		if path != "/android/create" {
			t.Errorf("path = %q, want /android/create", path)
		}
		return 200, `{"code":0,"message":"ok","data":{"instance":{
			"backend":"default","id":"tebox-2","name":"my-phone","variant":"V1","state":"stopped",
			"running":false,"cpu_cores":4,"memory_mb":4096,"disk_mb":8192,"disk_used_mb":0}}}`
	})
	c := NewInstanceClient(srv.host(), "", "")
	res, err := c.Create(context.Background(), CreateRequest{
		Name: "my-phone", CPUCores: 4, MemoryMB: 4096, DiskMB: 8192,
		Backend: "default", Variant: "V1",
	})
	if err != nil {
		t.Fatalf("Create 失败: %v", err)
	}
	if !srv.authOK {
		t.Error("未携带默认 Basic Auth")
	}
	if res.Instance == nil || res.Instance.ID != "tebox-2" || res.Instance.State != "stopped" {
		t.Fatalf("创建结果不符: %+v", res.Instance)
	}

	var body map[string]any
	if err := json.Unmarshal([]byte(srv.bodies[0]), &body); err != nil {
		t.Fatalf("create body 非法 JSON: %v", err)
	}
	for _, k := range []string{"name", "cpu_cores", "memory_mb", "disk_mb", "backend", "variant"} {
		if _, ok := body[k]; !ok {
			t.Errorf("body 缺少字段 %q: %v", k, body)
		}
	}
	// 未指定的三件套路径不应出现（omitempty）
	for _, k := range []string{"system_image", "vendor_image", "initramfs_image"} {
		if _, ok := body[k]; ok {
			t.Errorf("未指定镜像路径时不应带 %q: %v", k, body)
		}
	}
}

// 全默认参数：body 为空对象（服务端按 4核/4096/8192 处理）
func TestInstanceClientCreateDefaults(t *testing.T) {
	srv := newV4Server(t, func(_, _ string) (int, string) {
		return 200, `{"code":0,"message":"ok","data":{"instance":{"backend":"default","id":"x","state":"stopped"}}}`
	})
	if _, err := NewInstanceClient(srv.host(), "", "").Create(context.Background(), CreateRequest{}); err != nil {
		t.Fatalf("Create 失败: %v", err)
	}
	if got := strings.TrimSpace(srv.bodies[0]); got != "{}" {
		t.Errorf("全默认参数 body = %q, want {}", got)
	}
}

// 创建失败：HTTP 400 + message 要透出
func TestInstanceClientCreateError(t *testing.T) {
	srv := newV4Server(t, func(_, _ string) (int, string) {
		return 400, `{"code":400,"message":"backend \"zzz\" not found","data":null}`
	})
	_, err := NewInstanceClient(srv.host(), "", "").Create(context.Background(), CreateRequest{Backend: "zzz"})
	if err == nil {
		t.Fatal("HTTP 400 应返回错误")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("错误信息未透出 message: %v", err)
	}
}

// 未返回 instance 视为失败（避免前端拿到空对象当成功）
func TestInstanceClientCreateNoInstance(t *testing.T) {
	srv := newV4Server(t, func(_, _ string) (int, string) {
		return 200, `{"code":0,"message":"ok","data":{}}`
	})
	if _, err := NewInstanceClient(srv.host(), "", "").Create(context.Background(), CreateRequest{}); err == nil {
		t.Fatal("未返回 instance 应报错")
	}
}

// 镜像列表：解析 variants + files + ready
func TestInstanceClientListImages(t *testing.T) {
	srv := newV4Server(t, func(path, _ string) (int, string) {
		if path != "/image/list" {
			t.Errorf("path = %q, want /image/list", path)
		}
		return 200, `{"code":0,"message":"ok","data":{"root":"/mmc/tebox/user-images","total":1,
			"variants":[{"variant":"V1","ready":true,"files":[
				{"name":"system.img","size":1024,"ready":true},
				{"name":"vendor.img","size":512,"ready":true},
				{"name":"initramfs.img","size":64,"ready":true}]}]}}`
	})
	res, err := NewInstanceClient(srv.host(), "", "").ListImages(context.Background())
	if err != nil {
		t.Fatalf("ListImages 失败: %v", err)
	}
	if res.Total != 1 || len(res.Variants) != 1 {
		t.Fatalf("镜像列表不符: %+v", res)
	}
	v := res.Variants[0]
	if v.Variant != "V1" || !v.Ready || len(v.Files) != 3 {
		t.Errorf("变体字段不符: %+v", v)
	}
	if v.Files[0].Name != "system.img" || v.Files[0].Size != 1024 {
		t.Errorf("文件字段不符: %+v", v.Files[0])
	}
}

// 空镜像列表容错（variants 为 null）
func TestInstanceClientListImagesEmpty(t *testing.T) {
	srv := newV4Server(t, func(_, _ string) (int, string) {
		return 200, `{"code":0,"message":"ok","data":{"root":"/mmc/tebox/user-images","variants":[],"total":0}}`
	})
	res, err := NewInstanceClient(srv.host(), "", "").ListImages(context.Background())
	if err != nil {
		t.Fatalf("ListImages 失败: %v", err)
	}
	if res.Variants == nil || len(res.Variants) != 0 {
		t.Errorf("空列表应归一为非 nil 空切片: %+v", res.Variants)
	}
}

// 后端列表：解析 name/address/status/default
func TestInstanceClientListBackends(t *testing.T) {
	srv := newV4Server(t, func(path, _ string) (int, string) {
		if path != "/backend/list" {
			t.Errorf("path = %q, want /backend/list", path)
		}
		return 200, `{"code":0,"message":"ok","data":{"backends":[
			{"name":"coloros","address":"127.0.0.1:50052","status":"connected","instanceCount":1,"default":false},
			{"name":"default","address":"127.0.0.1:50051","status":"connected","instanceCount":1,"default":true}]}}`
	})
	res, err := NewInstanceClient(srv.host(), "", "").ListBackends(context.Background())
	if err != nil {
		t.Fatalf("ListBackends 失败: %v", err)
	}
	if len(res.Backends) != 2 {
		t.Fatalf("后端数量不符: %+v", res.Backends)
	}
	if res.Backends[1].Name != "default" || !res.Backends[1].Default || res.Backends[1].Address != "127.0.0.1:50051" {
		t.Errorf("后端字段不符: %+v", res.Backends[1])
	}
}

// 媒体端口推导：50051→9901/9904/9905，50052→9911/9914/9915（10.10.2.142 实测）
func TestCastPortsForBackend(t *testing.T) {
	cases := []struct {
		address string
		want    CastPorts
	}{
		{"127.0.0.1:50051", CastPorts{Video: 9901, Audio: 9904, Ctrl: 9905}},
		{"127.0.0.1:50052", CastPorts{Video: 9911, Audio: 9914, Ctrl: 9915}},
		{"127.0.0.1:50053", CastPorts{Video: 9921, Audio: 9924, Ctrl: 9925}},
		// 地址缺失/非法：回落默认后端端口
		{"", CastPorts{Video: 9901, Audio: 9904, Ctrl: 9905}},
		{"127.0.0.1", CastPorts{Video: 9901, Audio: 9904, Ctrl: 9905}},
		{"127.0.0.1:abc", CastPorts{Video: 9901, Audio: 9904, Ctrl: 9905}},
		// 低于基准不产生负偏移
		{"127.0.0.1:50000", CastPorts{Video: 9901, Audio: 9904, Ctrl: 9905}},
	}
	for _, c := range cases {
		if got := CastPortsForBackend(c.address); got != c.want {
			t.Errorf("CastPortsForBackend(%q) = %+v, want %+v", c.address, got, c.want)
		}
	}
}

// ResolveCastPorts：按后端名命中 /backend/list；未命中回落默认端口
func TestResolveCastPorts(t *testing.T) {
	srv := newV4Server(t, func(_, _ string) (int, string) {
		return 200, `{"code":0,"message":"ok","data":{"backends":[
			{"name":"coloros","address":"127.0.0.1:50052"},
			{"name":"default","address":"127.0.0.1:50051"}]}}`
	})
	c := NewInstanceClient(srv.host(), "", "")
	ctx := context.Background()
	if got := c.ResolveCastPorts(ctx, "coloros"); got.Video != 9911 || got.Ctrl != 9915 || got.Audio != 9914 {
		t.Errorf("coloros 端口 = %+v, want 9911/9914/9915", got)
	}
	if got := c.ResolveCastPorts(ctx, "default"); got.Video != 9901 {
		t.Errorf("default 端口 = %+v, want 9901", got)
	}
	if got := c.ResolveCastPorts(ctx, "unknown"); got.Video != 9901 {
		t.Errorf("未知后端应回落默认端口, got %+v", got)
	}
	// backend 为空：不打 /backend/list（省一次探测）
	before := len(srv.paths)
	if got := c.ResolveCastPorts(ctx, ""); got.Video != 9901 {
		t.Errorf("空 backend 应返回默认端口, got %+v", got)
	}
	if len(srv.paths) != before {
		t.Error("空 backend 不应发起 /backend/list 请求")
	}
}
