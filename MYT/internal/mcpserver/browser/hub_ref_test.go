package browser

// hub_ref_test.go — collector→hub 接线全链路回归：
// server.go 装配必须保证「ui_map 产出 refs 的 sink」与「click 解析 uid 的 Hub」
// 是同一实例，否则 hub.lookupRef 恒 miss、click 恒报「uid 已过期」（历史 bug）。

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/moyunteng/myt/internal/mcpserver/cdp/mock"
	"github.com/moyunteng/myt/internal/mcpserver/uimap"
)

// axPayload 最小 AX 树：RootWebArea → button(42) "设备 A" + textbox(17) "搜索"。
func axPayload() map[string]any {
	return map[string]any{"nodes": []map[string]any{
		{"nodeId": "n1", "backendNodeId": 1, "role": map[string]any{"value": "RootWebArea"}, "childIds": []string{"n2", "n3"}},
		{"nodeId": "n2", "backendNodeId": 42, "role": map[string]any{"value": "button"}, "name": map[string]any{"value": "设备 A"}},
		{"nodeId": "n3", "backendNodeId": 17, "role": map[string]any{"value": "textbox"}, "name": map[string]any{"value": "搜索"}},
	}}
}

// evalPayload 注入采集应答：侧栏「设备 A」按钮 + 主区「搜索」输入框。
// （mock 的 DOM 域对齐固定返回 backendNodeId = 1000 + 文档序。）
func evalPayload() string {
	b, _ := json.Marshal(map[string]any{
		"viewport":  map[string]any{"w": 1600, "h": 900},
		"activeTab": "host",
		"elements": []map[string]any{
			{"tag": "button", "text": "设备 A", "ariaLabel": "", "placeholder": "", "value": "",
				"disabled": false, "expanded": "", "x": 50, "y": 350, "w": 100, "h": 100},
			{"tag": "input", "text": "搜索", "ariaLabel": "", "placeholder": "搜索", "value": "",
				"disabled": false, "expanded": "", "x": 800, "y": 350, "w": 100, "h": 30},
		},
		"overlays": []map[string]any{},
	})
	return string(b)
}

// newWiredHub 按生产装配顺序建 collector→hub 全链路（mock CDP）。
func newWiredHub(t *testing.T, handler mock.Handler) (*uimap.Collector, *Hub) {
	t.Helper()
	base := mock.New(t, handler)
	collector := uimap.NewCollector(base, nil)
	hub := NewHub(base, collector)
	collector.SetSink(hub) // server.go 的接线方式
	return collector, hub
}

// defaultHandler 覆盖注入采集主路径（Runtime.evaluate + DOM 域对齐）与 AX 树路径。
// DOM 对齐：nodeId 7+i → backendNodeId 1000+i。
func defaultHandler(evalJSON string) mock.Handler {
	var parsed struct {
		Elements []any `json:"elements"`
	}
	_ = json.Unmarshal([]byte(evalJSON), &parsed)
	n := len(parsed.Elements)
	nodeIDs := make([]int64, n)
	flat := make([]map[string]any, n)
	for i := 0; i < n; i++ {
		nodeIDs[i] = int64(7 + i)
		flat[i] = map[string]any{"nodeId": 7 + i, "backendNodeId": 1000 + i}
	}
	return func(method string, _ json.RawMessage) (any, error) {
		switch method {
		case "Runtime.evaluate":
			return map[string]any{"result": map[string]any{"value": evalJSON}}, nil
		case "DOM.getDocument":
			return map[string]any{"root": map[string]any{"nodeId": 1}}, nil
		case "DOM.querySelectorAll":
			return map[string]any{"nodeIds": nodeIDs}, nil
		case "DOM.getFlattenedDocument":
			return map[string]any{"nodes": flat}, nil
		case "Accessibility.enable":
			return map[string]any{}, nil
		case "Accessibility.getFullAXTree":
			return axPayload(), nil
		case "Page.getResourceTree":
			return map[string]any{"frameTree": map[string]any{
				"frame": map[string]any{"loaderId": "L1"},
			}}, nil
		default:
			return map[string]any{}, nil
		}
	}
}

// TestCollectorWiresRefsIntoHub：ui_map 产出的元素 uid 必须能被 hub.lookupRef
// 命中（click 链路前置）；元素 uid 占 1_0/1_1，树行 uid 顺延（1_2 起）。
func TestCollectorWiresRefsIntoHub(t *testing.T) {
	collector, hub := newWiredHub(t, defaultHandler(evalPayload()))
	res, err := collector.Collect(context.Background(), uimap.Args{})
	if err != nil {
		t.Fatalf("Collect 失败: %v", err)
	}
	tree, _ := res.(map[string]any)["tree"].(string)
	if !strings.Contains(tree, `uid=1_2 button "设备 A"`) {
		t.Fatalf("快照缺树行（元素 uid 占用 1_0/1_1 后顺延）:\n%s", tree)
	}
	// 元素 uid（1_0/1_1）登记身份；树行 uid（1_2）不再注册 refs
	if info, ok := hub.lookupRef("1_0"); !ok || info.Tag != "button" || info.Text != "设备 A" {
		t.Fatalf("hub.lookupRef(1_0) = %+v,%v, want button/设备 A", info, ok)
	}
	if info, ok := hub.lookupRef("1_1"); !ok || info.Text != "搜索" {
		t.Fatalf("hub.lookupRef(1_1) = %+v,%v, want 搜索", info, ok)
	}
	if _, ok := hub.lookupRef("1_2"); ok {
		t.Fatal("树行 uid 不应注册 refs（uid 以 interactiveElements 为准）")
	}
}

// TestRegionFiltersInteractiveElements：region:sidebar 只输出侧栏元素
// （tree 为全量辅助输出，不受 region 影响）。
func TestRegionFiltersInteractiveElements(t *testing.T) {
	collector, _ := newWiredHub(t, defaultHandler(evalPayload()))
	res, err := collector.Collect(context.Background(), uimap.Args{Region: "sidebar"})
	if err != nil {
		t.Fatalf("Collect 失败: %v", err)
	}
	m := res.(map[string]any)
	inters, _ := m["interactiveElements"].([]map[string]any)
	if len(inters) != 1 {
		t.Fatalf("sidebar 应只含 1 个元素: %v", m["interactiveElements"])
	}
	if inters[0]["text"] != "设备 A" {
		t.Fatalf("sidebar 元素应为「设备 A」: %v", inters[0])
	}
	tree, _ := m["tree"].(string)
	if !strings.Contains(tree, `"设备 A"`) || !strings.Contains(tree, `"搜索"`) {
		t.Fatalf("tree 应为全量辅助输出:\n%s", tree)
	}
	if note, _ := m["note"].(string); !strings.Contains(note, "region") {
		t.Fatalf("region 非 all 时应注明过滤范围: %q", note)
	}
}

// TestRegionZeroHitKeepsTree：region 零命中时 interactiveElements 为空，
// tree 仍全量输出（不再返回空结果）。
func TestRegionZeroHitKeepsTree(t *testing.T) {
	collector, hub := newWiredHub(t, defaultHandler(evalPayload()))
	res, err := collector.Collect(context.Background(), uimap.Args{Region: "toolbar"})
	if err != nil {
		t.Fatalf("Collect 失败: %v", err)
	}
	m := res.(map[string]any)
	if inters, _ := m["interactiveElements"].([]map[string]any); len(inters) != 0 {
		t.Fatalf("dialog region 无命中，interactiveElements 应为空: %v", inters)
	}
	tree, _ := m["tree"].(string)
	if !strings.Contains(tree, `"设备 A"`) {
		t.Fatalf("tree 应保持全量输出:\n%s", tree)
	}
	// refs 仍全量注册（region 只影响输出展示）
	if info, ok := hub.lookupRef("1_1"); !ok || info.Text != "搜索" {
		t.Fatalf("region 过滤不应影响 refs 注册: %+v,%v", info, ok)
	}
}
