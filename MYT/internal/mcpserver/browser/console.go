package browser

// console.go — debugging 类工具（复刻 chrome-devtools-mcp src/tools/console.ts
// 与 src/tools/script.ts）：list_console_messages / get_console_message /
// evaluate_script。
// 采集层：事件会话订阅 Runtime.consoleAPICalled / Runtime.exceptionThrown /
// Log.entryAdded，环形缓冲 500 条。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/moyunteng/myt/internal/mcpserver/cdp"
	"github.com/moyunteng/myt/internal/mcpserver/reg"
)

// conBufferCap console 环形缓冲上限。
const conBufferCap = 500

// conMessage 单条 console 消息（官方 consoleMessage 等价字段）。
type conMessage struct {
	MsgID     int64    `json:"msgid"`
	Type      string   `json:"type"` // log/debug/info/error/warn/... （官方 ConsoleMessageType 枚举）
	Text      string   `json:"text"`
	Stack     []string `json:"stack,omitempty"`
	TimeStamp float64  `json:"timestamp"`
}

// conBuffer console 环形缓冲。
type conBuffer struct {
	mu    sync.Mutex
	seq   int64
	items []*conMessage
}

func newConBuffer() *conBuffer { return &conBuffer{} }

// add 追加消息（环形裁剪）。
func (b *conBuffer) add(m *conMessage) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.seq++
	m.MsgID = b.seq
	b.items = append(b.items, m)
	if len(b.items) > conBufferCap {
		b.items = b.items[1:]
	}
}

// list 快照（按 msgid 升序）。
func (b *conBuffer) list() []*conMessage {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]*conMessage, len(b.items))
	copy(out, b.items)
	return out
}

// get 按 msgid 查找。
func (b *conBuffer) get(id int64) *conMessage {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, m := range b.items {
		if m.MsgID == id {
			return m
		}
	}
	return nil
}

// onEvent 处理 console 相关事件（hub 事件泵调用）。
func (b *conBuffer) onEvent(method string, params json.RawMessage) {
	switch method {
	case "Runtime.consoleAPICalled":
		var p struct {
			Type       string  `json:"type"`
			Timestamp  float64 `json:"timestamp"`
			StackTrace *struct {
				CallFrames []struct {
					FunctionName string `json:"functionName"`
					URL          string `json:"url"`
					LineNumber   int    `json:"lineNumber"`
				} `json:"callFrames"`
			} `json:"stackTrace"`
			Args []struct {
				Type    string `json:"type"`
				Value   any    `json:"value"`
				Preview *struct {
					Description string `json:"description"`
				} `json:"preview"`
			} `json:"args"`
		}
		if cdp.JSONUnmarshal(params, &p) != nil {
			return
		}
		parts := make([]string, 0, len(p.Args))
		for _, a := range p.Args {
			switch {
			case a.Preview != nil && a.Preview.Description != "":
				parts = append(parts, a.Preview.Description)
			case a.Value != nil:
				switch v := a.Value.(type) {
				case string:
					parts = append(parts, v)
				default:
					b, err := json.Marshal(a.Value)
					if err != nil {
						parts = append(parts, a.Type)
						continue
					}
					sval := string(b)
					if len(sval) > 200 {
						sval = sval[:200] + "…"
					}
					parts = append(parts, sval)
				}
			default:
				parts = append(parts, a.Type)
			}
		}
		m := &conMessage{Type: p.Type, Text: strings.Join(parts, " "), TimeStamp: p.Timestamp}
		if p.StackTrace != nil {
			for _, f := range p.StackTrace.CallFrames {
				m.Stack = append(m.Stack, fmt.Sprintf("%s@%s:%d", f.FunctionName, f.URL, f.LineNumber))
			}
		}
		b.add(m)
	case "Runtime.exceptionThrown":
		var p struct {
			Timestamp        float64 `json:"timestamp"`
			ExceptionDetails struct {
				Text      string `json:"text"`
				Exception *struct {
					Description string `json:"description"`
				} `json:"exception"`
			} `json:"exceptionDetails"`
		}
		if cdp.JSONUnmarshal(params, &p) != nil {
			return
		}
		text := p.ExceptionDetails.Text
		if p.ExceptionDetails.Exception != nil && p.ExceptionDetails.Exception.Description != "" {
			text = p.ExceptionDetails.Exception.Description
		}
		b.add(&conMessage{Type: "error", Text: text, TimeStamp: p.Timestamp})
	case "Log.entryAdded":
		var p struct {
			Entry struct {
				Level     string  `json:"level"`
				Text      string  `json:"text"`
				Timestamp float64 `json:"timestamp"`
				Source    string  `json:"source"`
			} `json:"entry"`
		}
		if cdp.JSONUnmarshal(params, &p) != nil {
			return
		}
		b.add(&conMessage{Type: p.Entry.Level, Text: fmt.Sprintf("[%s] %s", p.Entry.Source, p.Entry.Text), TimeStamp: p.Entry.Timestamp})
	}
}

// filterableMessageTypes 官方可过滤消息类型（与官方枚举一致；issue 需 Audits
// 域，本实现不产出该类型）。
var filterableMessageTypes = map[string]bool{
	"log": true, "debug": true, "info": true, "error": true, "warn": true,
	"dir": true, "dirxml": true, "table": true, "trace": true, "clear": true,
	"startGroup": true, "startGroupCollapsed": true, "endGroup": true,
	"assert": true, "profile": true, "profileEnd": true, "count": true,
	"timeEnd": true, "verbose": true,
}

type listConsoleMessagesArgs struct {
	PageSize           int      `json:"pageSize,omitempty" jsonschema_desc:"每页最多返回条数；缺省返回全部"`
	PageIdx            int      `json:"pageIdx,omitempty" jsonschema_desc:"页号（0 起）；缺省第一页"`
	Types              []string `json:"types,omitempty" jsonschema_desc:"按类型过滤（log/debug/info/error/warn 等），空返回全部"`
	IncludeStackTraces bool     `json:"includeStackTraces,omitempty" jsonschema_desc:"true 时附带堆栈（若采集到），默认 false"`
}

type getConsoleMessageArgs struct {
	MsgID int64 `json:"msgid" jsonschema_desc:"消息序号（来自 list_console_messages）"`
}

type evaluateScriptArgs struct {
	Function     string   `json:"function,omitempty" jsonschema_desc:"JS 源码（function 形态：() => document.title；支持 async）。与 sourcePath 二选一"`
	SourcePath   string   `json:"sourcePath,omitempty" jsonschema_desc:"JS 文件路径（客户端本机）。与 function 二选一"`
	Format       string   `json:"format,omitempty" jsonschema_desc:"function（默认，支持 args）/script（经典脚本，不支持 args）"`
	Args         []string `json:"args,omitempty" jsonschema_desc:"可选参数：元素 uid 列表（函数入参为对应 DOM 元素）"`
	FilePath     string   `json:"filePath,omitempty" jsonschema_desc:"可选：输出保存到该文件（缺省内联返回）"`
	DialogAction string   `json:"dialogAction,omitempty" jsonschema_desc:"执行期间的 dialog 自动处理：accept（默认）/dismiss，或其他字符串作为 window.prompt 应答文本"`
}

// registerConsoleTools 注册 console 类工具 + evaluate_script。
func registerConsoleTools(srv *mcp.Server, sink reg.Sink, h *Hub) {
	reg.Reg(srv, sink, "list_console_messages",
		"列出页面的 console 消息（这是复刻 chrome-devtools-mcp 的 list_console_messages 工具，行为与官方一致："+
			"事件会话采集 Runtime/Log 域，环形缓冲最近 500 条；支持分页与类型过滤）。无破坏性。",
		func(ctx context.Context, a listConsoleMessagesArgs) (any, error) {
			if err := h.cdpGate(ctx); err != nil {
				return nil, err
			}
			if _, err := h.Session(ctx); err != nil {
				return nil, err
			}
			all := h.console.list()
			want := map[string]bool{}
			for _, t := range a.Types {
				want[t] = true
			}
			filtered := make([]*conMessage, 0, len(all))
			for _, m := range all {
				if len(want) > 0 && !want[m.Type] {
					continue
				}
				if !a.IncludeStackTraces {
					cp := *m
					cp.Stack = nil
					filtered = append(filtered, &cp)
					continue
				}
				filtered = append(filtered, m)
			}
			start, end := 0, len(filtered)
			if a.PageSize > 0 {
				start = a.PageIdx * a.PageSize
				if start > end {
					start = end
				}
				end = min(start+a.PageSize, end)
			}
			return map[string]any{"messages": filtered[start:end], "total": len(filtered)}, nil
		}, reg.ReadOnly())

	reg.Reg(srv, sink, "get_console_message",
		"按 msgid 获取单条 console 消息（这是复刻 chrome-devtools-mcp 的 get_console_message 工具，行为与官方一致）。无破坏性。",
		func(ctx context.Context, a getConsoleMessageArgs) (any, error) {
			if err := h.cdpGate(ctx); err != nil {
				return nil, err
			}
			if _, err := h.Session(ctx); err != nil {
				return nil, err
			}
			m := h.console.get(a.MsgID)
			if m == nil {
				return nil, fmt.Errorf("msgid %d 不存在（先调 list_console_messages；缓冲上限 %d 条）", a.MsgID, conBufferCap)
			}
			return m, nil
		}, reg.ReadOnly())

	// 高危：任意 JS 执行。description 显式标注 + destructiveHint（reg.Destructive）。
	reg.Reg(srv, sink, "evaluate_script",
		"【高危】在页面内执行任意 JavaScript 并返回 JSON 结果（这是复刻 chrome-devtools-mcp 的 "+
			"evaluate_script 工具，行为与官方一致：function/script 两种形态，args 传元素 uid，"+
			"结果须可 JSON 序列化）。⚠️ 该工具可读写页面任意数据、发起请求、触发业务操作，"+
			"仅在明确需要且已评估副作用时使用；优先考虑 ui_map/click/fill 等窄接口工具。",
		func(ctx context.Context, a evaluateScriptArgs) (any, error) {
			if err := h.cdpGate(ctx); err != nil {
				return nil, err
			}
			return h.evaluateScript(ctx, a)
		}, reg.Destructive())
}

// evaluateScript 官方 performEvaluation 等价：
//
//	function 形态：evaluateHandle(\n fn \n) → callFunctionOn(window, wrapper, [fn, ...args])
//	script 形态：evaluate(source) → callFunctionOn(window, v => JSON.stringify(v), [value])
func (h *Hub) evaluateScript(ctx context.Context, a evaluateScriptArgs) (any, error) {
	if (a.Function == "") == (a.SourcePath == "") {
		return nil, errors.New("Specify exactly one of function or sourcePath.")
	}
	format := a.Format
	if format == "" {
		format = "function"
	}
	if format != "function" && format != "script" {
		return nil, fmt.Errorf("不支持的 format %q（function/script）", format)
	}
	if format == "script" && len(a.Args) > 0 {
		return nil, errors.New("args cannot be used when format is \"script\".")
	}
	source := a.Function
	if a.SourcePath != "" {
		data, err := os.ReadFile(a.SourcePath)
		if err != nil {
			return nil, fmt.Errorf("Unable to read script source from %s: %w", a.SourcePath, err)
		}
		source = string(data)
	}

	sess, err := h.Session(ctx)
	if err != nil {
		return nil, err
	}
	// 执行期间 dialog 自动应答（官方 dialogAction，默认 accept）
	dialogAction := a.DialogAction
	if dialogAction == "" {
		dialogAction = "accept"
	}
	h.mu.Lock()
	h.autoDialog = dialogAction
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		h.autoDialog = ""
		h.mu.Unlock()
	}()

	// window 对象（callFunctionOn 的接收者，等价官方在 page 上下文 evaluate）
	winRes, err := sess.Call(ctx, "Runtime.evaluate", map[string]any{"expression": "window", "returnByValue": false}, 5*time.Second)
	if err != nil {
		return nil, err
	}
	windowID, err := remoteObjectID(winRes)
	if err != nil {
		return nil, err
	}

	var callArgs []map[string]any
	var wrapper string
	if format == "function" {
		fnRes, err := sess.Call(ctx, "Runtime.evaluate", map[string]any{
			"expression":    "(\n" + strings.TrimRight(source, ";") + "\n)",
			"returnByValue": false,
		}, 5*time.Second)
		if err != nil {
			return nil, err
		}
		fnID, err := remoteObjectID(fnRes)
		if err != nil {
			return nil, fmt.Errorf("function 源码解析失败（须为函数表达式）: %w", err)
		}
		callArgs = append(callArgs, map[string]any{"objectId": fnID})
		for _, uid := range a.Args {
			objID, err := h.locateObjectID(ctx, sess, uid)
			if err != nil {
				return nil, err
			}
			callArgs = append(callArgs, map[string]any{"objectId": objID})
		}
		wrapper = `async function(f, ...rest) { return JSON.stringify(await f(...rest)); }`
	} else {
		valRes, err := sess.Call(ctx, "Runtime.evaluate", map[string]any{"expression": source, "returnByValue": false}, 10*time.Second)
		if err != nil {
			return nil, err
		}
		valID, err := remoteObjectID(valRes)
		if err != nil {
			return nil, fmt.Errorf("脚本执行失败: %w", err)
		}
		callArgs = []map[string]any{{"objectId": valID}}
		wrapper = `function(v) { return JSON.stringify(v); }`
	}

	res, err := sess.Call(ctx, "Runtime.callFunctionOn", map[string]any{
		"objectId":            windowID,
		"functionDeclaration": wrapper,
		"arguments":           callArgs,
		"returnByValue":       true,
		"awaitPromise":        true,
	}, 30*time.Second)
	if err != nil {
		return nil, err
	}
	var out struct {
		Result struct {
			Value any `json:"value"`
		} `json:"result"`
		ExceptionDetails *struct {
			Text string `json:"text"`
		} `json:"exceptionDetails"`
	}
	if err := cdp.JSONUnmarshal(res, &out); err != nil {
		return nil, err
	}
	if out.ExceptionDetails != nil {
		return nil, fmt.Errorf("JS 执行异常: %s", out.ExceptionDetails.Text)
	}
	resultStr, _ := out.Result.Value.(string)
	if a.FilePath != "" {
		if err := os.WriteFile(a.FilePath, []byte(resultStr), 0o644); err != nil {
			return nil, fmt.Errorf("写入输出文件失败: %w", err)
		}
		return map[string]any{"result": fmt.Sprintf("Script ran on page. Output saved to %s.", a.FilePath)}, nil
	}
	return map[string]any{"result": "Script ran on page and returned:", "value": resultStr}, nil
}
