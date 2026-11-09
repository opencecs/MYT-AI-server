package sdkapi

import (
	"strings"
	"testing"
)

// TestV2RewriteCreate 验证 8000 POST /androidV2 → 81 create 的参数翻译
func TestV2RewriteCreate(t *testing.T) {
	body := `{"name":"r713c8dcdf97655bc68171f891e171ae_1_1","imageUrl":"registry.cn-guangzhou.aliyuncs.com/mytos/dobox:Q12_gms_202510101644","dns":"223.5.5.5","indexNum":1,"sandboxSize":"","doboxWidth":"720","doboxHeight":"1280","doboxDpi":"320","start":true,"enforce":false,"compatMode":"0"}`
	method, path, newBody, ok, err := v2Rewrite("10.10.9.153", "POST", "/androidV2", []byte(body))
	if err != nil || !ok {
		t.Fatalf("rewrite failed: ok=%v err=%v", ok, err)
	}
	if method != "POST" {
		t.Fatalf("method = %s, want POST", method)
	}
	if !strings.Contains(path, "/dc_api/v1/create/10.10.9.153/1/r713c8dcdf97655bc68171f891e171ae_1_1") {
		t.Fatalf("path unexpected: %s", path)
	}
	// start 参数已移除：81 dc create 无此参数（被 mytsdk 忽略）——
	// 「创建后开机」由前端显式调 /android/start（壳子同款）
	for _, want := range []string{"image_addr=", "dns=223.5.5.5", "resolution=0"} {
		if !strings.Contains(path, want) {
			t.Fatalf("path missing %s: %s", want, path)
		}
	}
	// 字符串分辨率非预设值 → 自定义模式 2
	body2 := `{"name":"x","indexNum":"3","doboxWidth":"1152","doboxHeight":"2048","doboxDpi":"240"}`
	_, path2, _, ok2, err2 := v2Rewrite("ip", "POST", "/androidV2", []byte(body2))
	if err2 != nil || !ok2 {
		t.Fatalf("rewrite2 failed: %v", err2)
	}
	if !strings.Contains(path2, "resolution=2") || !strings.Contains(path2, "width=1152") {
		t.Fatalf("custom resolution unexpected: %s", path2)
	}
	if newBody != nil {
		t.Fatalf("create body should be nil, got %s", newBody)
	}
	// 空 name 应报错
	_, _, _, _, err = v2Rewrite("ip", "POST", "/androidV2", []byte(`{"imageUrl":"x"}`))
	if err == nil || !strings.Contains(err.Error(), "name") {
		t.Fatalf("empty name should error, got %v", err)
	}
	// 空 body 应报错
	_, _, _, _, err = v2Rewrite("ip", "POST", "/androidV2", nil)
	if err == nil {
		t.Fatalf("nil body should error")
	}
}

// TestV2BusinessError 验证 81 业务码拦截：HTTP 200 + code≠200 必须报错
// （否则删除/重置等失败被当成功——假成功让前端毫无提示）。
func TestV2BusinessError(t *testing.T) {
	// 业务失败：remove 对不存在的容器（实测响应形态）
	err := v2BusinessError([]byte(`{"code":2,"error":null,"message":"xx容器不存在!"}`))
	if err == nil {
		t.Fatalf("code=2 should be business error")
	}
	if !strings.Contains(err.Error(), "容器不存在") {
		t.Fatalf("error should carry message, got %v", err)
	}
	// 业务失败：msg 字段变体
	if err = v2BusinessError([]byte(`{"code":500,"msg":"internal error"}`)); err == nil {
		t.Fatalf("code=500 (msg) should be business error")
	}
	// 成功：code=200 / code=0
	if err = v2BusinessError([]byte(`{"code":200,"data":[]}`)); err != nil {
		t.Fatalf("code=200 should pass, got %v", err)
	}
	if err = v2BusinessError([]byte(`{"code":0,"data":[]}`)); err != nil {
		t.Fatalf("code=0 should pass, got %v", err)
	}
	// 非 dc_api 响应（2375 裸 JSON / 非对象）不误伤
	if err = v2BusinessError([]byte(`[1,2,3]`)); err != nil {
		t.Fatalf("array JSON should pass, got %v", err)
	}
	if err = v2BusinessError([]byte(`{"ContainersDeleted":["a"]}`)); err != nil {
		t.Fatalf("docker-style JSON should pass, got %v", err)
	}
	if err = v2BusinessError([]byte(`not json`)); err != nil {
		t.Fatalf("non-JSON should pass, got %v", err)
	}
}

