package browser

// mock_test.go — 假 CDP 服务端接入（复用 cdp/mock 子包）+ 公共测试工具。

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/moyunteng/myt/internal/mcpserver/cdp"
	"github.com/moyunteng/myt/internal/mcpserver/cdp/mock"
	"github.com/moyunteng/myt/internal/mcpserver/uimap"
)

// recorder 记录 CDP 命令调用序列（命令断言用）。
type recorder struct {
	calls []recordedCall
}

type recordedCall struct {
	Method string
	Params map[string]any
}

// match 判断是否命中指定方法（可选参数子串校验放调用侧）。
func (r *recorder) methods() []string {
	out := make([]string, len(r.calls))
	for i, c := range r.calls {
		out[i] = c.Method
	}
	return out
}

func (r *recorder) count(method string) int {
	n := 0
	for _, c := range r.calls {
		if c.Method == method {
			n++
		}
	}
	return n
}

// newTestHub 启动 mock CDP 并返回接入它的 Hub + recorder。
// handler 由用例提供，可读取 rec 检查调用序列。
func newTestHub(t *testing.T, handler func(rec *recorder, method string, params json.RawMessage) (any, error)) (*Hub, *recorder) {
	t.Helper()
	rec := &recorder{}
	base := mock.New(t, func(method string, params json.RawMessage) (any, error) {
		var m map[string]any
		_ = json.Unmarshal(params, &m)
		rec.calls = append(rec.calls, recordedCall{Method: method, Params: m})
		return handler(rec, method, params)
	})
	h := NewHub(base, nil)
	return h, rec
}

// stdHandler 提供会话域与常用命令的默认应答（用例按需覆盖）。
func stdHandler(rec *recorder, method string, params json.RawMessage) (any, error) {
	switch method {
	case "Network.enable", "Runtime.enable", "Log.enable", "Page.enable",
		"Page.bringToFront", "Page.reload", "Emulation.setDeviceMetricsOverride",
		"Emulation.setCPUThrottlingRate", "Network.emulateNetworkConditions",
		"Emulation.setUserAgentOverride", "Emulation.setEmulatedMedia",
		"Emulation.setGeolocationOverride", "Network.setExtraHTTPHeaders",
		"Input.insertText", "Input.dispatchKeyEvent", "Input.dispatchMouseEvent",
		"DOM.scrollIntoViewIfNeeded", "Page.handleJavaScriptDialog":
		return map[string]any{}, nil
	case "Runtime.evaluate":
		// viewportSize / waitFor / evaluate_script / 动作注入 的兜底应答
		var p struct {
			Expression string `json:"expression"`
		}
		_ = json.Unmarshal(params, &p)
		if contains(p.Expression, "findEl") {
			// 动作/定位注入：按表达式形态返回对应结果
			if contains(p.Expression, "return f.el") {
				// objectExpr（returnByValue:false）→ objectId
				return map[string]any{"result": map[string]any{"objectId": "obj-1"}}, nil
			}
			if contains(p.Expression, "ok: true, rect") {
				// rectExpr → 实时矩形
				return map[string]any{"result": map[string]any{"value": `{"ok":true,"rect":{"x":10,"y":20,"w":100,"h":30}}`}}, nil
			}
			return map[string]any{"result": map[string]any{"value": `{"ok":true,"tag":"button","text":"设备 A","value":""}`}}, nil
		}
		if contains(p.Expression, "innerWidth") {
			return map[string]any{"result": map[string]any{"value": map[string]any{"w": 1600.0, "h": 900.0}}}, nil
		}
		if contains(p.Expression, "body.innerText") {
			return map[string]any{"result": map[string]any{"value": true}}, nil
		}
		if contains(p.Expression, "location.href") {
			return map[string]any{"result": map[string]any{"value": "http://localhost/"}}, nil
		}
		return map[string]any{"result": map[string]any{"value": nil}}, nil
	case "Runtime.callFunctionOn":
		return map[string]any{"result": map[string]any{"value": map[string]any{"ok": true, "tag": "input"}}}, nil
	case "DOM.requestNode":
		return map[string]any{"nodeId": 5}, nil
	default:
		return map[string]any{}, nil
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// dispatchDialog 模拟 CDP 推送 dialog 打开事件。
func dispatchDialog(h *Hub, typ, message string) {
	b, _ := json.Marshal(map[string]any{"type": typ, "message": message})
	h.dispatch(cdp.Event{Method: "Page.javascriptDialogOpening", Params: b})
}

// withRefs 注入 uid 身份表。
func withRefs(h *Hub, refs uimap.RefTable) { h.ReplaceRefs(refs) }

// refOf 构造测试用元素身份。
func refOf(tag, text string, x, y float64) uimap.RefInfo {
	return uimap.RefInfo{Tag: tag, Text: text, AriaLabel: "", X: x, Y: y, W: 100, H: 30}
}

// ctxBg 测试 context。
func ctxBg() context.Context { return context.Background() }
