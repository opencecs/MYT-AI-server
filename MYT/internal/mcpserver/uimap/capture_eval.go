package uimap

// capture_eval.go — ui_map 观测主路径：Runtime.evaluate 注入采集
//（cdp-shot.js dumpPage 同源路径，真机实战验证可用）。
//
// 数据流：注入 JS 一次性返回可见可交互元素/遮罩/视口/当前页签；uid 注册为
// RefTable（uid→元素身份 tag/text/ariaLabel/rect）。动作工具（click/fill 等）
// 按同一份候选查询逻辑在页面内按身份重新定位后执行——全程不依赖 CDP 节点 id
//（nodeId 会话作用域、backendNodeId 解析在 WebView2 上均不可靠）。
//
// CollectPrelude 为注入采集与动作定位共用的页面 JS 前置段（browser/act.go
// 也引用），保证「ui_map 看到的元素」与「动作时重新找到的元素」同源。
//
// 可见性判定与 AGENTS.md 的 CDP 取证纪律一致：getComputedStyle
//（display/visibility/opacity）+ getBoundingClientRect 宽高>5 + 视口内，
// 禁用 offsetParent（fixed 恒 null）。

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// CollectPrelude 注入采集与动作定位共用的页面 JS 前置段：
// vis/inViewport/clean/textOf/candidates（候选 = 选择器命中 + 可见 + 视口内）。
const CollectPrelude = `
  const vis = el => {
    const s = getComputedStyle(el)
    if (s.display === 'none' || s.visibility === 'hidden' || parseFloat(s.opacity) <= 0) return false
    const r = el.getBoundingClientRect()
    return r.width > 5 && r.height > 5
  }
  const inViewport = r => r.bottom > 0 && r.right > 0 && r.top < innerHeight && r.left < innerWidth
  const clean = t => (t || '').replace(/\s+/g, ' ').trim()
  const MCP_SEL = 'button, a, [role="button"], input, select, textarea, [onclick], [role="menuitem"], [role="tab"]'
  const textOf = el => clean(el.innerText || el.textContent || el.value || el.placeholder ||
    el.getAttribute('aria-label') || el.title || '').slice(0, 80)
  const candidates = [...document.querySelectorAll(MCP_SEL)].filter(vis).filter(el => inViewport(el.getBoundingClientRect()))
`

// collectJS 注入采集脚本（自包含，返回 JSON 字符串）：
//   - elements：可见可交互元素（文档序）；
//   - overlays：遮罩/弹窗候选（fixed/absolute + 面积≥视口50% + z≥50 或
//     类名/role 含 modal/overlay/dialog）；
//   - viewport / activeTab：视口尺寸与可见的 data-tab 容器。
const collectJS = `(() => {` + CollectPrelude + `
  const elements = candidates.map(el => {
    const r = el.getBoundingClientRect()
    return {
      tag: el.tagName.toLowerCase(),
      text: textOf(el),
      ariaLabel: el.getAttribute('aria-label') || '',
      placeholder: el.getAttribute('placeholder') || '',
      value: ['input', 'textarea', 'select'].includes(el.tagName.toLowerCase()) ? String(el.value || '').slice(0, 80) : '',
      disabled: !!el.disabled || el.getAttribute('aria-disabled') === 'true',
      expanded: el.getAttribute('aria-expanded') || '',
      x: Math.round(r.x), y: Math.round(r.y), w: Math.round(r.width), h: Math.round(r.height)
    }
  })
  const vw = { w: innerWidth, h: innerHeight }
  const overlays = [...document.querySelectorAll('body *')].filter(el => {
    const s = getComputedStyle(el)
    if (s.position !== 'fixed' && s.position !== 'absolute') return false
    const r = el.getBoundingClientRect()
    if (r.width < vw.w * 0.5 || r.height < vw.h * 0.5) return false
    const z = parseInt(s.zIndex, 10) || 0
    const labeled = /modal|overlay|dialog/i.test((el.getAttribute('class') || '') + ' ' + (el.getAttribute('role') || ''))
    return z >= 50 || labeled
  }).map(el => {
    const r = el.getBoundingClientRect()
    return { x: Math.round(r.x), y: Math.round(r.y), w: Math.round(r.width), h: Math.round(r.height),
      z: getComputedStyle(el).zIndex, cls: (el.getAttribute('class') || '').slice(0, 80), role: el.getAttribute('role') || '' }
  })
  let activeTab = ''
  for (const el of document.querySelectorAll('[data-tab]')) {
    if (getComputedStyle(el).display !== 'none') { activeTab = el.getAttribute('data-tab'); break }
  }
  return JSON.stringify({ viewport: vw, activeTab: activeTab, elements: elements, overlays: overlays })
})()`

// element 注入采集返回的单个可交互元素（字段与 interactiveElements 输出对应）。
type element struct {
	Tag         string `json:"tag"`
	Text        string `json:"text"`
	AriaLabel   string `json:"ariaLabel"`
	Placeholder string `json:"placeholder"`
	Value       string `json:"value"`
	Disabled    bool   `json:"disabled"`
	Expanded    string `json:"expanded"` // aria-expanded 原值（"" = 无此属性）
	X, Y, W, H  float64
}

// overlay 注入采集返回的遮罩/弹窗候选。
type overlay struct {
	X, Y, W, H float64
	Z          string `json:"z"`
	Cls        string `json:"cls"`
	Role       string `json:"role"`
}

// evalResult 注入采集的一次完整结果。
type evalResult struct {
	Viewport struct {
		W float64 `json:"w"`
		H float64 `json:"h"`
	} `json:"viewport"`
	ActiveTab string    `json:"activeTab"`
	Elements  []element `json:"elements"`
	Overlays  []overlay `json:"overlays"`
}

// RefInfo uid 对应元素的身份信息：动作工具按身份在页面内重新定位，
// 不依赖 CDP 节点 id。Rect 允许 ±8px 误差（容少量滚动）。
// json 键必须为小写 x/y/w/h——与 collectJS 产出键及 act.go locatePrelude
// 读取键（I.x/I.y/I.w/I.h）一致，大写键会导致 findEl 恒 not-found（有防回归单测）。
type RefInfo struct {
	Tag       string  `json:"tag"`
	Text      string  `json:"text"`
	AriaLabel string  `json:"ariaLabel"`
	X         float64 `json:"x"`
	Y         float64 `json:"y"`
	W         float64 `json:"w"`
	H         float64 `json:"h"`
}

// RefTable uid → 元素身份表（每次 ui_map 全量重建，经 RefSink 下发）。
type RefTable map[string]RefInfo

// evalCollect 执行注入采集（Runtime.evaluate，returnByValue）。
func (c *Collector) evalCollect(ctx context.Context) (*evalResult, error) {
	v, err := c.client.Eval(ctx, collectJS, 15*time.Second)
	if err != nil {
		return nil, err
	}
	s, ok := v.(string)
	if !ok {
		return nil, fmt.Errorf("注入采集返回类型异常: %T", v)
	}
	var out evalResult
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return nil, fmt.Errorf("解析注入采集结果失败: %w", err)
	}
	return &out, nil
}
