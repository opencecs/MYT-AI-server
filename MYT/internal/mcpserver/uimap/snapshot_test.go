package uimap

import (
	"context"
	"strings"
	"testing"

	"github.com/moyunteng/myt/internal/mcpserver/cdp/mock"
	"github.com/moyunteng/myt/internal/mcpserver/uimap/axtree"
)

// --- uid 映射：Collect 产出 refs 到 RefSink（全量替换语义） ---

// refCapture 测试用 RefSink：记录每次 ReplaceRefs 的身份表。
type refCapture struct{ refs RefTable }

func (r *refCapture) ReplaceRefs(refs RefTable) { r.refs = refs }

func TestCollectFillsRefSink(t *testing.T) {
	axResp := map[string]any{"nodes": fakeAXNodes()}
	cl := mock.New(t, mockCDPHandler(t, axResp, evalFixtureSimple()))
	sink := &refCapture{}
	c := NewCollector(cl, sink)
	if _, err := c.Collect(context.Background(), Args{}); err != nil {
		t.Fatalf("Collect 失败: %v", err)
	}
	// 元素 uid 是 refs 的权威来源：注入采集第 0 个元素 → uid 1_0 → backendNodeId
	if info, ok := sink.refs["1_0"]; !ok || info.Tag != "button" || info.Text != "192.0.2.202" {
		t.Fatalf("refs[1_0] = %+v,%v, want button/192.0.2.202", info, ok)
	}
	// 全量替换语义：第二次快照的 refs 覆盖第一次（uid 前缀变为 2_）
	if _, err := c.Collect(context.Background(), Args{MaxNodes: 1}); err != nil {
		t.Fatalf("第二次 Collect 失败: %v", err)
	}
	// 元素 uid 每快照全新（eval_<快照号>_<序号>）：上轮元素 uid 不残留；
	// 树行 uid 按官方机制跨快照复用（同节点稳定），属预期行为。
	if info, ok := sink.refs["2_0"]; !ok || info.Tag != "button" {
		t.Fatalf("第二轮元素 uid 2_0 应存在，实际 %+v,%v", info, ok)
	}
	if _, ok := sink.refs["1_0"]; ok {
		t.Fatal("上轮元素 uid 1_0 不应残留（每快照全新，不跨快照复用）")
	}
}

// --- interesting 裁剪 + uid 分配 + 快照文本格式 ---
// （axtree 建树/裁剪/序列化的核心单测在 uimap/axtree 包；此处为集成面）

func TestSnapshotFormat(t *testing.T) {
	roots := axtree.BuildAXTree(fakeAXNodes())
	interesting := map[*axtree.AxParsed]bool{}
	axtree.CollectInteresting(interesting, roots[0], false)
	var serialized []*axtree.SnapNode
	for _, r := range roots {
		serialized = append(serialized, axtree.SerializeTree(r, interesting)...)
	}
	a := newUIDAllocator(NewCollector(nil, nil))
	for _, r := range serialized {
		assignTree(r, "L1", a)
	}
	var lines []string
	budget := 100
	for _, r := range serialized {
		renderSnap(r, 0, nil, "all", false, a, &lines, &budget)
	}
	tree := strings.Join(lines, "\n")
	want := []string{
		`uid=1_0 button "192.0.2.202"`,
		`uid=1_1 textbox "搜索实例名称"`,
		// 布尔映射：disabled=true → disableable + disabled（官方顺序）
		`uid=1_2 button "创建云机" disableable disabled # 语义：打开创建云机面板`,
		// role=none → ignored（官方规则）
		`uid=1_3 ignored "装饰节点"`,
		// checked 属性 "true" → 裸 checked
		`uid=1_4 checkbox "自动开机" checked`,
	}
	for _, w := range want {
		if !strings.Contains(tree, w) {
			t.Errorf("树输出缺少 %q\n实际:\n%s", w, tree)
		}
	}
	// 树行 uid 不注册 refs（uid 以 interactiveElements 为准），仅校验 uid 分配
	uids := map[string]bool{}
	for _, r := range serialized {
		uids[r.UID] = true
	}
	if len(uids) != 5 {
		t.Fatalf("uid 应两两唯一，实际 %d 个: %v", len(uids), uids)
	}
}

// TestUIDAllocationZeroBackend WebView2 坑位：backendNodeId=0 的节点照常分配
// uid（key 用 AxID 区分，不与其他零 id 节点串号）。
func TestUIDAllocationZeroBackend(t *testing.T) {
	payloads := []axtree.AxNode{
		{NodeID: "n1", BackendNodeID: 42, Role: axtree.AxValue{Value: "button"}, Name: axtree.AxValue{Value: "正常"}},
		{NodeID: "n2", BackendNodeID: 0, Role: axtree.AxValue{Value: "button"}, Name: axtree.AxValue{Value: "零ID"}},
		{NodeID: "n3", BackendNodeID: 0, Role: axtree.AxValue{Value: "button"}, Name: axtree.AxValue{Value: "零ID2"}},
	}
	roots := axtree.BuildAXTree(payloads)
	var serialized []*axtree.SnapNode
	for _, r := range roots {
		serialized = append(serialized, axtree.SerializeTree(r, nil)...) // verbose：全保留
	}
	a := newUIDAllocator(NewCollector(nil, nil))
	for _, r := range serialized {
		assignTree(r, "L1", a)
	}
	uids := map[string]int64{}
	for _, n := range serialized {
		uids[n.UID] = n.BackendID
	}
	if len(uids) != 3 {
		t.Fatalf("uid 应两两唯一: %v", uids)
	}
	// 零 id 节点的 uid 也唯一（key 用 AxID 区分），正常节点有真实 backendId
	zeroCount := 0
	for uid, id := range uids {
		if id == 0 {
			zeroCount++
			if uid == "" {
				t.Fatal("零 id 节点也应有 uid")
			}
		}
	}
	if zeroCount != 2 {
		t.Fatalf("应有 2 个零 id 节点, got %d", zeroCount)
	}
	// 零 id 节点 key 含 AxID（跨快照不复用）；正常节点 key 稳定可复用
	a2 := newUIDAllocator(NewCollector(nil, nil))
	for _, r := range roots {
		for _, n := range axtree.SerializeTree(r, nil) {
			assignTree(n, "L1", a2)
		}
	}
	if got := a2.reuse["L1_42"]; got == "" {
		t.Fatal("正常节点的 uid key 应稳定可复用")
	}
	zeroKeys := 0
	for k := range a2.reuse {
		if strings.Contains(k, "_0#") {
			zeroKeys++ // 零 id 节点 key 带 AxID；AX nodeId 跨快照变化即自然清理
		}
	}
	if zeroKeys != 2 {
		t.Fatalf("零 id 节点应有 2 个 AxID key, got %d", zeroKeys)
	}

}

// TestSnapAttributes 快照行属性规则（官方 SnapshotFormatter #getAttributes）。
func TestSnapAttributes(t *testing.T) {
	cases := []struct {
		name   string
		node   *axtree.SnapNode
		expect string
	}{
		{"布尔映射与三态", &axtree.SnapNode{UID: "3_1", Role: "button", Name: "确定", Fields: map[string]any{
			"disabled": true, "focused": true, "checked": "mixed",
		}}, `uid=3_1 button "确定" checked="mixed" disableable disabled focusable focused`},
		{"focused=false 只显示 focusable", &axtree.SnapNode{UID: "3_2", Role: "tab", Name: "主机", Fields: map[string]any{
			"focused": false,
		}}, `uid=3_2 tab "主机" focusable`},
		{"数值属性", &axtree.SnapNode{UID: "3_3", Role: "heading", Name: "标题", Fields: map[string]any{
			"level": float64(2),
		}}, `uid=3_3 heading "标题" level="2"`},
		{"StaticText 不显示 role", &axtree.SnapNode{UID: "3_4", Role: "StaticText", Name: "纯文本"},
			`uid=3_4 "纯文本"`},
		{"option 的 value=name 不重复且 selectable 抑制", &axtree.SnapNode{UID: "3_5", Role: "option", Name: "选项A", Fields: map[string]any{
			"value": "选项A", "selected": true,
		}}, `uid=3_5 option "选项A" selected`},
	}
	for _, c := range cases {
		if got := formatSnapLine(c.node, 0, ""); got != c.expect {
			t.Errorf("%s:\n got %q\nwant %q", c.name, got, c.expect)
		}
	}
}

// TestHasRedundantTextChildren 冗余 StaticText 子节点剪枝。
func TestHasRedundantTextChildren(t *testing.T) {
	mk := func() *axtree.SnapNode {
		return &axtree.SnapNode{UID: "1_0", Role: "group", Name: "登录 表单", Children: []*axtree.SnapNode{
			{UID: "1_1", Role: "StaticText", Name: "登录 "},
			{UID: "1_2", Role: "StaticText", Name: "表单"},
		}}
	}
	if !hasRedundantTextChildren(mk(), false) {
		t.Error("同名拼接的 StaticText 子节点应被剪枝")
	}
	if hasRedundantTextChildren(mk(), true) {
		t.Error("verbose 模式不剪枝")
	}
	n := mk()
	n.Children = append(n.Children, &axtree.SnapNode{UID: "1_3", Role: "button", Name: "提交"})
	if hasRedundantTextChildren(n, false) {
		t.Error("含非 StaticText 子节点时不剪枝")
	}
}

// --- renderSnap：budget / region ---

func TestRenderSnapBudget(t *testing.T) {
	roots := axtree.BuildAXTree(fakeAXNodes())
	interesting := map[*axtree.AxParsed]bool{}
	axtree.CollectInteresting(interesting, roots[0], false)
	var serialized []*axtree.SnapNode
	for _, r := range roots {
		serialized = append(serialized, axtree.SerializeTree(r, interesting)...)
	}
	a := newUIDAllocator(NewCollector(nil, nil))
	for _, r := range serialized {
		assignTree(r, "L1", a)
	}
	var lines []string
	budget := 2
	for _, r := range serialized {
		renderSnap(r, 0, nil, "all", false, a, &lines, &budget)
	}
	if len(lines) != 2 {
		t.Fatalf("budget=2 应只输出 2 行，实际 %d:\n%s", len(lines), strings.Join(lines, "\n"))
	}
	// 树行 uid 不再注册 refs（uid 以 interactiveElements 为准，树行仅阅读辅助）
	if len(a.refs) != 0 {
		t.Fatalf("树行不应注册 refs，实际 %d", len(a.refs))
	}
}

// --- ui_map 集成（mock CDP） ---

func TestUIMapWithMockCDP(t *testing.T) {
	axResp := map[string]any{"nodes": fakeAXNodes()}
	sink := &refCapture{}
	c := NewCollector(mock.New(t, mockCDPHandler(t, axResp, evalFixtureSimple())), sink)
	out, err := c.Collect(context.Background(), Args{})
	if err != nil {
		t.Fatalf("uiMap 失败: %v", err)
	}
	m, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("uiMap 返回类型异常: %T", out)
	}
	tree, _ := m["tree"].(string)
	if !strings.Contains(tree, `uid=1_1 button "192.0.2.202"`) {
		t.Fatalf("ui_map 输出应含官方格式行:\n%s", tree)
	}
	// 元素 uid（1_0）登记元素身份；树行 uid 仅作阅读辅助，不再注册 refs
	if info, ok := sink.refs["1_0"]; !ok || info.Text != "192.0.2.202" {
		t.Fatalf("元素 uid 表异常: %+v,%v", info, ok)
	}
	if _, ok := sink.refs["1_1"]; ok {
		t.Fatal("树行 uid 不应注册 refs（uid 以 interactiveElements 为准）")
	}
}

func TestUIMapTruncationNote(t *testing.T) {
	axResp := map[string]any{"nodes": fakeAXNodes()}
	sink := &refCapture{}
	c := NewCollector(mock.New(t, mockCDPHandler(t, axResp, evalFixtureSimple())), sink)
	out, err := c.Collect(context.Background(), Args{MaxNodes: 2})
	if err != nil {
		t.Fatalf("uiMap 失败: %v", err)
	}
	m := out.(map[string]any)
	tree, _ := m["tree"].(string)
	// 树尾注释行：快照共 5 节点（裁剪后），仅显示前 2 个
	if !strings.Contains(tree, "# 已截断：快照共 5 节点，仅显示前 2 个") {
		t.Fatalf("截断时应有树尾注释行:\n%s", tree)
	}
	if n, _ := m["nodeCount"].(int); n != 2 {
		t.Fatalf("nodeCount = %v, want 2", m["nodeCount"])
	}
}

// TestUIMapVerbose verbose=true 输出完整树（RootWebArea/generic 不裁剪）。
func TestUIMapVerbose(t *testing.T) {
	axResp := map[string]any{"nodes": fakeAXNodes()}
	sink := &refCapture{}
	c := NewCollector(mock.New(t, mockCDPHandler(t, axResp, evalFixtureSimple())), sink)
	out, err := c.Collect(context.Background(), Args{Verbose: true, MaxNodes: 50})
	if err != nil {
		t.Fatalf("uiMap 失败: %v", err)
	}
	tree, _ := out.(map[string]any)["tree"].(string)
	if !strings.Contains(tree, "RootWebArea") || !strings.Contains(tree, "generic") {
		t.Errorf("verbose 应包含 RootWebArea/generic 节点:\n%s", tree)
	}
}

// --- region 分区推断 ---

func TestInRegion(t *testing.T) {
	cases := []struct {
		cx, cy float64
		region string
		want   bool
	}{
		{100, 500, "sidebar", true},
		{339, 500, "sidebar", true},
		{340, 500, "sidebar", false},
		{800, 50, "toolbar", true},
		{800, 110, "toolbar", false},
		{800, 500, "main", true},
		{100, 500, "main", false},
		{800, 50, "main", false},
		{800, 500, "all", true},
	}
	for _, c := range cases {
		if got := inRegion(c.cx, c.cy, c.region); got != c.want {
			t.Errorf("inRegion(%v,%v,%q) = %v, want %v", c.cx, c.cy, c.region, got, c.want)
		}
	}
	if normalizeRegion("") != "all" || normalizeRegion("xxx") != "all" || normalizeRegion("dialog") != "dialog" {
		t.Fatal("normalizeRegion 异常")
	}
	if got := clampMaxNodes(0); got != maxNodesDefault {
		t.Fatalf("clampMaxNodes(0) = %d", got)
	}
	if got := clampMaxNodes(9999); got != maxNodesCap {
		t.Fatalf("clampMaxNodes(9999) = %d", got)
	}
}

// --- uid 分配器：官方 TextSnapshot.create 语义（原 uid_alloc_test.go） ---

// --- uid 分配器：官方 TextSnapshot.create 语义 ---

func TestUIDAllocator(t *testing.T) {
	a := newUIDAllocator(NewCollector(nil, nil))
	if a.snapshotID != 1 {
		t.Fatalf("首个快照号 = %d, want 1（官方 nextSnapshotId 从 1 起）", a.snapshotID)
	}
	// 同 key 复用同一 uid；不同 key 递增
	u1 := a.assign("lid_42", 42)
	u2 := a.assign("lid_17", 17)
	if u1 != "1_0" || u2 != "1_1" {
		t.Fatalf("uid 分配 = %q,%q, want 1_0,1_1", u1, u2)
	}
	if got := a.assign("lid_42", 42); got != u1 {
		t.Fatalf("同 key 应复用 uid: got %q, want %q", got, u1)
	}
	// cleanup：本轮未出现的 key 被删除（官方 TextSnapshot.ts:141-146）
	a.seen["lid_42"] = false
	delete(a.seen, "lid_42")
	a.cleanup()
	if _, ok := a.reuse["lid_42"]; ok {
		t.Fatal("cleanup 应删除本轮未出现的 key")
	}
	if _, ok := a.reuse["lid_17"]; !ok {
		t.Fatal("cleanup 不应删除本轮出现的 key")
	}
}

// TestUIDAllocatorCrossSnapshot 跨快照复用：同一 DOM 节点（同 loaderId_backendId）
// 在第二次快照中沿用首次分配的 uid（官方 uniqueBackendNodeIdToMcpId）。
func TestUIDAllocatorCrossSnapshot(t *testing.T) {
	a1c := NewCollector(nil, nil)
	a1 := newUIDAllocator(a1c)
	uid1 := a1.assign("lid_42", 42)
	a2 := newUIDAllocator(a1c) // 同一 Collector 的第二次快照
	if a2.snapshotID != 2 {
		t.Fatalf("第二个快照号 = %d, want 2", a2.snapshotID)
	}
	if uid2 := a2.assign("lid_42", 42); uid2 != uid1 {
		t.Fatalf("跨快照应复用 uid: got %q, want %q", uid2, uid1)
	}
	// 新节点拿新快照号前缀的 uid
	if uid := a2.assign("lid_99", 99); uid != "2_0" {
		t.Fatalf("新节点 uid = %q, want 2_0", uid)
	}
}
