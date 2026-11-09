package auth

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestParseLoginResponseSuccess(t *testing.T) {
	body := map[string]interface{}{
		"code": "200",
		"msg":  "OK",
		"data": map[string]string{
			"token": "abc123",
			"uname": "testuser",
			"uid":   "1001",
		},
	}
	data, _ := json.Marshal(body)

	result, err := parseLoginResponse(data)
	if err != nil {
		t.Fatalf("parseLoginResponse() error: %v", err)
	}
	if result.Code != "200" {
		t.Errorf("Code = %q, want %q", result.Code, "200")
	}
	if result.Token != "abc123" {
		t.Errorf("Token = %q, want %q", result.Token, "abc123")
	}
	if result.Uname != "testuser" {
		t.Errorf("Uname = %q, want %q", result.Uname, "testuser")
	}
	if result.Uid != "1001" {
		t.Errorf("Uid = %q, want %q", result.Uid, "1001")
	}
}

func TestParseLoginResponseNumericCode(t *testing.T) {
	body := map[string]interface{}{
		"code": 200,
		"msg":  "OK",
		"data": map[string]string{
			"token": "xyz",
			"uname": "u",
			"uid":   "1",
		},
	}
	data, _ := json.Marshal(body)

	result, err := parseLoginResponse(data)
	if err != nil {
		t.Fatalf("parseLoginResponse() error: %v", err)
	}
	if result.Code != "200" {
		t.Errorf("Code = %q, want %q", result.Code, "200")
	}
}

func TestParseLoginResponseFailure(t *testing.T) {
	body := map[string]interface{}{
		"code": "3037",
		"msg":  "密码错误",
		"data": []interface{}{},
	}
	data, _ := json.Marshal(body)

	result, err := parseLoginResponse(data)
	if err != nil {
		t.Fatalf("parseLoginResponse() error: %v", err)
	}
	if result.Code != "3037" {
		t.Errorf("Code = %q, want %q", result.Code, "3037")
	}
	if result.Msg != "密码错误" {
		t.Errorf("Msg = %q, want %q", result.Msg, "密码错误")
	}
	if result.Token != "" {
		t.Errorf("Token should be empty on failure, got %q", result.Token)
	}
}

func TestParseLoginResponseInvalidJSON(t *testing.T) {
	_, err := parseLoginResponse([]byte("not json"))
	if err == nil {
		t.Error("expected error for invalid JSON, got nil")
	}
}

func TestAuthServiceSetDeviceIPsGetter(t *testing.T) {
	svc := NewAuthService()

	called := false
	svc.SetDeviceIPsGetter(func() []string {
		called = true
		return []string{"10.10.0.1"}
	})

	svc.mu.Lock()
	getter := svc.deviceIPsGetter
	svc.mu.Unlock()

	if getter == nil {
		t.Fatal("deviceIPsGetter should not be nil")
	}
	ips := getter()
	if !called {
		t.Error("getter was not called")
	}
	if len(ips) != 1 || ips[0] != "10.10.0.1" {
		t.Errorf("ips = %v, want [10.10.0.1]", ips)
	}
}

// SyncOnce 是自动同步与手动同步的**唯一**出口：授权只能来自用户登录账号，
// 未登录必须整体跳过（不推送、不兜底）。以下逐条覆盖「未登录」的三种成因。

func TestSyncOnceNotLoggedInWhenGetterNil(t *testing.T) {
	svc := NewAuthService()

	_, err := svc.SyncOnce()
	if !errors.Is(err, ErrNotLoggedIn) {
		t.Errorf("err = %v, want ErrNotLoggedIn", err)
	}
}

func TestSyncOnceNotLoggedInWhenGetterErrors(t *testing.T) {
	svc := NewAuthService()
	svc.SetTokenGetter(func() (string, error) { return "", errors.New("no credentials") })

	_, err := svc.SyncOnce()
	if !errors.Is(err, ErrNotLoggedIn) {
		t.Errorf("err = %v, want ErrNotLoggedIn", err)
	}
}

func TestSyncOnceNotLoggedInOnEmptyToken(t *testing.T) {
	// getter 不报错但给出空串（凭证在、token 取不到）同样算未登录，
	// 否则会推一个空的 lgtoken 给设备
	svc := NewAuthService()
	svc.SetTokenGetter(func() (string, error) { return "", nil })

	_, err := svc.SyncOnce()
	if !errors.Is(err, ErrNotLoggedIn) {
		t.Errorf("err = %v, want ErrNotLoggedIn", err)
	}
}

func TestSyncOnceNoOnlineDevices(t *testing.T) {
	// 已登录但无在线设备：返回 0 且不报错（前端提示「暂无在线设备」而非失败）
	svc := NewAuthService()
	svc.SetTokenGetter(func() (string, error) { return "token", nil })
	svc.SetDeviceIPsGetter(func() []string { return nil })

	n, err := svc.SyncOnce()
	if err != nil {
		t.Fatalf("SyncOnce() error: %v", err)
	}
	if n != 0 {
		t.Errorf("n = %d, want 0", n)
	}
}

func TestDoSyncTreatsNotLoggedInAsHandled(t *testing.T) {
	// 未登录不算失败：doSync 返回 true，避免 syncAuth 无意义地指数退避重试
	svc := NewAuthService()

	if !svc.doSync() {
		t.Error("doSync() = false, want true（未登录应视为本轮处理完毕，不重试）")
	}
}

func TestLoginResultFields(t *testing.T) {
	r := &LoginResult{Code: "200", Msg: "OK", Token: "tok", Uname: "u", Uid: "1"}
	if r.Code != "200" || r.Token != "tok" || r.Uname != "u" || r.Uid != "1" {
		t.Errorf("LoginResult fields unexpected: %+v", r)
	}
}
