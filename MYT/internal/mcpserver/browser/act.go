package browser

// act.go — 动作工具的执行路径：单会话 Runtime.evaluate 注入「定位 + 动作」JS。
//
// 与旧路径（actOnRef → DOM.resolveNode(backendNodeId)）的区别：彻底不依赖
// CDP 节点 id（nodeId 会话作用域、backendNodeId 在 WebView2 上解析不可靠，
// 且曾出现「报错但动了别的元素」的错位副作用）。定位按元素身份
//（tag+text+ariaLabel+rect±8px，与 ui_map 注入采集同源候选）在页面内完成：
// 找不到或无法唯一定位 → 不动手，直接报「ref 已过期或无法唯一定位」。
// 动作全部在页面 JS 内完成（click 事件序列 / React valueTracker 填充 /
// 追加输入 / KeyboardEvent 按键），并回传元素状态供验证。

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/moyunteng/myt/internal/mcpserver/cdp"
	"github.com/moyunteng/myt/internal/mcpserver/uimap"
)

// locatePrelude 身份匹配段（配合 uimap.CollectPrelude 的 candidates）：
// 唯一命中直接返回；多命中按中心距离取最近者，且要求显著区分
// （best 至少比 second 近 8px），否则视为无法唯一定位。
const locatePrelude = `
  const findEl = I => {
    const m = candidates.filter(el => {
      if (el.tagName.toLowerCase() !== I.tag) return false
      if (textOf(el) !== I.text) return false
      if ((el.getAttribute('aria-label') || '') !== I.ariaLabel) return false
      const r = el.getBoundingClientRect()
      return Math.abs(r.x - I.x) <= 8 && Math.abs(r.y - I.y) <= 8
    })
    if (m.length === 0) return { err: 'not-found' }
    if (m.length === 1) return { el: m[0] }
    const cx = I.x + I.w / 2, cy = I.y + I.h / 2
    const dist = el => { const r = el.getBoundingClientRect(); return Math.hypot(r.x + r.width / 2 - cx, r.y + r.height / 2 - cy) }
    m.sort((a, b) => dist(a) - dist(b))
    if (dist(m[0]) + 8 < dist(m[1])) return { el: m[0] }
    return { err: 'ambiguous' }
  }
`

// actExpr 构造「定位 + 动作」注入表达式（identity/params 以 JSON 内嵌）。
func actExpr(identity uimap.RefInfo, action string, params map[string]any) string {
	idJSON, _ := json.Marshal(identity)
	if params == nil {
		params = map[string]any{}
	}
	params["action"] = action
	pJSON, _ := json.Marshal(params)
	return `(() => {` + uimap.CollectPrelude + locatePrelude + `
  const I = ` + string(idJSON) + `;
  const P = ` + string(pJSON) + `;
  const f = findEl(I);
  if (f.err) return JSON.stringify({ ok: false, reason: f.err });
  const el = f.el;
  const out = { ok: true, tag: el.tagName.toLowerCase() };
  // value setter：优先标准原型（React 受控组件需原生 setter 绕过 valueTracker），
  // 取不到再沿元素自身原型链兜底；仍取不到 → 结构化错误，不盲目调用。
  const setterFor = el => {
    const proto = el.tagName === 'TEXTAREA' ? HTMLTextAreaElement.prototype : HTMLInputElement.prototype;
    const d = Object.getOwnPropertyDescriptor(proto, 'value');
    if (d && typeof d.set === 'function') return d.set;
    const own = Object.getOwnPropertyDescriptor(Object.getPrototypeOf(el), 'value');
    if (own && typeof own.set === 'function') return own.set;
    return null;
  };
  switch (P.action) {
    case 'click': {
      el.scrollIntoView({ block: 'center' });
      const o = { bubbles: true, cancelable: true, view: window, button: 0 };
      el.dispatchEvent(new PointerEvent('pointerdown', o));
      el.dispatchEvent(new MouseEvent('mousedown', o));
      el.dispatchEvent(new PointerEvent('pointerup', o));
      el.dispatchEvent(new MouseEvent('mouseup', o));
      el.click();
      if (P.dbl) { el.dispatchEvent(new MouseEvent('dblclick', o)); }
      break;
    }
    case 'fill': {
      const role = el.getAttribute && el.getAttribute('role');
      const isToggle = (el.tagName === 'INPUT' && (el.type === 'checkbox' || el.type === 'radio')) ||
        role === 'checkbox' || role === 'radio' || role === 'switch';
      if (isToggle) {
        if (P.value !== 'true' && P.value !== 'false') {
          return JSON.stringify({ ok: false, reason: 'toggle-value',
            err: 'Checkboxes, radio boxes and toggles require "true" or "false" value, but ' + P.value + ' was used' });
        }
        if ((P.value === 'true') !== !!el.checked) { el.click(); }
        break;
      }
      if (el.tagName === 'SELECT') {
        const opt = [...el.options].find(o => o.text === P.value || o.value === P.value);
        if (!opt) return JSON.stringify({ ok: false, reason: 'no-option',
          err: 'Could not find option with text "' + P.value + '"' });
        const setter = Object.getOwnPropertyDescriptor(HTMLSelectElement.prototype, 'value').set;
        setter.call(el, opt.value);
        el.dispatchEvent(new Event('input', { bubbles: true }));
        el.dispatchEvent(new Event('change', { bubbles: true }));
        out.value = opt.value;
        break;
      }
      el.focus();
      const setter = setterFor(el);
      if (!setter) return JSON.stringify({ ok: false, reason: 'not-fillable', err: '该元素不支持编程式赋值' });
      // React 受控组件：原生 value setter 绕过 valueTracker，再派发 input/change
      setter.call(el, P.value);
      el.dispatchEvent(new Event('input', { bubbles: true }));
      el.dispatchEvent(new Event('change', { bubbles: true }));
      out.value = el.value;
      break;
    }
    case 'type': {
      el.focus();
      const setter = setterFor(el);
      if (!setter) return JSON.stringify({ ok: false, reason: 'not-fillable', err: '该元素不支持编程式赋值' });
      setter.call(el, (el.value || '') + P.value);
      el.dispatchEvent(new Event('input', { bubbles: true }));
      el.dispatchEvent(new Event('change', { bubbles: true }));
      out.value = el.value;
      break;
    }
    case 'press': {
      if (el.focus) el.focus();
      const base = { key: P.key, code: P.code, keyCode: P.keyCode, which: P.keyCode,
        bubbles: true, cancelable: true, view: window };
      el.dispatchEvent(new KeyboardEvent('keydown', base));
      el.dispatchEvent(new KeyboardEvent('keypress', base));
      el.dispatchEvent(new KeyboardEvent('keyup', base));
      break;
    }
    default:
      return JSON.stringify({ ok: false, reason: 'unknown-action' });
  }
  return JSON.stringify(out);
})()`
}

// rectExpr 定位表达式：返回元素 scrollIntoView 后的实时 rect（hover/drag
// 的可信鼠标事件坐标源）。
func rectExpr(identity uimap.RefInfo) string {
	idJSON, _ := json.Marshal(identity)
	return `(() => {` + uimap.CollectPrelude + locatePrelude + `
  const f = findEl(` + string(idJSON) + `);
  if (f.err) return JSON.stringify({ ok: false, reason: f.err });
  f.el.scrollIntoView({ block: 'center' });
  const r = f.el.getBoundingClientRect();
  return JSON.stringify({ ok: true, rect: { x: r.x, y: r.y, w: r.width, h: r.height } });
})()`
}

// objectExpr 定位表达式：返回元素对象本身（returnByValue:false 取 objectId，
// upload_file 的 DOM.requestNode / evaluate_script 的元素参数用）。
func objectExpr(identity uimap.RefInfo) string {
	idJSON, _ := json.Marshal(identity)
	return `(() => {` + uimap.CollectPrelude + locatePrelude + `
  const f = findEl(` + string(idJSON) + `);
  if (f.err) return null;
  return f.el;
})()`
}

// actResult 动作注入结果。
type actResult struct {
	OK     bool   `json:"ok"`
	Reason string `json:"reason"`
	Err    string `json:"err"`
	Tag    string `json:"tag"`
	Value  string `json:"value"`
}

// evalOnSessionDial 独立会话上执行注入表达式，返回其 JSON 字符串结果。
func (h *Hub) evalOnSessionDial(ctx context.Context, expr string) (string, error) {
	wsURL, err := h.CurrentWS(ctx)
	if err != nil {
		return "", err
	}
	sess, err := h.client.Dial(wsURL)
	if err != nil {
		return "", fmt.Errorf("建立动作会话失败: %w", err)
	}
	defer sess.Close()
	res, err := sess.Call(ctx, "Runtime.evaluate", map[string]any{
		"expression": expr, "returnByValue": true,
	}, 15*time.Second)
	if err != nil {
		return "", err
	}
	var out struct {
		Result struct {
			Value string `json:"value"`
		} `json:"result"`
		ExceptionDetails *struct {
			Text      string `json:"text"`
			Exception *struct {
				Description string `json:"description"`
			} `json:"exception"`
		} `json:"exceptionDetails"`
	}
	if err := cdp.JSONUnmarshal(res, &out); err != nil {
		return "", err
	}
	// JS 异常：result.value 缺失，必须带出真实异常描述（否则下游只会看到
	// 「unexpected end of JSON input」这类空 JSON 报错，真机无法定位）。
	if out.ExceptionDetails != nil {
		desc := out.ExceptionDetails.Text
		if out.ExceptionDetails.Exception != nil && out.ExceptionDetails.Exception.Description != "" {
			desc = out.ExceptionDetails.Exception.Description
		}
		return "", fmt.Errorf("动作 JS 执行异常: %s（表达式片段: %.160s）", desc, expr)
	}
	// returnByValue 下 undefined 序列化为 value 缺失 → 空串，同样结构化报错。
	if out.Result.Value == "" {
		return "", fmt.Errorf("动作 JS 未返回结果（undefined/空），表达式片段: %.160s", expr)
	}
	return out.Result.Value, nil
}

// actOnIdentity 按身份定位并执行动作（click/fill/type/press）。
// 找不到或无法唯一定位 → ErrRefPageChanged（不动手，消除错位副作用）。
func (h *Hub) actOnIdentity(ctx context.Context, uid, action string, params map[string]any) (actResult, error) {
	info, ok := h.lookupRef(uid)
	if !ok {
		return actResult{}, fmt.Errorf("uid %q: %w", uid, cdp.ErrRefNotInSnapshot)
	}
	s, err := h.evalOnSessionDial(ctx, actExpr(info, action, params))
	if err != nil {
		return actResult{}, err
	}
	var r actResult
	if err := json.Unmarshal([]byte(s), &r); err != nil {
		return actResult{}, fmt.Errorf("解析动作结果失败: %w", err)
	}
	if !r.OK {
		switch r.Reason {
		case "not-found", "ambiguous":
			return r, fmt.Errorf("uid %q: %w", uid, cdp.ErrRefPageChanged)
		default:
			return r, fmt.Errorf("%s 失败: %s", action, r.Err)
		}
	}
	return r, nil
}

// locateRectCenter 按身份定位元素，返回 scrollIntoView 后的中心坐标
// （hover/drag 的可信鼠标事件用）。
func (h *Hub) locateRectCenter(ctx context.Context, uid string) (int, int, error) {
	info, ok := h.lookupRef(uid)
	if !ok {
		return 0, 0, fmt.Errorf("uid %q: %w", uid, cdp.ErrRefNotInSnapshot)
	}
	s, err := h.evalOnSessionDial(ctx, rectExpr(info))
	if err != nil {
		return 0, 0, err
	}
	var r struct {
		OK     bool   `json:"ok"`
		Reason string `json:"reason"`
		Rect   struct {
			X, Y, W, H float64
		} `json:"rect"`
	}
	if err := json.Unmarshal([]byte(s), &r); err != nil {
		return 0, 0, fmt.Errorf("解析定位结果失败: %w", err)
	}
	if !r.OK {
		return 0, 0, fmt.Errorf("uid %q: %w", uid, cdp.ErrRefPageChanged)
	}
	return int(r.Rect.X + r.Rect.W/2), int(r.Rect.Y + r.Rect.H/2), nil
}

// locateObjectID 在给定会话上按身份定位元素，返回其 RemoteObject objectId
// （upload_file / evaluate_script 的元素参数用）。
func (h *Hub) locateObjectID(ctx context.Context, sess *cdp.Session, uid string) (string, error) {
	info, ok := h.lookupRef(uid)
	if !ok {
		return "", fmt.Errorf("uid %q: %w", uid, cdp.ErrRefNotInSnapshot)
	}
	res, err := sess.Call(ctx, "Runtime.evaluate", map[string]any{
		"expression": objectExpr(info), "returnByValue": false,
	}, 10*time.Second)
	if err != nil {
		return "", err
	}
	objID, err := remoteObjectID(res)
	if err != nil {
		return "", fmt.Errorf("uid %q: %w", uid, cdp.ErrRefPageChanged)
	}
	return objID, nil
}
