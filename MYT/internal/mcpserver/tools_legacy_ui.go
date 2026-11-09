package mcpserver

// tools_legacy_ui.go — 遗留 UI 工具（早于浏览器类工具移植存在，保留原名以免
// 破坏既有 MCP 客户端用法）：screenshot（主窗口截图）与 dom_query
// （可见可交互元素枚举，坐标逃生舱）。browser 包提供官方 1:1 的
// take_screenshot 等价能力后，这两个工具作为轻量快捷方式保留。
//
// 归属说明：本文件留在 mcpserver 包根目录而非 tools/ 子包——工具闭包绑定
// 根包 *Server 的非导出字段（s.cdp/s.mcpServer/s.recordTool），移入子包需
// 导出 Server 内部状态；且 tools/ 是纯业务域（不碰 UI 自动化），本文件依赖
// cdp，移入会破坏「tools 不碰 UI 自动化」的解耦边界。

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/moyunteng/myt/internal/mcpserver/cdp"
	"github.com/moyunteng/myt/internal/mcpserver/reg"
)

type screenshotArgsUI struct {
	// 无参数：截取主窗口当前画面
}

type domQueryArgs struct {
	MaxResults int `json:"maxResults,omitempty" jsonschema_desc:"最多返回元素数（默认 200，上限 2000，超出钳到 2000）"`
}

// registerUITools 注册遗留 UI 工具。
func registerUITools(s *Server) {
	reg.RegRaw(s.mcpServer, s.recordTool, "screenshot",
		"截取客户端主窗口当前画面，返回 PNG 图片。用于确认界面状态。无破坏性。"+
			"注意：客户端窗口最小化时会超时。",
		func(ctx context.Context, _ screenshotArgsUI) (*mcp.CallToolResult, error) {
			if err := s.cdpGate(ctx); err != nil {
				return nil, err
			}
			wsURL, err := s.cdp.FirstPageTarget(ctx)
			if err != nil {
				return nil, err
			}
			if _, err := s.cdp.Call(ctx, wsURL, "Page.enable", map[string]any{}, 5*time.Second); err != nil {
				return nil, err
			}
			res, err := s.cdp.Call(ctx, wsURL, "Page.captureScreenshot", map[string]any{"format": "png"}, 10*time.Second)
			if err != nil {
				return nil, err
			}
			var out struct {
				Data string `json:"data"`
			}
			if err := cdp.JSONUnmarshal(res, &out); err != nil {
				return nil, err
			}
			raw, err := base64.StdEncoding.DecodeString(out.Data)
			if err != nil {
				return nil, fmt.Errorf("解码截图失败: %w", err)
			}
			return &mcp.CallToolResult{Content: []mcp.Content{
				&mcp.ImageContent{Data: raw, MIMEType: "image/png"},
			}}, nil
		}, reg.ReadOnly())

	reg.Reg(s.mcpServer, s.recordTool, "dom_query",
		"枚举客户端界面当前可见的可交互元素（按钮/输入框/链接等），返回 tag/text/中心坐标/尺寸。"+
			"最多返回 maxResults 个元素（默认 200，上限 2000）；元素较多时结果可能丢尾，可增大 maxResults 或缩小页面范围。"+
			"配合 click/type_text/scroll 使用；优先用 ui_map（可访问性快照，uid 定位更稳）。无破坏性。",
		func(ctx context.Context, a domQueryArgs) (any, error) {
			if err := s.cdpGate(ctx); err != nil {
				return nil, err
			}
			v, err := s.cdp.Eval(ctx, domQueryJS(a.MaxResults), 10*time.Second)
			if err != nil {
				return nil, err
			}
			return v, nil
		}, reg.ReadOnly())
}

// cdpGate 校验 CDP 可用性（遗留 UI 工具用）。
func (s *Server) cdpGate(ctx context.Context) error {
	if !s.cdp.Ready(ctx) {
		return errors.New("界面控制需重启客户端后可用（CDP 未就绪：客户端启动时未开启远程调试，或窗口尚未创建）")
	}
	return nil
}

// --- dom_query 注入脚本（原 tools_cdp.go 迁移） ---

// domQueryJSFmt 收集可见可交互元素：返回 tag/text/中心坐标/尺寸。
const domQueryJSFmt = `(() => {
  const vis = el => {
    const st = getComputedStyle(el);
    if (st.display === 'none' || st.visibility === 'hidden' || +st.opacity === 0) return false;
    const r = el.getBoundingClientRect();
    return r.width > 0 && r.height > 0;
  };
  const els = [...document.querySelectorAll('button, input, select, textarea, a, [role="button"], [onclick], [title]')];
  return els.filter(vis).slice(0, %d).map((el, i) => {
    const r = el.getBoundingClientRect();
    return { i, tag: el.tagName.toLowerCase(),
      text: ((el.innerText || el.value || el.placeholder || el.getAttribute('aria-label') || el.title || '') + '').trim().slice(0, 80),
      x: Math.round(r.x + r.width / 2), y: Math.round(r.y + r.height / 2),
      w: Math.round(r.width), h: Math.round(r.height) };
  });
})()`

// domQueryLimit 计算 dom_query 的返回上限：缺省 200，上限 2000。
func domQueryLimit(maxResults int) int {
	const def, max = 200, 2000
	if maxResults <= 0 {
		return def
	}
	if maxResults > max {
		return max
	}
	return maxResults
}

// domQueryJS 按 maxResults 生成注入脚本。
func domQueryJS(maxResults int) string {
	return fmt.Sprintf(domQueryJSFmt, domQueryLimit(maxResults))
}
