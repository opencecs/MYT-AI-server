package browser

// input_test.go — input 类工具单测：Cdp 命令序列断言
// （click 双击/单击、hover、press_key、fill/fill_form 参数映射、upload_file、
// handle_dialog）。

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/moyunteng/myt/internal/mcpserver/uimap"
)

// TestClickUIDInjectsActionJS click(uid)：单会话注入「定位+动作」JS
// （身份匹配 + 页面内点击事件序列），不再使用任何 DOM 节点 id 命令。
func TestClickUIDInjectsActionJS(t *testing.T) {
	h, rec := newTestHub(t, stdHandler)
	withRefs(h, uimap.RefTable{"1_0": refOf("button", "设备 A", 10, 20)})
	if err := h.clickUID(ctxBg(), "1_0", false); err != nil {
		t.Fatalf("clickUID 失败: %v", err)
	}
	for _, m := range rec.methods() {
		if m == "DOM.resolveNode" || m == "DOM.getContentQuads" || m == "DOM.scrollIntoViewIfNeeded" {
			t.Fatalf("动作链路不应再使用节点 id 命令: %s", m)
		}
	}
	if rec.count("Runtime.evaluate") != 1 {
		t.Fatalf("click 应恰好注入一次动作 JS，实际 %d", rec.count("Runtime.evaluate"))
	}
	if !strings.Contains(rec.calls[0].Params["expression"].(string), `"action":"click"`) {
		t.Fatal("动作 JS 应含 click action")
	}
}

// TestClickUIDDblClick dblClick=true → 动作参数携带 dbl=true。
func TestClickUIDDblClick(t *testing.T) {
	h, rec := newTestHub(t, stdHandler)
	withRefs(h, uimap.RefTable{"1_0": refOf("button", "设备 A", 10, 20)})
	if err := h.clickUID(ctxBg(), "1_0", true); err != nil {
		t.Fatalf("clickUID 失败: %v", err)
	}
	if !strings.Contains(rec.calls[0].Params["expression"].(string), `"dbl":true`) {
		t.Fatal("双击应在动作参数中携带 dbl=true")
	}
}

// TestClickUIDExpiredRef 失效 uid（不在快照表）→ 明确报错（不派发任何事件）。
func TestClickUIDExpiredRef(t *testing.T) {
	h, rec := newTestHub(t, stdHandler)
	err := h.clickUID(ctxBg(), "9_9", false)
	if err == nil || !strings.Contains(err.Error(), "不在当前快照") {
		t.Fatalf("期望「uid 不在当前快照」错误, got %v", err)
	}
	if n := rec.count("Input.dispatchMouseEvent"); n != 0 {
		t.Fatalf("过期 uid 不应派发事件，实际 %d", n)
	}
}

// TestHoverSequence hover：身份定位 → 元素中心可信 mouseMoved。
func TestHoverSequence(t *testing.T) {
	h, rec := newTestHub(t, stdHandler)
	withRefs(h, uimap.RefTable{"1_0": refOf("button", "设备 A", 10, 20)})
	if err := h.hoverUID(ctxBg(), "1_0"); err != nil {
		t.Fatalf("hoverUID 失败: %v", err)
	}
	last := rec.calls[len(rec.calls)-1]
	if last.Method != "Input.dispatchMouseEvent" || last.Params["type"] != "mouseMoved" {
		t.Fatalf("hover 应以 mouseMoved 结束: %v", last)
	}
	// mock 返回 rect {x:10,y:20,w:100,h:30} → 中心 (60,35)
	if last.Params["x"] != 60.0 || last.Params["y"] != 35.0 {
		t.Fatalf("mouseMoved 坐标 = %v, want (60,35)", last.Params)
	}
}

// TestPressKeySequence press_key：keyDown + keyUp，带虚拟键码。
func TestPressKeySequence(t *testing.T) {
	h, rec := newTestHub(t, stdHandler)
	if err := h.pressKey(ctxBg(), "Enter", keyCodes["Enter"]); err != nil {
		t.Fatalf("pressKey 失败: %v", err)
	}
	types := []string{}
	for _, c := range rec.calls {
		if c.Method == "Input.dispatchKeyEvent" {
			types = append(types, c.Params["type"].(string))
			if c.Params["windowsVirtualKeyCode"] != 13.0 {
				t.Fatalf("Enter 虚拟键码应为 13: %v", c.Params)
			}
		}
	}
	if len(types) != 2 || types[0] != "keyDown" || types[1] != "keyUp" {
		t.Fatalf("按键序列 = %v, want [keyDown keyUp]", types)
	}
}

// TestFillInjectsActionJS fill：单会话注入「定位+填充」JS，value 内嵌于
// 表达式（React 受控组件经原生 value setter + input/change 事件响应）。
func TestFillInjectsActionJS(t *testing.T) {
	h, rec := newTestHub(t, stdHandler)
	withRefs(h, uimap.RefTable{"1_1": refOf("input", "搜索", 10, 20)})
	if err := h.fillOne(ctxBg(), "1_1", "hello"); err != nil {
		t.Fatalf("fillOne 失败: %v", err)
	}
	if rec.count("Runtime.evaluate") != 1 {
		t.Fatalf("fill 应恰好注入一次动作 JS，实际 %d", rec.count("Runtime.evaluate"))
	}
	expr := rec.calls[0].Params["expression"].(string)
	if !strings.Contains(expr, `"action":"fill"`) || !strings.Contains(expr, `"value":"hello"`) {
		t.Fatalf("动作 JS 应含 fill action 与 value: %s", expr)
	}
	if !strings.Contains(expr, "HTMLInputElement.prototype") ||
		!strings.Contains(expr, "Object.getOwnPropertyDescriptor(proto, 'value')") {
		t.Fatal("fill JS 应优先取 HTMLInputElement/TextAreaElement 原型的原生 value setter")
	}
	if !strings.Contains(expr, "Object.getPrototypeOf(el)") {
		t.Fatal("fill JS 应有原型链兜底取 setter")
	}
	if !strings.Contains(expr, "typeof d.set === 'function'") {
		t.Fatal("fill JS 调用 setter 前应判空（防 setter.call is not a function）")
	}
}

// TestFillFormDialogInterrupt fill_form：第二个元素触发 dialog → 中断，
// 报告 interruptedUid 且剩余元素不填充（官方 fill_form 中断语义）。
func TestFillFormDialogInterrupt(t *testing.T) {
	var h *Hub
	h, rec := newTestHub(t, func(rec *recorder, method string, params json.RawMessage) (any, error) {
		res, err := stdHandler(rec, method, params)
		if err != nil {
			return res, err
		}
		// 第二个元素（value=boom）触发 dialog：模拟 CDP 事件
		if method == "Runtime.evaluate" && contains(string(params), "boom") {
			dispatchDialog(h, "alert", "blocked")
		}
		return res, nil
	})
	withRefs(h, uimap.RefTable{"1_0": refOf("input", "a", 10, 20), "1_1": refOf("input", "b", 10, 60), "1_2": refOf("input", "c", 10, 100)})
	out, err := h.fillForm(ctxBg(), []fillFormElement{
		{UID: "1_0", Value: "a"},
		{UID: "1_1", Value: "boom"},
		{UID: "1_2", Value: "c"},
	})
	if err != nil {
		t.Fatalf("fillForm 失败: %v", err)
	}
	if out["result"] != "dialog_interrupted" {
		t.Fatalf("result = %v, want dialog_interrupted", out["result"])
	}
	if out["interruptedUid"] != "1_1" {
		t.Fatalf("interruptedUid = %v, want 1_1", out["interruptedUid"])
	}
	if out["filled"] != 1 {
		t.Fatalf("filled = %v, want 1", out["filled"])
	}
	if !strings.Contains(out["message"].(string), "The remaining elements were not filled out.") {
		t.Fatalf("message 缺少剩余未填充说明: %v", out["message"])
	}
	// 第三个元素不应被填充：动作注入只出现 2 次
	if n := rec.count("Runtime.evaluate"); n != 2 {
		t.Fatalf("dialog 中断后不应继续填充，动作注入次数 = %d, want 2", n)
	}
}

// TestFillFormComplete 全部填充成功 → filled 计数。
func TestFillFormComplete(t *testing.T) {
	h, _ := newTestHub(t, stdHandler)
	withRefs(h, uimap.RefTable{"1_0": refOf("input", "a", 10, 20), "1_1": refOf("input", "b", 10, 60)})
	out, err := h.fillForm(ctxBg(), []fillFormElement{
		{UID: "1_0", Value: "true"},
		{UID: "1_1", Value: "false"},
	})
	if err != nil {
		t.Fatalf("fillForm 失败: %v", err)
	}
	if out["result"] != "ok" || out["filled"] != 2 {
		t.Fatalf("结果异常: %v", out)
	}
}

// TestFillToggleRejectsNonBool checkbox 填非布尔值 → 明确报错（官方文案）。
func TestFillToggleRejectsNonBool(t *testing.T) {
	h, _ := newTestHub(t, func(rec *recorder, method string, params json.RawMessage) (any, error) {
		if method == "Runtime.evaluate" && contains(string(params), "yes") {
			return map[string]any{"result": map[string]any{"value": `{"ok":false,"reason":"toggle-value","err":"Checkboxes, radio boxes and toggles require \"true\" or \"false\" value, but yes was used"}`}}, nil
		}
		return stdHandler(rec, method, params)
	})
	withRefs(h, uimap.RefTable{"1_0": refOf("input", "开关", 10, 20)})
	err := h.fillOne(ctxBg(), "1_0", "yes")
	if err == nil || !strings.Contains(err.Error(), "true\" or \"false\"") {
		t.Fatalf("期望官方 toggle 报错文案, got %v", err)
	}
}

// TestUploadFileCommand upload_file：会话内身份定位 → DOM.requestNode →
// DOM.setFileInputFiles（files + nodeId）。
func TestUploadFileCommand(t *testing.T) {
	h, rec := newTestHub(t, stdHandler)
	withRefs(h, uimap.RefTable{"1_0": refOf("input", "文件", 10, 20)})
	err := h.uploadFile(ctxBg(), "1_0", []string{`C:\tmp\a.png`, `C:\tmp\b.png`})
	if err != nil {
		t.Fatalf("uploadFile 失败: %v", err)
	}
	var call *recordedCall
	for i := range rec.calls {
		if rec.calls[i].Method == "DOM.setFileInputFiles" {
			call = &rec.calls[i]
		}
	}
	if call == nil {
		t.Fatal("upload_file 应调用 DOM.setFileInputFiles")
	}
	files, _ := call.Params["files"].([]any)
	if len(files) != 2 || files[0] != `C:\tmp\a.png` {
		t.Fatalf("files 参数异常: %v", call.Params["files"])
	}
	if call.Params["nodeId"] != 5.0 {
		t.Fatalf("setFileInputFiles nodeId = %v, want 5（DOM.requestNode 所得）", call.Params["nodeId"])
	}
	// 定位环节应在同会话 evaluate 中按身份完成（不再有 DOM.resolveNode）
	for _, c := range rec.calls {
		if c.Method == "DOM.resolveNode" {
			t.Fatal("upload 链路不应使用 DOM.resolveNode")
		}
	}
}

// TestHandleDialog handle_dialog：accept/dismiss 走 Page.handleJavaScriptDialog
// 并清除 dialog 状态。
func TestHandleDialog(t *testing.T) {
	h, rec := newTestHub(t, stdHandler)
	dispatchDialog(h, "confirm", "确定删除？")
	if h.GetDialog() == nil {
		t.Fatal("dialog 事件后应有未处理 dialog")
	}
	if err := h.respondDialog(ctxBg(), "accept", ""); err != nil {
		t.Fatalf("respondDialog 失败: %v", err)
	}
	var call *recordedCall
	for i := range rec.calls {
		if rec.calls[i].Method == "Page.handleJavaScriptDialog" {
			call = &rec.calls[i]
		}
	}
	if call == nil {
		t.Fatal("accept 应调用 Page.handleJavaScriptDialog")
	}
	if call.Params["accept"] != true {
		t.Fatalf("accept 参数异常: %v", call.Params)
	}
	if h.GetDialog() != nil {
		t.Fatal("处理后 dialog 状态应清除")
	}
	// 无 dialog 时明确报错（官方 'No open dialog found'）
	if err := h.respondDialog(ctxBg(), "dismiss", ""); err == nil || !strings.Contains(err.Error(), "No open dialog") {
		t.Fatalf("期望 No open dialog found, got %v", err)
	}
}

// TestDialogAutoAccept evaluate_script 等执行期间 autoDialog 生效：
// dialog 事件到达即自动应答（官方 waitForEventsAfterAction handleDialog）。
func TestDialogAutoAccept(t *testing.T) {
	h, rec := newTestHub(t, stdHandler)
	if _, err := h.Session(ctxBg()); err != nil {
		t.Fatalf("建立事件会话失败: %v", err)
	}
	h.mu.Lock()
	h.autoDialog = "accept"
	h.mu.Unlock()
	dispatchDialog(h, "beforeunload", "离开？")
	// 自动应答是异步的：轮询等待
	deadline := int64(0)
	for i := 0; i < 50; i++ {
		if rec.count("Page.handleJavaScriptDialog") > 0 {
			deadline = 1
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if deadline == 0 {
		t.Fatal("autoDialog=accept 时 dialog 应被自动应答")
	}
}

// TestActRefErrorMessages 报错文案二分：uid 不在快照表 vs 在表但页面内
// 找不到（界面已变化）。
func TestActRefErrorMessages(t *testing.T) {
	h, _ := newTestHub(t, func(rec *recorder, method string, params json.RawMessage) (any, error) {
		if method == "Runtime.evaluate" && contains(string(params), "findEl") {
			return map[string]any{"result": map[string]any{"value": `{"ok":false,"reason":"not-found"}`}}, nil
		}
		return stdHandler(rec, method, params)
	})
	withRefs(h, uimap.RefTable{"1_0": refOf("button", "设备 A", 10, 20)})

	// uid 不在快照表 → 「不在当前快照」
	err := h.clickUID(ctxBg(), "9_9", false)
	if err == nil || !strings.Contains(err.Error(), "不在当前快照") {
		t.Fatalf("期望「uid 不在当前快照」，got %v", err)
	}
	// 在表但页面内未找到 → 「页面已变化」
	err = h.clickUID(ctxBg(), "1_0", false)
	if err == nil || !strings.Contains(err.Error(), "页面已变化") {
		t.Fatalf("期望「页面已变化，无法唯一定位」，got %v", err)
	}
}

// TestActJSExceptionStructuredError 页面 JS 抛异常 → 结构化错误带出真实
// 异常描述与表达式片段（不再裸报「unexpected end of JSON input」）。
func TestActJSExceptionStructuredError(t *testing.T) {
	h, _ := newTestHub(t, func(rec *recorder, method string, params json.RawMessage) (any, error) {
		if method == "Runtime.evaluate" && contains(string(params), "findEl") {
			// handler 返回值即协议 result：exceptionDetails 在顶层
			return map[string]any{
				"exceptionDetails": map[string]any{
					"text":      "Uncaught",
					"exception": map[string]any{"description": "TypeError: Cannot read properties of undefined (reading 'onChange')"},
				},
			}, nil
		}
		return stdHandler(rec, method, params)
	})
	withRefs(h, uimap.RefTable{"1_0": refOf("input", "搜索", 10, 20)})
	err := h.fillOne(ctxBg(), "1_0", "hello")
	if err == nil || !strings.Contains(err.Error(), "动作 JS 执行异常") ||
		!strings.Contains(err.Error(), "TypeError: Cannot read properties") {
		t.Fatalf("应返回含异常描述的结构化错误, got %v", err)
	}
	if strings.Contains(err.Error(), "unexpected end of JSON input") {
		t.Fatalf("不应再出现空 JSON 裸报错: %v", err)
	}
}

// TestActJSEmptyResultStructuredError evaluate 正常返回但 value 缺失
// （undefined 序列化为空）→ 结构化错误（fill/fill_form/type_text/press_key
// 共用此路径）。
func TestActJSEmptyResultStructuredError(t *testing.T) {
	h, _ := newTestHub(t, func(rec *recorder, method string, params json.RawMessage) (any, error) {
		if method == "Runtime.evaluate" && contains(string(params), "findEl") {
			return map[string]any{"result": map[string]any{}}, nil
		}
		return stdHandler(rec, method, params)
	})
	withRefs(h, uimap.RefTable{"1_0": refOf("input", "搜索", 10, 20)})
	err := h.fillOne(ctxBg(), "1_0", "hello")
	if err == nil || !strings.Contains(err.Error(), "未返回结果") ||
		!strings.Contains(err.Error(), "表达式片段") {
		t.Fatalf("应返回「未返回结果」结构化错误, got %v", err)
	}
}
