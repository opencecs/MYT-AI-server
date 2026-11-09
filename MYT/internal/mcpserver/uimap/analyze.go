package uimap

// analyze.go — ui_map「观察即分析」层：消费注入采集结果（capture_eval.go），
// 产出分析好的局面，让 AI 拿到一次 ui_map 输出即可决策：
//   - state：viewport（视口尺寸）/ modal（检测到的全屏遮罩或弹窗，含弹窗内
//     可见按钮 uid 与建议操作）/ activeTab（data-tab 容器可见的那个）
//   - interactiveElements：全部可见可交互元素（tag/text/placeholder/value/
//     disabled/rect/uid），uid 与 refs 表打通，click(uid) 直接命中。

import (
	"fmt"
	"strconv"
	"strings"
)

// interactiveElements 输出上限（超出截断并注明）。
const maxInteractiveElements = 300

// analyze Collect 的分析入口：返回 summary / state / interactiveElements。
// items 已完成 uid 分配与 refs 注册（见 itemsFromEval）；region 仅作用于
// interactiveElements 输出过滤（refs 已全量注册，uid 不受 region 影响）。
func (c *Collector) analyze(ev *evalResult, items []interactiveItem, region string) (map[string]any, map[string]any, []map[string]any) {
	state := buildLayoutState(ev, items)
	return buildSummary(state, items), state, renderInteractive(items, region)
}

// interactiveItem 可交互元素 + 已分配 uid（与树行同一分配器）。
type interactiveItem struct {
	e   element
	uid string
}

// itemsFromEval 为注入元素分配 uid 并构建身份表（refs）。
// uid key 每快照全新（eval_<快照号>_<序号>）→ uid 不跨快照复用，
// 与「uid 仅本次快照有效」的约定一致。
func itemsFromEval(ev *evalResult, a *uidAllocator) ([]interactiveItem, RefTable) {
	items := make([]interactiveItem, 0, len(ev.Elements))
	refs := make(RefTable, len(ev.Elements))
	for i := range ev.Elements {
		uid := a.assign(fmt.Sprintf("eval_%d_%d", a.snapshotID, i), 0)
		e := ev.Elements[i]
		items = append(items, interactiveItem{e: e, uid: uid})
		refs[uid] = RefInfo{Tag: e.Tag, Text: e.Text, AriaLabel: e.AriaLabel, X: e.X, Y: e.Y, W: e.W, H: e.H}
	}
	return items, refs
}

// renderInteractive 输出 interactiveElements 段（region 过滤 + 上限截断）。
func renderInteractive(items []interactiveItem, region string) []map[string]any {
	out := make([]map[string]any, 0, len(items))
	for _, it := range items {
		e := it.e
		if region != "all" && !inRegion(e.X+e.W/2, e.Y+e.H/2, region) {
			continue
		}
		if len(out) >= maxInteractiveElements {
			break
		}
		m := map[string]any{
			"tag": e.Tag,
			"uid": it.uid,
			"rect": map[string]float64{
				"x": e.X, "y": e.Y, "w": e.W, "h": e.H,
			},
		}
		if e.Text != "" {
			m["text"] = textClip(e.Text, 60)
		}
		if e.AriaLabel != "" {
			m["ariaLabel"] = textClip(e.AriaLabel, 60)
		}
		if e.Placeholder != "" {
			m["placeholder"] = textClip(e.Placeholder, 60)
		}
		if e.Value != "" {
			m["value"] = textClip(e.Value, 60)
		}
		if e.Disabled {
			m["disabled"] = true
		}
		if e.Expanded != "" {
			m["expanded"] = e.Expanded
		}
		out = append(out, m)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// buildLayoutState 组装 state 段：视口 / 弹窗 / 当前页签。
func buildLayoutState(ev *evalResult, items []interactiveItem) map[string]any {
	state := map[string]any{"interactiveCount": len(items)}
	if ev.Viewport.W > 0 {
		state["viewport"] = map[string]float64{"width": ev.Viewport.W, "height": ev.Viewport.H}
	}
	if ev.ActiveTab != "" {
		state["activeTab"] = ev.ActiveTab
	}
	if m := detectModal(ev.Overlays, ev.Viewport.W, ev.Viewport.H, items); m != nil {
		state["modal"] = m
	}
	return state
}

// modalKeywords 弹窗内建议优先点击的按钮文案（自更新弹窗「稍后提醒」优先）。
var modalKeywords = []string{"稍后提醒", "稍后", "以后再说", "跳过", "取消", "关闭", "忽略", "不再提醒"}

// detectModal 检测全屏遮罩/弹窗（候选由注入 JS 预筛：fixed/absolute +
// 面积≥视口50% + z≥50 或 modal/overlay/dialog 标记），命中时列出弹窗内
// 可见按钮（含 uid）并给建议操作。
func detectModal(candidates []overlay, vw, vh float64, items []interactiveItem) map[string]any {
	ov := pickOverlay(candidates, vw, vh)
	if ov == nil {
		return nil
	}
	buttons := make([]map[string]any, 0, 8)
	for _, it := range items {
		e := it.e
		cx, cy := e.X+e.W/2, e.Y+e.H/2
		if cx < ov.X || cx > ov.X+ov.W || cy < ov.Y || cy > ov.Y+ov.H {
			continue
		}
		b := map[string]any{"uid": it.uid, "tag": e.Tag}
		if name := textClip(e.Text, 40); name != "" {
			b["text"] = name
		}
		if isTitlebarControl(e, vw) {
			// 窗口标题栏控件：列出但标注，不作为 suggested 候选
			//（误选「关闭」会退出整个客户端，真机实证）
			b["titlebar"] = true
		}
		buttons = append(buttons, b)
	}
	m := map[string]any{
		"detected": true,
		"rect":     map[string]float64{"x": ov.X, "y": ov.Y, "w": ov.W, "h": ov.H},
		"buttons":  buttons,
	}
	if ov.Cls != "" {
		m["class"] = ov.Cls
	}
	if ov.Role != "" {
		m["role"] = ov.Role
	}
	if ov.Z != "" {
		m["zIndex"] = ov.Z
	}
	hint := "检测到模态弹窗挡住交互，请先关闭弹窗"
	if s := suggestModalButton(buttons); s != nil {
		m["suggested"] = s
		hint = fmt.Sprintf("检测到模态弹窗挡住交互，建议先处理：%s(uid=%s)", s["text"], s["uid"])
	}
	m["hint"] = hint
	return m
}

// pickOverlay 选最上层的遮罩候选：面积占视口 ≥ 50%（视口未知时信任注入侧
// 预筛），按数值 z-index 取最大。
func pickOverlay(candidates []overlay, vw, vh float64) *overlay {
	var best *overlay
	bestZ := -1.0
	for i := range candidates {
		ov := &candidates[i]
		if vw > 0 && (ov.W < 0.5*vw || ov.H < 0.5*vh) {
			continue
		}
		z := overlayZ(ov)
		if z >= bestZ {
			bestZ = z
			best = ov
		}
	}
	return best
}

// overlayZ 遮罩候选的数值 z-index（解析失败按 0）。
func overlayZ(ov *overlay) float64 {
	z, err := strconv.ParseFloat(ov.Z, 64)
	if err != nil {
		return 0
	}
	return z
}

// suggestModalButton 按关键词优先级从弹窗按钮中挑建议操作
// （titlebar 窗口控件不参与——误选「关闭」会退出整个客户端）。
func suggestModalButton(buttons []map[string]any) map[string]any {
	for _, kw := range modalKeywords {
		for _, b := range buttons {
			if tb, _ := b["titlebar"].(bool); tb {
				continue
			}
			if t, _ := b["text"].(string); strings.Contains(t, kw) {
				return b
			}
		}
	}
	return nil
}

// isTitlebarControl 窗口自绘标题栏控件（App.tsx 自绘 h-8 标题栏）：
// 文案为 关闭/最小化/全屏/最大化，或位于视口顶部 40px 内且超出
// 视口宽-200（右上角控件区）。
func isTitlebarControl(e element, vw float64) bool {
	name := e.Text
	if e.AriaLabel != "" {
		name = e.AriaLabel
	}
	switch name {
	case "关闭", "最小化", "全屏", "最大化":
		return true
	}
	return vw > 0 && e.Y <= 40 && e.X > vw-200
}
