package axtree

// axtree_test.go — 建树/裁剪/序列化核心链路的最小自包含单测
//（uimap/snapshot_test.go 侧有基于完整 fixture 的集成覆盖）。

import "testing"

func miniAXNodes() []AxNode {
	return []AxNode{
		{NodeID: "root", Role: AxValue{Value: "RootWebArea"}, ChildIDs: []string{"b1", "t1", "c1", "decor"}},
		{NodeID: "b1", BackendNodeID: 42, Role: AxValue{Value: "button"}, Name: AxValue{Value: "创建云机"},
			Properties: []AxProperty{{Name: "disabled", Value: AxValue{Value: true}}}},
		{NodeID: "t1", BackendNodeID: 17, Role: AxValue{Value: "textbox"}, Name: AxValue{Value: "搜索"}},
		{NodeID: "c1", BackendNodeID: 0, Role: AxValue{Value: "checkbox"}, Name: AxValue{Value: "自动开机"},
			Properties: []AxProperty{{Name: "checked", Value: AxValue{Value: "true"}}}},
		{NodeID: "decor", Role: AxValue{Value: "generic"}},
	}
}

func TestBuildAXTreeRoots(t *testing.T) {
	roots := BuildAXTree(miniAXNodes())
	if len(roots) != 1 || roots[0].role != "RootWebArea" {
		t.Fatalf("roots 异常: %+v", roots)
	}
	if len(roots[0].children) != 4 {
		t.Fatalf("RootWebArea 子节点数 = %d, want 4", len(roots[0].children))
	}
}

func TestCollectAndSerializePrune(t *testing.T) {
	roots := BuildAXTree(miniAXNodes())
	interesting := map[*AxParsed]bool{}
	CollectInteresting(interesting, roots[0], false)
	var serialized []*SnapNode
	for _, r := range roots {
		serialized = append(serialized, SerializeTree(r, interesting)...)
	}
	// RootWebArea/generic 均不 interesting → 裁掉后子节点上移，剩 3 个根
	if len(serialized) != 3 {
		t.Fatalf("裁剪后根数 = %d, want 3", len(serialized))
	}
	wantRoles := []string{"button", "textbox", "checkbox"}
	for i, r := range serialized {
		if r.Role != wantRoles[i] {
			t.Errorf("serialized[%d].Role = %q, want %q", i, r.Role, wantRoles[i])
		}
	}
	// serialize 字段模型：disabled 布尔 / checked 三态字符串
	if serialized[0].Fields["disabled"] != true {
		t.Errorf("disabled = %v, want true", serialized[0].Fields["disabled"])
	}
	if serialized[2].Fields["checked"] != true {
		t.Errorf("checked = %v, want true（三态 'true' → bool）", serialized[2].Fields["checked"])
	}
	// verbose（interesting=nil）保留全部节点（含 RootWebArea/generic）
	var verbose []*SnapNode
	for _, r := range roots {
		verbose = append(verbose, SerializeTree(r, nil)...)
	}
	if len(verbose) != 1 || verbose[0].Role != "RootWebArea" {
		t.Fatalf("verbose 应保留 RootWebArea 根: %+v", verbose)
	}
}
