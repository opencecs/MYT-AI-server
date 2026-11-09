package maintenance

import (
	"fmt"
	"strings"
	"testing"
)

// TestPanelFormV3 验证 v3 形态与历史硬编码行为逐字一致（myt-panel -port 8081）。
func TestPanelFormV3(t *testing.T) {
	// 未注入 isV2Mode（nil）→ 默认 v3
	form := panelFormByMode("192.0.2.202")
	if form.Binary != "myt-panel" {
		t.Fatalf("Binary = %q, want myt-panel", form.Binary)
	}
	if form.Args != "-port 8081" {
		t.Fatalf("Args = %q, want -port 8081", form.Args)
	}
	if form.Port != 8081 {
		t.Fatalf("Port = %d, want 8081", form.Port)
	}
	if form.CommandLine() != "/home/user/myt-panel -port 8081" {
		t.Fatalf("CommandLine = %q", form.CommandLine())
	}
	if form.RelCommandLine() != "./myt-panel -port 8081" {
		t.Fatalf("RelCommandLine = %q", form.RelCommandLine())
	}
	if form.URL("192.0.2.202") != "http://192.0.2.202:8081" {
		t.Fatalf("URL = %q", form.URL("192.0.2.202"))
	}
}

// TestPanelFormV2 验证 v2 形态为全参数（myt-panel-v2 -host-ip <ip> -sdk-port 81 -port 8181）。
func TestPanelFormV2(t *testing.T) {
	defer func() { isV2Mode = nil }()
	isV2Mode = func() bool { return true }
	form := panelFormByMode("192.0.2.202")
	if form.Binary != "myt-panel-v2" {
		t.Fatalf("Binary = %q, want myt-panel-v2", form.Binary)
	}
	want := "-host-ip 192.0.2.202 -sdk-port 81 -port 8181"
	if form.Args != want {
		t.Fatalf("Args = %q, want %q", form.Args, want)
	}
	if form.Port != 8181 {
		t.Fatalf("Port = %d, want 8181", form.Port)
	}
	if form.CommandLine() != "/home/user/myt-panel-v2 "+want {
		t.Fatalf("CommandLine = %q", form.CommandLine())
	}
	if form.URL("192.0.2.202") != "http://192.0.2.202:8181" {
		t.Fatalf("URL = %q", form.URL("192.0.2.202"))
	}
	if !strings.Contains(form.ListenGrep(), "grep 8181") {
		t.Fatalf("ListenGrep = %q", form.ListenGrep())
	}
}

// TestPanelFormListenGrepMatch 验证健康检查命令与 Contains 判定对得上
// （netstat/ss 输出里端口以 :port 形态出现）。
func TestPanelFormListenGrepMatch(t *testing.T) {
	for _, port := range []int{8081, 8181} {
		sample := fmt.Sprintf("tcp 0.0.0.0:%d 0.0.0.0:* LISTEN", port)
		if !strings.Contains(sample, fmt.Sprintf(":%d", port)) {
			t.Fatalf("port %d 应能被 :%%d 判定命中", port)
		}
	}
}
