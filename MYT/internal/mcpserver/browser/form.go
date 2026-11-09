package browser

// form.go — 表单类工具（复刻 chrome-devtools-mcp src/tools/input.ts 的
// fill / fill_form / upload_file）。fill 语义对齐官方 locator.fill：
// checkbox/radio/switch → "true"/"false" 切换；select → 按文本选中 option；
// 文本框 → 原生 setter + input/change 事件（React 受控组件可响应）。

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/moyunteng/myt/internal/mcpserver/cdp"
	"github.com/moyunteng/myt/internal/mcpserver/reg"
)

type fillArgs struct {
	UID             string `json:"uid" jsonschema_desc:"目标元素 uid（来自 ui_map 快照行）"`
	Value           string `json:"value" jsonschema_desc:"填充值。checkbox/toggle 用 \"true\"/\"false\"，radio 用 \"true\"；select 传 option 文本；其余为文本内容"`
	IncludeSnapshot bool   `json:"includeSnapshot,omitempty" jsonschema_desc:"true 时在响应中附带最新快照"`
}

type fillFormElement struct {
	UID   string `json:"uid" jsonschema_desc:"要填充的元素 uid"`
	Value string `json:"value" jsonschema_desc:"填充值（同 fill 的 value 规则）"`
}

type fillFormArgs struct {
	Elements        []fillFormElement `json:"elements" jsonschema_desc:"要填充的元素列表（uid + value）。官方建议：优先用本工具一次填完整个表单，而非多次 fill"`
	IncludeSnapshot bool              `json:"includeSnapshot,omitempty" jsonschema_desc:"true 时在响应中附带最新快照"`
}

type uploadFileArgs struct {
	UID             string   `json:"uid" jsonschema_desc:"file input 元素 uid（或会打开文件选择器的元素 uid）"`
	FilePaths       []string `json:"filePaths" jsonschema_desc:"一个或多个本地文件绝对路径"`
	IncludeSnapshot bool     `json:"includeSnapshot,omitempty" jsonschema_desc:"true 时在响应中附带最新快照"`
}

// registerFormTools 注册 fill/fill_form/upload_file。
func registerFormTools(srv *mcp.Server, sink reg.Sink, h *Hub) {
	reg.Reg(srv, sink, "fill",
		"向输入框/文本域填入文本，或从 <select> 选中选项，或切换 checkbox/radio"+
			"（这是复刻 chrome-devtools-mcp 的 fill 工具，行为与官方一致：checkbox/toggle 用 "+
			"\"true\"/\"false\"，radio 用 \"true\"，select 传 option 文本）。uid 来自 ui_map 快照行。",
		func(ctx context.Context, a fillArgs) (any, error) {
			if err := h.cdpGate(ctx); err != nil {
				return nil, err
			}
			if err := h.fillOne(ctx, a.UID, a.Value); err != nil {
				return nil, err
			}
			out := map[string]any{"result": "ok"}
			if a.IncludeSnapshot {
				h.includeSnapshot(ctx, out)
			}
			return out, nil
		})

	reg.Reg(srv, sink, "fill_form",
		"一次填充多个表单元素（inputs/selects/checkbox/radios）（这是复刻 chrome-devtools-mcp 的 "+
			"fill_form 工具，行为与官方一致）。官方建议：与表单交互时始终优先用本工具，"+
			"比多次 fill/click 更快更稳、减少轮次。某个元素触发 JS dialog 时会中断并报告该 uid。",
		func(ctx context.Context, a fillFormArgs) (any, error) {
			if err := h.cdpGate(ctx); err != nil {
				return nil, err
			}
			if len(a.Elements) == 0 {
				return nil, errors.New("elements 不能为空")
			}
			out, err := h.fillForm(ctx, a.Elements)
			if err != nil {
				return nil, err
			}
			if a.IncludeSnapshot {
				h.includeSnapshot(ctx, out)
			}
			return out, nil
		})

	reg.Reg(srv, sink, "upload_file",
		"通过 file input 元素上传文件（这是复刻 chrome-devtools-mcp 的 upload_file 工具，行为与官方一致："+
			"经 CDP DOM.setFileInputFiles 设置文件）。filePaths 为本机（客户端进程所在机器）绝对路径。",
		func(ctx context.Context, a uploadFileArgs) (any, error) {
			if err := h.cdpGate(ctx); err != nil {
				return nil, err
			}
			if len(a.FilePaths) == 0 {
				return nil, errors.New("filePaths 不能为空")
			}
			if err := h.uploadFile(ctx, a.UID, a.FilePaths); err != nil {
				return nil, err
			}
			out := map[string]any{"result": "ok", "files": a.FilePaths}
			if a.IncludeSnapshot {
				h.includeSnapshot(ctx, out)
			}
			return out, nil
		})
}

// fillOne 填充单个元素（fill 工具主体）。
func (h *Hub) fillOne(ctx context.Context, uid, value string) error {
	_, err := h.fillElement(ctx, uid, value)
	return err
}

// fillElement 填充单个元素，返回元素标签（fill_form 中断报告用）。
// 按身份定位 + 页面 JS 填充（React 受控组件经原生 value setter 响应）。
func (h *Hub) fillElement(ctx context.Context, uid, value string) (string, error) {
	r, err := h.actOnIdentity(ctx, uid, "fill", map[string]any{"value": value})
	if err != nil {
		return "", err
	}
	return r.Tag, nil
}

// fillForm 依次填充元素列表（官方 fillFormElements：单个 waitForEventsAfterAction
// 包住全部填充；某元素触发 dialog 时返回 interruptedUid，剩余元素不再填充，
// 打开 dialog 的元素本身即中断点、不计入 filled）。
func (h *Hub) fillForm(ctx context.Context, elements []fillFormElement) (map[string]any, error) {
	filled := 0
	for i, el := range elements {
		if _, err := h.fillElement(ctx, el.UID, el.Value); err != nil {
			if h.GetDialog() != nil {
				return h.fillFormInterrupted(ctx, elements, i, filled), nil
			}
			return nil, fmt.Errorf("填充第 %d 个元素失败: %w", i+1, err)
		}
		if h.GetDialog() != nil {
			// 本次填充打开了 dialog：该元素即中断点（官方 handleActionError 语义）
			return h.fillFormInterrupted(ctx, elements, i, filled), nil
		}
		filled++
	}
	_ = h.afterAction(ctx, func() error { return nil })
	return map[string]any{"result": "ok", "filled": filled}, nil
}

// fillFormInterrupted 构造 dialog 中断响应（官方 fill_form 的中断文案语义）。
// idx 为打开 dialog 的元素下标（0 起），filled 为成功填充数（不含该元素）。
func (h *Hub) fillFormInterrupted(ctx context.Context, elements []fillFormElement, idx, filled int) map[string]any {
	interrupted := ""
	if idx >= 0 && idx < len(elements) {
		interrupted = elements[idx].UID
	}
	hasRemaining := idx < len(elements)
	msg := fmt.Sprintf("Filling out the element with uid %s opened a dialog.", interrupted)
	if hasRemaining {
		msg += " The remaining elements were not filled out."
	}
	out := map[string]any{"result": "dialog_interrupted", "interruptedUid": interrupted, "filled": filled, "message": msg}
	h.includeSnapshot(ctx, out)
	return out
}

// uploadFile 官方 uploadFile 等价：会话内按身份定位 file input 的 objectId →
// DOM.requestNode 转 nodeId → DOM.setFileInputFiles 设置文件列表（元素为
// <input type=file> 时有效）。官方的 file chooser 回退依赖 fileChooser 事件
// 拦截，WebView2 场景不移植，失败时明确报错。
func (h *Hub) uploadFile(ctx context.Context, uid string, filePaths []string) error {
	wsURL, err := h.CurrentWS(ctx)
	if err != nil {
		return err
	}
	sess, err := h.client.Dial(wsURL)
	if err != nil {
		return fmt.Errorf("建立上传会话失败: %w", err)
	}
	defer sess.Close()
	objID, err := h.locateObjectID(ctx, sess, uid)
	if err != nil {
		return err
	}
	res, err := sess.Call(ctx, "DOM.requestNode", map[string]any{"objectId": objID}, 10*time.Second)
	if err != nil {
		return fmt.Errorf("uid %q: %w", uid, cdp.ErrRefPageChanged)
	}
	var node struct {
		NodeID int64 `json:"nodeId"`
	}
	if err := cdp.JSONUnmarshal(res, &node); err != nil || node.NodeID == 0 {
		return fmt.Errorf("uid %q: %w", uid, cdp.ErrRefPageChanged)
	}
	if _, err := sess.Call(ctx, "DOM.setFileInputFiles", map[string]any{
		"files": filePaths, "nodeId": node.NodeID,
	}, 10*time.Second); err != nil {
		return fmt.Errorf(
			"Failed to upload file. The element could not accept the file directly "+
				"（DOM.setFileInputFiles 失败：元素可能不是 <input type=file>）: %w", err)
	}
	return nil
}
