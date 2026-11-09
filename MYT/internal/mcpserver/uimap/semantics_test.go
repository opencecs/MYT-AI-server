package uimap

// semantics_test.go — 语义字典单测（工具 annotations 注册检查见根包 server_test.go）。

import (
	"testing"
)

// --- 语义字典 ---

func TestSemanticNote(t *testing.T) {
	cases := []struct {
		name string
		want string
	}{
		{"创建云机", "打开创建云机面板"},
		{"关闭投屏", "停止当前投屏会话"}, // 长词优先于「投屏」
		{"投屏", "发起投屏"},
		{"删除分组", "删除当前分组（组内设备不受影响）"},
		{"批量删除", "删除目标（不可恢复，谨慎点击）"}, // 无专有规则时兜底
		{"192.0.2.202", ""},
		{"", ""},
	}
	for _, c := range cases {
		if got := semanticNote(c.name); got != c.want {
			t.Errorf("semanticNote(%q) = %q, want %q", c.name, got, c.want)
		}
	}
}
