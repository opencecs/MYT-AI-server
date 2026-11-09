package util

import (
	"errors"
	"net"
	"testing"
)

func TestMD5Hash(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"", "d41d8cd98f00b204e9800998ecf8427e"},
		{"hello", "5d41402abc4b2a76b9719d911017c592"},
		{"15071042575", "ee02716b623c73f69ef35e6a37c08ff0"},
	}
	for _, tt := range tests {
		got := MD5Hash(tt.input)
		if got != tt.want {
			t.Errorf("MD5Hash(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestIsSuccessCode(t *testing.T) {
	tests := []struct {
		code any
		want bool
	}{
		{0, true},
		{200, true},
		{float64(0), true},
		{float64(200), true},
		{"0", true},
		{"200", true},
		{404, false},
		{float64(500), false},
		{"404", false},
		{nil, false},
		{true, false},
	}
	for _, tt := range tests {
		got := IsSuccessCode(tt.code)
		if got != tt.want {
			t.Errorf("IsSuccessCode(%v) = %v, want %v", tt.code, got, tt.want)
		}
	}
}

func TestFriendlyNetError(t *testing.T) {
	tests := []struct {
		err  error
		want string
	}{
		{nil, ""},
		{errors.New("context deadline exceeded"), "网络请求超时"},
		{errors.New("Client.Timeout exceeded while awaiting headers"), "网络请求超时"},
		{errors.New("i/o timeout"), "网络请求超时"},
		{errors.New("connection refused"), "网络连接异常"},
		{errors.New("no such host"), "网络连接异常"},
		{errors.New("connection reset by peer"), "网络连接中断"},
		{errors.New("broken pipe"), "网络连接中断"},
		{errors.New("some unknown error"), "some unknown error"},
	}
	for _, tt := range tests {
		got := FriendlyNetError(tt.err)
		if got != tt.want {
			t.Errorf("FriendlyNetError(%v) = %q, want %q", tt.err, got, tt.want)
		}
	}
}

func TestNewHTTPClient(t *testing.T) {
	client := NewHTTPClient()
	if client == nil {
		t.Fatal("NewHTTPClient() returned nil")
	}
	if client.Timeout == 0 {
		t.Error("NewHTTPClient() timeout should not be zero")
	}
}

func TestLgCloudConstants(t *testing.T) {
	if LgCloudMagic != "lgcloud" {
		t.Errorf("LgCloudMagic = %q, want %q", LgCloudMagic, "lgcloud")
	}
	if LgCloudPort != 7678 {
		t.Errorf("LgCloudPort = %d, want %d", LgCloudPort, 7678)
	}
}

func TestMirrorItemFields(t *testing.T) {
	item := MirrorItem{
		ID:     "1",
		Name:   "test",
		URL:    "http://example.com/image.tar",
		TType2: []string{"r1s_v3"},
		OsVer:  "5",
		SysVer: "1",
		UDesc:  "test image",
	}
	if item.ID != "1" {
		t.Errorf("MirrorItem.ID = %q, want %q", item.ID, "1")
	}
	if len(item.TType2) != 1 || item.TType2[0] != "r1s_v3" {
		t.Errorf("MirrorItem.TType2 = %v, unexpected", item.TType2)
	}
}

// 确保FriendlyNetError处理net.Error接口类型
func TestFriendlyNetErrorNetError(t *testing.T) {
	var err net.Error
	// net.DNSError 实现了 net.Error 接口
	err = &net.DNSError{Err: "no such host", Name: "test.invalid"}
	got := FriendlyNetError(err)
	if got != "网络连接异常" {
		t.Errorf("FriendlyNetError(DNSError) = %q, want %q", got, "网络连接异常")
	}
}
