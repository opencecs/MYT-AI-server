package uimap

// analyze_test.go — 「观察即分析」集成测试（注入采集版）：mock Runtime.evaluate
// 返回采集 JSON，验证 state.modal（弹窗检测 + 建议操作）、interactiveElements
// （uid 与 click 的 refs 表打通）与 summary（搜索框/折叠分组场景）。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/moyunteng/myt/internal/mcpserver/cdp/mock"
	"github.com/moyunteng/myt/internal/mcpserver/uimap/axtree"
)

// evalResultOf 构造注入采集返回 JSON（Go 侧组装，避免手写转义）。
func evalResultOf(activeTab string, elems []map[string]any, overlays []map[string]any) string {
	b, _ := json.Marshal(map[string]any{
		"viewport":  map[string]any{"w": 1720, "h": 1000},
		"activeTab": activeTab,
		"elements":  elems,
		"overlays":  overlays,
	})
	return string(b)
}

// elem 构造单个注入元素。
func elem(tag, text string, x, y, w, h float64, extra map[string]any) map[string]any {
	m := map[string]any{
		"tag": tag, "text": text, "ariaLabel": "", "placeholder": "", "value": "",
		"disabled": false, "expanded": "",
		"x": x, "y": y, "w": w, "h": h,
	}
	for k, v := range extra {
		m[k] = v
	}
	return m
}

// TestAnalyzeStateAndInteractive 弹窗场景：全屏 modal-overlay + 弹窗内
// 「稍后提醒」按钮 → modal.suggested 命中、uid 在 refs 表可解析。
func TestAnalyzeStateAndInteractive(t *testing.T) {
	evalJSON := evalResultOf("", []map[string]any{
		elem("button", "稍后提醒", 800, 480, 80, 40, nil),
		elem("button", "创建云机", 100, 200, 100, 40, nil),
	}, []map[string]any{
		{"x": 0, "y": 0, "w": 1720, "h": 1000, "z": "100", "cls": "modal-overlay", "role": ""},
	})
	sink := &refCapture{}
	c := NewCollector(mock.New(t, mockCDPHandler(t, map[string]any{"nodes": fakeAXNodes()}, evalJSON)), sink)
	out, err := c.Collect(context.Background(), Args{})
	if err != nil {
		t.Fatalf("ui_map 失败: %v", err)
	}
	m := out.(map[string]any)

	// state.modal：检测到弹窗 + 建议操作指向「稍后提醒」
	state, _ := m["state"].(map[string]any)
	if state == nil {
		t.Fatalf("输出应含 state: %v", m)
	}
	modal, _ := state["modal"].(map[string]any)
	if modal == nil {
		t.Fatalf("应检测到 modal: %v", state)
	}
	sug, _ := modal["suggested"].(map[string]any)
	if sug == nil || sug["text"] != "稍后提醒" {
		t.Fatalf("suggested 应为「稍后提醒」: %v", modal["suggested"])
	}
	if hint, _ := modal["hint"].(string); hint == "" {
		t.Fatalf("modal 应含 hint: %v", modal)
	}

	// interactiveElements：两个可见按钮，uid 可在 refs 表查到（click 可命中）
	inters, _ := m["interactiveElements"].([]map[string]any)
	if len(inters) != 2 {
		t.Fatalf("interactiveElements 应有 2 项: %v", m["interactiveElements"])
	}
	sugUID, _ := sug["uid"].(string)
	info, ok := sink.refs[sugUID]
	if !ok || info.Tag != "button" || info.Text != "稍后提醒" {
		t.Fatalf("suggested uid=%s 应登记元素身份（button/稍后提醒），实际 %+v,%v", sugUID, info, ok)
	}
}

// TestSummarySearchAndGroups 搜索框 + 折叠分组场景：summary 应正确识别页面、
// 列出搜索框与折叠分组区块，并建议「先搜索过滤」。
func TestSummarySearchAndGroups(t *testing.T) {
	elems := []map[string]any{
		elem("input", "", 10, 20, 320, 24, map[string]any{"placeholder": "搜索 IP / 备注"}),
		elem("button", "", 10, 60, 320, 24, map[string]any{"expanded": "false"}),
		elem("button", "", 10, 90, 320, 24, map[string]any{"expanded": "false"}),
	}
	for i := 0; i < 8; i++ {
		elems = append(elems, elem("button", fmt.Sprintf("192.0.2.%d", 200+i), 350, 120, 100, 30, nil))
	}
	evalJSON := evalResultOf("host", elems, nil)
	sink := &refCapture{}
	c := NewCollector(mock.New(t, mockCDPHandler(t, map[string]any{"nodes": fakeAXNodes()}, evalJSON)), sink)
	out, err := c.Collect(context.Background(), Args{})
	if err != nil {
		t.Fatalf("ui_map 失败: %v", err)
	}
	m := out.(map[string]any)
	sum, _ := m["summary"].(map[string]any)
	if sum == nil {
		t.Fatalf("输出应含 summary: %v", m)
	}
	page, _ := sum["page"].(string)
	if !strings.Contains(page, "云机管理页") {
		t.Fatalf("page 应识别为云机管理页: %q", page)
	}
	blocks, _ := sum["blocks"].([]string)
	joined := strings.Join(blocks, "\n")
	if !strings.Contains(joined, "搜索框") || !strings.Contains(joined, "折叠") {
		t.Fatalf("blocks 应含搜索框与折叠分组描述:\n%s", joined)
	}
	if next, _ := sum["next"].(string); next != "先在搜索框输入目标 IP/名称过滤，再点击对应条目" {
		t.Fatalf("next 应建议搜索过滤: %q", next)
	}
	// uidNote 与 uid 生命周期（提示修正 ③）
	if note, _ := m["uidNote"].(string); !strings.Contains(note, "仅本次快照有效") {
		t.Fatalf("uidNote 应声明 uid 生命周期: %q", note)
	}
	// state.activeTab 来自注入采集
	if state, _ := m["state"].(map[string]any); state["activeTab"] != "host" {
		t.Fatalf("state.activeTab = %v, want host", state["activeTab"])
	}
}

// mockCDPHandler 构造覆盖注入采集主路径 + AX 树路径的 mock CDP handler：
// Runtime.evaluate 返回 evalJSON（注入采集结果）；DOM 域按打标对齐返回
// backendNodeId = 1000 + 文档序；AX 树返回 axResp。
func mockCDPHandler(t *testing.T, axResp map[string]any, evalJSON string) mock.Handler {
	t.Helper()
	return func(method string, _ json.RawMessage) (any, error) {
		switch method {
		case "Runtime.evaluate":
			return map[string]any{"result": map[string]any{"value": evalJSON}}, nil
		case "Accessibility.enable":
			return map[string]any{}, nil
		case "Accessibility.getFullAXTree":
			return axResp, nil
		default:
			return nil, errors.New("unexpected: " + method)
		}
	}
}

// evalFixtureElements 从注入采集 JSON 里数元素（mock 构造用）。
func evalFixtureElements(evalJSON string) []any {
	var out struct {
		Elements []any `json:"elements"`
	}
	_ = json.Unmarshal([]byte(evalJSON), &out)
	return out.Elements
}

// evalFixtureSimple 单元素注入采集结果。
func evalFixtureSimple() string {
	return `{"viewport":{"w":1600,"h":900},"activeTab":"host","elements":[` +
		`{"tag":"button","text":"192.0.2.202","x":400,"y":300,"w":100,"h":30}` +
		`],"overlays":[]}`
}

// --- 假 AX 树（payload 形态，与 CDP 返回一致） ---

func fakeAXNodes() []axtree.AxNode {
	return []axtree.AxNode{
		{NodeID: "n1", BackendNodeID: 1, Role: axtree.AxValue{Value: "RootWebArea"}, ChildIDs: []string{"n2"}},
		{NodeID: "n2", BackendNodeID: 2, Role: axtree.AxValue{Value: "generic"}, ChildIDs: []string{"n3", "n4", "n5", "n6", "n7"}},
		{NodeID: "n3", BackendNodeID: 42, Role: axtree.AxValue{Value: "button"}, Name: axtree.AxValue{Value: "192.0.2.202"}},
		{NodeID: "n4", BackendNodeID: 17, Role: axtree.AxValue{Value: "textbox"}, Name: axtree.AxValue{Value: "搜索实例名称"}},
		{NodeID: "n5", BackendNodeID: 55, Role: axtree.AxValue{Value: "button"}, Name: axtree.AxValue{Value: "创建云机"},
			Properties: []axtree.AxProperty{{Name: "disabled", Value: axtree.AxValue{Value: true}}}},
		{NodeID: "n6", BackendNodeID: 99, Role: axtree.AxValue{Value: "none"}, Name: axtree.AxValue{Value: "装饰节点"}},
		{NodeID: "n7", BackendNodeID: 77, Role: axtree.AxValue{Value: "checkbox"}, Name: axtree.AxValue{Value: "自动开机"},
			Properties: []axtree.AxProperty{{Name: "checked", Value: axtree.AxValue{Value: "true"}}}},
	}
}

// TestRefInfoJSONKeys 防回归：RefInfo 序列化键必须为小写 x/y/w/h——
// act.go 的 locatePrelude 按 I.x/I.y/I.w/I.h 读取，大写键会导致
// undefined → NaN 比较 → findEl 恒 not-found（真机已实证）。
func TestRefInfoJSONKeys(t *testing.T) {
	b, err := json.Marshal(RefInfo{Tag: "button", Text: "设备 A", X: 8, Y: 335, W: 60, H: 56})
	if err != nil {
		t.Fatalf("marshal 失败: %v", err)
	}
	s := string(b)
	for _, want := range []string{`"x":8`, `"y":335`, `"w":60`, `"h":56`, `"tag":"button"`, `"text":"设备 A"`, `"ariaLabel":""`} {
		if !strings.Contains(s, want) {
			t.Fatalf("RefInfo JSON 缺少 %q（大写键 = 定位全盲）:\n%s", want, s)
		}
	}
	if strings.Contains(s, `"X":`) || strings.Contains(s, `"Y":`) {
		t.Fatalf("RefInfo JSON 不应有大写坐标键:\n%s", s)
	}
}

// TestModalSuggestedExcludesTitlebar 右键菜单场景：全屏遮罩内含窗口标题栏
// 「关闭」按钮——它不作为 suggested 候选（点它会退出客户端），但仍列在
// buttons 中并标注 titlebar=true；建议落在真实菜单项上。
func TestModalSuggestedExcludesTitlebar(t *testing.T) {
	evalJSON := evalResultOf("", []map[string]any{
		elem("button", "关闭", 1660, 8, 40, 24, nil),   // 标题栏控件
		elem("button", "新机", 800, 300, 100, 30, nil), // 菜单项
		elem("button", "取消", 800, 340, 100, 30, nil), // 菜单项
	}, []map[string]any{
		{"x": 0, "y": 0, "w": 1720, "h": 1000, "z": "100", "cls": "context-menu-backdrop", "role": ""},
	})
	sink := &refCapture{}
	c := NewCollector(mock.New(t, mockCDPHandler(t, map[string]any{"nodes": fakeAXNodes()}, evalJSON)), sink)
	out, err := c.Collect(context.Background(), Args{})
	if err != nil {
		t.Fatalf("ui_map 失败: %v", err)
	}
	modal := out.(map[string]any)["state"].(map[string]any)["modal"].(map[string]any)
	sug, _ := modal["suggested"].(map[string]any)
	// 关闭（titlebar）被排除；「新机」无关键词命中 → 按优先级落在「取消」
	if sug == nil || sug["text"] != "取消" {
		t.Fatalf("suggested 应为「取消」（排除 titlebar 关闭），实际 %v", modal["suggested"])
	}
	buttons, _ := modal["buttons"].([]map[string]any)
	if len(buttons) != 3 {
		t.Fatalf("buttons 应仍列出全部 3 个按钮，实际 %d", len(buttons))
	}
	for _, b := range buttons {
		if b["text"] == "关闭" && b["titlebar"] != true {
			t.Fatalf("关闭按钮应标注 titlebar=true: %v", b)
		}
	}
}
