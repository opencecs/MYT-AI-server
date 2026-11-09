package reg_test

// reg_test.go — structuredContent 形态防回归：经 in-memory transport 全链路
// 调用真实注册的工具，断言 structuredContent 恒为 JSON 对象（MCP/SEP-2106
// 要求 record，裸数组会被外部客户端以「expected record, received array」拒绝）。

import (
	"context"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/moyunteng/myt/internal/mcpserver/reg"
)

// listSliceTool 返回裸数组的工具（违规形态的回归样本）。
func listSliceTool(ctx context.Context, _ struct{}) (any, error) {
	return []map[string]any{{"ip": "192.168.1.100"}, {"ip": "192.168.1.101"}}, nil
}

// objTool 返回对象的工具（合规形态，应原样透传不被加壳）。
func objTool(ctx context.Context, _ struct{}) (any, error) {
	return map[string]any{"result": "ok"}, nil
}

// scalarTool 返回标量的工具（同样需要加壳）。
func scalarTool(ctx context.Context, _ struct{}) (any, error) {
	return 42, nil
}

// setupServer 注册三个探针工具并接好 client/server 会话。
func setupServer(t *testing.T) *mcp.ClientSession {
	t.Helper()
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	reg.Reg(srv, nil, "list_probe", "list", listSliceTool, reg.ReadOnly())
	reg.Reg(srv, nil, "obj_probe", "obj", objTool, reg.ReadOnly())
	reg.Reg(srv, nil, "scalar_probe", "scalar", scalarTool, reg.ReadOnly())

	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	// 顺序敏感：先接 server 端，client Connect 的 initialize 握手才有响应方。
	if _, err := srv.Connect(ctxBg(), serverTransport, nil); err != nil {
		t.Fatalf("server connect: %v", err)
	}
	cm := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0"}, nil)
	cs, err := cm.Connect(ctxBg(), clientTransport, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

func ctxBg() context.Context { return context.Background() }

// TestStructuredContentIsObject 全链路断言：数组/标量被包成 {"items":...}，
// 对象透传。
func TestStructuredContentIsObject(t *testing.T) {
	cs := setupServer(t)

	// 裸数组 → {"items":[...]}，字段内容不变只是加壳
	res, err := cs.CallTool(ctxBg(), &mcp.CallToolParams{Name: "list_probe"})
	if err != nil {
		t.Fatalf("list_probe: %v", err)
	}
	obj, ok := res.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("structuredContent 应为对象，实际 %T: %+v", res.StructuredContent, res.StructuredContent)
	}
	items, ok := obj["items"].([]any)
	if !ok || len(items) != 2 {
		t.Fatalf("items 应为长度 2 的数组: %+v", obj)
	}
	first, _ := items[0].(map[string]any)
	if first["ip"] != "192.168.1.100" {
		t.Fatalf("items[0].ip = %v, 内容应原样保留", first["ip"])
	}
	// content 文本回退也是加壳后的 JSON（两条路径一致）
	if len(res.Content) == 0 {
		t.Fatal("content 回退缺失")
	}

	// 对象 → 原样透传
	res, err = cs.CallTool(ctxBg(), &mcp.CallToolParams{Name: "obj_probe"})
	if err != nil {
		t.Fatalf("obj_probe: %v", err)
	}
	obj, ok = res.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("对象返回不应被加壳，实际 %T", res.StructuredContent)
	}
	if obj["result"] != "ok" {
		t.Fatalf("对象内容应原样: %+v", obj)
	}
	if _, has := obj["items"]; has {
		t.Fatalf("对象返回不应出现 items 键: %+v", obj)
	}

	// 标量 → {"items": 42}
	res, err = cs.CallTool(ctxBg(), &mcp.CallToolParams{Name: "scalar_probe"})
	if err != nil {
		t.Fatalf("scalar_probe: %v", err)
	}
	obj, ok = res.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("标量返回也应加壳为对象，实际 %T", res.StructuredContent)
	}
	if obj["items"] != float64(42) {
		t.Fatalf("items = %v, want 42", obj["items"])
	}
}
