package guide

// guide_test.go — 字典章节切分/过滤与注册验证。

import (
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestParseSections(t *testing.T) {
	if len(sections) != 13 {
		t.Fatalf("应有 13 章，实际 %d：%v", len(sections), Keys())
	}
	want := []string{"host", "maintenance", "backup", "mirror", "payment",
		"vpc", "model", "diskless", "stream", "agent", "settings", "shared", "player"}
	got := Keys()
	for i, k := range want {
		if got[i] != k {
			t.Fatalf("章节 key 顺序异常：got %v", got)
		}
	}
	// host 章内容应包含其标题行
	if sec := Section("host"); !strings.Contains(sec, "host") || !strings.Contains(sec, "## 第 1 章") {
		t.Fatalf("Section(host) 内容异常：%.80s", sec)
	}
	// 各章内容互不串章：host 章不应含第 2 章标题
	if strings.Contains(Section("host"), "## 第 2 章") {
		t.Fatal("Section(host) 混入了下一章内容")
	}
	if Section("nope") != "" {
		t.Fatal("未知章节应返回空串")
	}
	if !strings.Contains(TOC(), "## 第 13 章 player") {
		t.Fatalf("TOC 应含全部章节标题:\n%s", TOC())
	}
}

func TestRegister(t *testing.T) {
	var tools []*mcp.Tool
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	Register(srv, func(tool *mcp.Tool) { tools = append(tools, tool) })
	if len(tools) != 1 || tools[0].Name != "get_ui_guide" {
		t.Fatalf("应注册 get_ui_guide 工具，实际 %v", tools)
	}
}
