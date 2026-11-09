package sdkapi

import "testing"

// TestParseModelFromFirmware 固件版本兜底解析机型（model 为空的设备/镜像用）。
// 格式依据：192.0.2.202/205 实测 "QL-r1q-2026.v0.8.8.202608110721-v3" → "r1q_v3"。
func TestParseModelFromFirmware(t *testing.T) {
	tests := []struct {
		name    string
		version string
		want    string
	}{
		{"标准格式：段2机型+尾段版本代", "QL-r1q-2026.v0.8.8.202608110721-v3", "r1q_v3"},
		{"无版本代尾段：只用段2", "QL-r1s-2026.v0.8.7", "r1s"},
		{"版本代多位数字", "QL-p1-2026.v1.0.0.202601010101-v29", "p1_v29"},
		{"段数不足两段", "QL-r1q", ""},
		{"单段", "2026.v0.8.8", ""},
		{"段2为空", "QL--2026.v0.8.8", ""},
		{"空输入", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ParseModelFromFirmware(tt.version); got != tt.want {
				t.Errorf("ParseModelFromFirmware(%q) = %q, want %q", tt.version, got, tt.want)
			}
		})
	}
}
