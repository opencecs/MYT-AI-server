package winshot

// winshot_test.go — 标识归一化四形态、start 幂等、409 自愈重试、close 流程
//（httptest 模拟播放器 control server）。

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/moyunteng/myt/internal/config"
)

// runningFixture 运行中容器清单（IP_坑位 / id / name 与真机同构）。
func runningFixture() []config.AndroidInfo {
	return []config.AndroidInfo{
		{ID: "1791623019096_8_T0008", Name: "测试机8", IP: "192.0.2.202", IndexNum: 8},
		{ID: "1791623019096_9_T0009", Name: "测试机9", IP: "192.0.2.202", IndexNum: 9},
		{ID: "1791623019001_0_T0001", Name: "单台机", IP: "192.0.2.100", IndexNum: 0},
	}
}

func TestResolveAndroidFourForms(t *testing.T) {
	d := &Deps{RunningAndroids: runningFixture}
	cases := []struct {
		name    string
		android string
		wantID  string
		wantErr string // 空 = 期望成功
	}{
		{"androidId", "1791623019096_8_T0008", "1791623019096_8_T0008", ""},
		{"设备IP_坑位", "192.0.2.202_9", "1791623019096_9_T0009", ""},
		{"容器名唯一", "单台机", "1791623019001_0_T0001", ""},
		{"设备IP 仅一台运行中", "192.0.2.100", "1791623019001_0_T0001", ""},
		{"设备IP 多台不猜", "192.0.2.202", "", "有 2 台运行中云机"},
		{"容器名无命中", "不存在", "", "未找到运行中的云机"},
		{"空串", "", "", "android 参数不能为空"},
	}
	for _, c := range cases {
		meta, err := d.resolveAndroid(c.android)
		if c.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Errorf("%s: 期望错误含 %q, got %v", c.name, c.wantErr, err)
			}
			continue
		}
		if err != nil || meta.ID != c.wantID {
			t.Errorf("%s: got %v,%v, want %s", c.name, meta.ID, err, c.wantID)
		}
	}
}

func TestResolveAndroidEmptyRunning(t *testing.T) {
	d := &Deps{RunningAndroids: func() []config.AndroidInfo { return nil }}
	if _, err := d.resolveAndroid("anything"); err == nil || !strings.Contains(err.Error(), "无运行中的云机") {
		t.Fatalf("无运行中容器应明确报错, got %v", err)
	}
}

func TestStartCastIdempotent(t *testing.T) {
	var startCalls atomic.Int32
	var keys []string // 初始无投屏
	d := &Deps{
		RunningAndroids: runningFixture,
		RunningKeys:     func() []string { return keys },
		StartCast: func(ip string, idx int) error {
			startCalls.Add(1)
			keys = append(keys, fmt.Sprintf("%s_%d", ip, idx))
			return nil
		},
	}
	// 首次：未投屏 → 发起
	key, already, err := d.startCast("1791623019096_8_T0008")
	if err != nil || already || key != "192.0.2.202_8" {
		t.Fatalf("首次 start 异常: key=%q already=%v err=%v", key, already, err)
	}
	// 重复：已在投屏 → 幂等（不再拉起）
	key, already, err = d.startCast("192.0.2.202_8")
	if err != nil || !already || key != "192.0.2.202_8" {
		t.Fatalf("重复 start 应幂等: key=%q already=%v err=%v", key, already, err)
	}
	if n := startCalls.Load(); n != 1 {
		t.Fatalf("同一 key 连续调用应只拉起一次播放器, 实际 %d 次", n)
	}
	// 启动失败（StartCast 报错）→ 明确报错
	d2 := &Deps{
		RunningAndroids: runningFixture,
		RunningKeys:     func() []string { return nil },
		StartCast:       func(string, int) error { return errors.New("设备离线") },
	}
	if _, _, err := d2.startCast("192.0.2.202_9"); err == nil || !strings.Contains(err.Error(), "发起投屏失败") {
		t.Fatalf("启动失败应明确报错, got %v", err)
	}
}

// newControlDeps 启动模拟播放器 control server（/screenshot /close）。
func newControlDeps(t *testing.T, conflictFirst int) (*Deps, *atomic.Int32) {
	t.Helper()
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/screenshot" && r.Method == http.MethodGet:
			if int(attempts.Add(1)) <= conflictFirst {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusConflict)
				_, _ = w.Write([]byte(`{"error":"纹理硬解帧不可截取，可改用设备端截图接口"}`))
				return
			}
			_, _ = w.Write([]byte{0x89, 'P', 'N', 'G'})
		case r.URL.Path == "/close" && r.Method == http.MethodPost:
			_, _ = w.Write([]byte(`{"ok":true}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	port := 0
	for _, c := range srv.URL[strings.LastIndex(srv.URL, ":")+1:] {
		port = port*10 + int(c-'0')
	}
	keys := []string{"192.0.2.202_8"}
	return &Deps{
		RunningAndroids: runningFixture,
		RunningKeys:     func() []string { return keys },
		ControlPortOf: func(key string) int {
			if key == "192.0.2.202_8" {
				return port
			}
			return 0
		},
		HTTPClient: srv.Client(),
	}, &attempts
}

func TestCaptureTextureRetryHeals(t *testing.T) {
	d, attempts := newControlDeps(t, 2) // 前 2 次 409，第 3 次成功
	key, png, err := d.capture("1791623019096_8_T0008")
	if err != nil {
		t.Fatalf("409 应在第 3 次自愈: %v", err)
	}
	if key != "192.0.2.202_8" || len(png) == 0 {
		t.Fatalf("capture 返回异常: key=%q png=%v", key, png)
	}
	if n := attempts.Load(); n != 3 {
		t.Fatalf("应共尝试 3 次, 实际 %d", n)
	}
}

func TestCaptureTextureExhausted(t *testing.T) {
	d, attempts := newControlDeps(t, 99) // 恒 409
	start := time.Now()
	_, _, err := d.capture("1791623019096_8_T0008")
	if err == nil || !strings.Contains(err.Error(), "get_screenshot") {
		t.Fatalf("重试耗尽应提示 get_screenshot 替代, got %v", err)
	}
	if !strings.Contains(err.Error(), "纹理硬解帧不可截取") {
		t.Fatalf("错误应保留纹理硬解语义: %v", err)
	}
	if n := attempts.Load(); n != shotAttempts {
		t.Fatalf("应尝试 %d 次, 实际 %d", shotAttempts, n)
	}
	if elapsed := time.Since(start); elapsed < 2*shotRetryInterval {
		t.Fatalf("重试间应有间隔, 实际 %v", elapsed)
	}
}

func TestCaptureNotInCastGuidance(t *testing.T) {
	d, _ := newControlDeps(t, 0)
	// 容器运行中但未投屏 → 引导错误列当前投屏清单
	if _, _, err := d.capture("1791623019096_9_T0009"); err == nil || !strings.Contains(err.Error(), "未在投屏中") {
		t.Fatalf("未投屏应报引导错误, got %v", err)
	}
}

func TestCloseFlow(t *testing.T) {
	d, _ := newControlDeps(t, 0)
	key, err := d.closePlayer("192.0.2.202_8")
	if err != nil || key != "192.0.2.202_8" {
		t.Fatalf("close 异常: key=%q err=%v", key, err)
	}
	if _, err := d.closePlayer("不存在"); err == nil || !strings.Contains(err.Error(), "未找到运行中的云机") {
		t.Fatalf("未知标识应报未找到, got %v", err)
	}
}

func TestControlConnectionError(t *testing.T) {
	d := &Deps{
		RunningAndroids: runningFixture,
		RunningKeys:     func() []string { return []string{"192.0.2.202_8"} },
		ControlPortOf:   func(string) int { return 1 }, // port 1 不可连
	}
	if _, _, err := d.capture("1791623019096_8_T0008"); err == nil || !strings.Contains(err.Error(), "连接播放器控制接口失败") {
		t.Fatalf("连接失败应有结构化错误, got %v", err)
	}
}
