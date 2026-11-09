package protocol

import (
	"net"
	"testing"
)

func TestParseReply(t *testing.T) {
	tests := []struct {
		name     string
		data     string
		wantNil  bool
		wantType string
		wantID   string
	}{
		{
			name:     "v3设备",
			data:     "lgcloud:device123:r1s_v3",
			wantNil:  false,
			wantType: "R1S",
			wantID:   "device123",
		},
		{
			name:     "v3设备带后缀_cecs",
			data:     "lgcloud:abc123:q1_v3_cecs",
			wantNil:  false,
			wantType: "Q1",
			wantID:   "abc123",
		},
		{
			name:     "v2主机（V2模式数据源，ParseReply 放行，过滤由调用方按模式做）",
			data:     "lgcloud:device456:r1s_v2",
			wantNil:  false,
			wantType: "R1S",
			wantID:   "device456",
		},
		{
			// 发现层全量放行（含未知后缀），模式过滤由调用方做
			//（local 只留 v3，v2 由扫描白名单把关）
			name:     "未知后缀也放行（v0，过滤由调用方做）",
			data:     "lgcloud:device789:r1s_v9",
			wantNil:  false,
			wantType: "R1S",
			wantID:   "device789",
		},
		{
			// 裸名设备（无版本后缀，旧代设备）：同样放行，由调用方过滤
			name:     "裸名设备放行",
			data:     "lgcloudpro:SN123:C1",
			wantNil:  false,
			wantType: "C1",
			wantID:   "SN123",
		},
		{
			name:    "格式不足3段",
			data:    "lgcloud:device789",
			wantNil: true,
		},
		{
			name:    "空数据",
			data:    "",
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			addr := &net.UDPAddr{IP: net.ParseIP("10.10.0.237"), Port: 7678}
			got := ParseReply([]byte(tt.data), addr)

			if tt.wantNil {
				if got != nil {
					t.Errorf("ParseReply(%q) = %+v, want nil", tt.data, got)
				}
				return
			}

			if got == nil {
				t.Fatalf("ParseReply(%q) = nil, want non-nil", tt.data)
			}
			if got.DeviceType != tt.wantType {
				t.Errorf("DeviceType = %q, want %q", got.DeviceType, tt.wantType)
			}
			if got.DeviceID != tt.wantID {
				t.Errorf("DeviceID = %q, want %q", got.DeviceID, tt.wantID)
			}
			if got.IP != "10.10.0.237" {
				t.Errorf("IP = %q, want %q", got.IP, "10.10.0.237")
			}
		})
	}
}

func TestParseReplyNonUDPAddr(t *testing.T) {
	// 非UDPAddr应该返回nil
	addr := &net.TCPAddr{IP: net.ParseIP("10.10.0.237"), Port: 8000}
	got := ParseReply([]byte("lgcloud:dev1:r1s_v3"), addr)
	if got != nil {
		t.Errorf("ParseReply with non-UDP addr = %+v, want nil", got)
	}
}

func TestParseReplyV3Truncation(t *testing.T) {
	addr := &net.UDPAddr{IP: net.ParseIP("10.10.0.1"), Port: 7678}

	// _v3 后面的内容应被截断
	got := ParseReply([]byte("type:id:p1_v3_cecs_extra"), addr)
	if got == nil {
		t.Fatal("ParseReply returned nil for v3 device with suffix")
	}
	if got.DeviceName != "p1_v3" {
		t.Errorf("DeviceName = %q, want %q", got.DeviceName, "p1_v3")
	}
}

func TestParseReplyBareNameVersion(t *testing.T) {
	// 裸名设备（如 C1、q1_10）解析后应拿到 Device，且 ParseVersion 判为 v0
	addr := &net.UDPAddr{IP: net.ParseIP("10.10.0.5"), Port: 7678}
	got := ParseReply([]byte("lgcloudpro:SN123:C1"), addr)
	if got == nil {
		t.Fatal("ParseReply(lgcloudpro:SN123:C1) = nil, want non-nil")
	}
	if got.DeviceName != "C1" {
		t.Errorf("DeviceName = %q, want %q", got.DeviceName, "C1")
	}
	if v := ParseVersion(got.DeviceName); v != "v0" {
		t.Errorf("ParseVersion(%q) = %q, want %q", got.DeviceName, v, "v0")
	}
}

func TestParseVersion(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{"r1s_v3", "v3"},
		{"q1_v3_cecs", "v3"},
		{"p1_10", "v1"},
		{"old_device", "v0"},
		{"", "v0"},
	}
	for _, tt := range tests {
		got := ParseVersion(tt.name)
		if got != tt.want {
			t.Errorf("ParseVersion(%q) = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestExtractDeviceType(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{"r1s_v3", "r1s"},
		{"q1_v3_cecs", "q1"},
		{"p1", "p1"}, // 无下划线，返回原值
	}
	for _, tt := range tests {
		got := extractDeviceType(tt.name)
		if got != tt.want {
			t.Errorf("extractDeviceType(%q) = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestMagicBytes(t *testing.T) {
	magic := MagicBytes()
	if string(magic) != "lgcloud" {
		t.Errorf("MagicBytes() = %q, want %q", string(magic), "lgcloud")
	}
}

func TestPort(t *testing.T) {
	if Port() != 7678 {
		t.Errorf("Port() = %d, want %d", Port(), 7678)
	}
}

func TestScreenshotPort(t *testing.T) {
	if ScreenshotPort != 30105 {
		t.Errorf("ScreenshotPort = %d, want %d", ScreenshotPort, 30105)
	}
}
