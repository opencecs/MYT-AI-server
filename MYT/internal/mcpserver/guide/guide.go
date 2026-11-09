// Package guide 界面元素字典：go:embed 嵌入的客户端 UI 元素字典
// （每章一个页面：元素文案 + 出处 + 作用），以两种方式暴露给 MCP 客户端，
// 覆盖不同客户端的 resource 支持差异：
//   - MCP resource：guide://ui-dictionary（全文）
//   - 工具 get_ui_guide：按章节 key 查询单章（无参数返回目录 + 用法）
//
// 源文件：docs/界面元素字典.md；嵌入副本 ui_guide.md 修改后需同步回源。
package guide

import (
	"context"
	_ "embed"
	"fmt"
	"regexp"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/moyunteng/myt/internal/mcpserver/reg"
)

//go:embed ui_guide.md
var content string

// ResourceURI resource 注册用 URI。
const ResourceURI = "guide://ui-dictionary"

// sectionPattern 章节标题行：`## 第 N 章 key（中文名）`，捕获章节 key。
var sectionPattern = regexp.MustCompile(`^## 第 (\d+) 章 ([a-zA-Z]+)（([^）]*)）`)

// section 提取出的单个章节。
type section struct {
	Num     int    // 章号
	Key     string // 章节 key（与 data-tab 值一致，如 host/agent）
	Title   string // 标题整行（含中文注释）
	Content string // 章节正文（含标题行，到下一章前）
}

// parseSections 按二级标题切分章节。
func parseSections() []section {
	lines := strings.Split(content, "\n")
	var out []section
	var cur *section
	for _, ln := range lines {
		if m := sectionPattern.FindStringSubmatch(ln); m != nil {
			out = append(out, section{Title: strings.TrimRight(ln, "\r"), Content: ln + "\n"})
			cur = &out[len(out)-1]
			fmt.Sscanf(m[1], "%d", &cur.Num)
			cur.Key = m[2]
			continue
		}
		if cur != nil {
			cur.Content += ln + "\n"
		}
	}
	return out
}

// sections 包初始化时切分好的章节（字典为编译期常量，无需并发保护）。
var sections = parseSections()

// Keys 返回全部章节 key（目录展示用）。
func Keys() []string {
	keys := make([]string, 0, len(sections))
	for _, s := range sections {
		keys = append(keys, s.Key)
	}
	return keys
}

// Section 按章节 key 取单章 markdown（key 大小写不敏感）；未命中返回空串。
func Section(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	for _, s := range sections {
		if s.Key == name {
			return s.Content
		}
	}
	return ""
}

// TOC 返回章节目录（每行一条，供无参数查询时导航）。
func TOC() string {
	var b strings.Builder
	for _, s := range sections {
		fmt.Fprintf(&b, "%s\n", s.Title)
	}
	return b.String()
}

// guideArgs get_ui_guide 参数。
type guideArgs struct {
	Section string `json:"section,omitempty" jsonschema_desc:"章节 key（host/maintenance/backup/mirror/payment/vpc/model/diskless/stream/agent/settings），大小写不敏感；留空返回目录与用法说明"`
}

// Register 注册 resource 与 get_ui_guide 工具（server.go 装配时调用）。
func Register(srv *mcp.Server, sink reg.Sink) {
	srv.AddResource(&mcp.Resource{
		URI:      ResourceURI,
		Name:     "界面元素字典",
		Title:    "MYT 客户端界面元素字典",
		MIMEType: "text/markdown",
		Size:     int64(len(content)),
		Description: "客户端各页面（host/mirror/agent 等 11 章）元素的文案、出处与作用。" +
			"需要了解某页面/某元素的作用时读取本字典，不要凭猜测操作。",
	}, func(context.Context, *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{
			{URI: ResourceURI, MIMEType: "text/markdown", Text: content},
		}}, nil
	})

	reg.Reg(srv, sink, "get_ui_guide",
		"查询《界面元素字典》：客户端各页面元素的文案/出处/作用（与 ui_map 的 summary 配套——"+
			"summary 告诉你当前在哪个页，字典告诉你每个元素干什么）。"+
			"section 传章节 key（如 host/agent）返回单章 markdown；留空返回章节目录与用法。只读。",
		func(_ context.Context, a guideArgs) (any, error) {
			if strings.TrimSpace(a.Section) == "" {
				return map[string]any{
					"usage": "get_ui_guide 的 section 参数传章节 key 取单章；" +
						"也可直接读取 resource " + ResourceURI + " 获取全文",
					"toc": TOC(),
				}, nil
			}
			sec := Section(a.Section)
			if sec == "" {
				return nil, fmt.Errorf("未找到章节 %q（可用章节：%s）", a.Section, strings.Join(Keys(), "/"))
			}
			return map[string]any{"section": strings.ToLower(a.Section), "markdown": sec}, nil
		}, reg.ReadOnly())
}
