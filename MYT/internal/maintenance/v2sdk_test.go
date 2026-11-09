package maintenance

import (
	"strings"
	"testing"
)

func TestDigitsOnly(t *testing.T) {
	cases := []struct{ in, want string }{
		{"1234,users:...", "1234"},
		{"1234", "1234"},
		{"", ""},
		{"abc", ""},
		{"42)", "42"},
	}
	for _, c := range cases {
		if got := digitsOnly(c.in); got != c.want {
			t.Errorf("digitsOnly(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestStripDeletedSuffix(t *testing.T) {
	// (deleted) 后缀必须被 strip（2026-10-14 现场失败根因：readlink 返回
	// "/path (deleted)" 被当 mv 目标 → mv 非法 exit 2）
	cases := []struct{ in, want string }{
		{"/home/user/v2/myt-sdk-go-v2", "/home/user/v2/myt-sdk-go-v2"},
		{"/home/user/v2/myt-sdk-go-v2.oldbin (deleted)", "/home/user/v2/myt-sdk-go-v2.oldbin"},
		{"/proc/1/exe (deleted)", "/proc/1/exe"},
	}
	for _, c := range cases {
		if got := strings.TrimSuffix(c.in, " (deleted)"); got != c.want {
			t.Errorf("TrimSuffix(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestParseV2SdkInfoBody(t *testing.T) {
	cases := []struct {
		name string
		body string
		want bool
	}{
		{"v2_sdk_data层", `{"code":0,"message":"OK","data":{"status":"ok","currentVersion":"3"}}`, true},
		{"顶层平铺_老版本", `{"status":"ok","currentVersion":"2"}`, true},
		{"data层仅currentVersion", `{"data":{"currentVersion":"3"}}`, true},
		{"顶层仅status", `{"status":"ok"}`, true},
		{"非JSON_HTML", `<html><body>SPA fallback</body></html>`, false},
		{"空对象", `{}`, false},
		{"404响应体", `{"message":"not found"}`, false},
	}
	for _, c := range cases {
		if got := parseV2SdkInfoBody([]byte(c.body)); got != c.want {
			t.Errorf("%s: parseV2SdkInfoBody(%q) = %v, want %v", c.name, c.body, got, c.want)
		}
	}
}
