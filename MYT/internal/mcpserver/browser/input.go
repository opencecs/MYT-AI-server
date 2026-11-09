package browser

// input.go — input 类工具（复刻 chrome-devtools-mcp src/tools/input.ts）：
// click / click_at / hover / drag / type_text / press_key / scroll。
// uid 路径走「可信鼠标事件」（官方 locator.click 落地为
// Input.dispatchMouseEvent），坐标路径为逃生舱。

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/moyunteng/myt/internal/mcpserver/reg"
)

type clickArgs struct {
	UID             string `json:"uid,omitempty" jsonschema_desc:"目标元素 uid（来自 ui_map 快照行，优先使用）"`
	X               int    `json:"x,omitempty" jsonschema_desc:"逃生舱：目标坐标 X（仅无 uid 时使用）"`
	Y               int    `json:"y,omitempty" jsonschema_desc:"逃生舱：目标坐标 Y（仅无 uid 时使用）"`
	DblClick        bool   `json:"dblClick,omitempty" jsonschema_desc:"true 为双击，默认 false"`
	IncludeSnapshot bool   `json:"includeSnapshot,omitempty" jsonschema_desc:"true 时在响应中附带最新快照，默认 false"`
}

type clickAtArgs struct {
	X               int  `json:"x" jsonschema_desc:"点击坐标 X"`
	Y               int  `json:"y" jsonschema_desc:"点击坐标 Y"`
	DblClick        bool `json:"dblClick,omitempty" jsonschema_desc:"true 为双击，默认 false"`
	IncludeSnapshot bool `json:"includeSnapshot,omitempty" jsonschema_desc:"true 时在响应中附带最新快照"`
}

type hoverArgs struct {
	UID             string `json:"uid" jsonschema_desc:"目标元素 uid（来自 ui_map 快照行）"`
	IncludeSnapshot bool   `json:"includeSnapshot,omitempty" jsonschema_desc:"true 时在响应中附带最新快照"`
}

type dragArgs struct {
	FromUID         string `json:"from_uid" jsonschema_desc:"拖动源元素 uid"`
	ToUID           string `json:"to_uid" jsonschema_desc:"放置目标元素 uid"`
	IncludeSnapshot bool   `json:"includeSnapshot,omitempty" jsonschema_desc:"true 时在响应中附带最新快照"`
}

type typeTextArgs struct {
	Text string `json:"text" jsonschema_desc:"要输入的文本内容"`
	UID  string `json:"uid,omitempty" jsonschema_desc:"目标输入框 uid（按身份定位后追加文本）"`
	Ref  string `json:"ref,omitempty" jsonschema_desc:"兼容别名：同 uid（旧参数名）"`
}

// targetUID uid 优先、ref 兼容别名（参数名统一）。
func (a typeTextArgs) targetUID() string {
	if a.UID != "" {
		return a.UID
	}
	return a.Ref
}

type pressKeyArgs struct {
	Key string `json:"key" jsonschema_desc:"键名：Enter/Tab/Escape/Backspace/Delete/ArrowUp/ArrowDown/ArrowLeft/ArrowRight/Home/End/PageUp/PageDown"`
	UID string `json:"uid,omitempty" jsonschema_desc:"可选：先按 uid 定位并聚焦元素再按键（缺省发给当前焦点元素）"`
	Ref string `json:"ref,omitempty" jsonschema_desc:"兼容别名：同 uid（旧参数名）"`
}

// targetUID uid 优先、ref 兼容别名（参数名统一）。
func (a pressKeyArgs) targetUID() string {
	if a.UID != "" {
		return a.UID
	}
	return a.Ref
}

type scrollArgs struct {
	X      int `json:"x" jsonschema_desc:"滚动位置 X 坐标"`
	Y      int `json:"y" jsonschema_desc:"滚动位置 Y 坐标"`
	DeltaY int `json:"deltaY" jsonschema_desc:"纵向滚动量（正=向下，负=向上，建议 ±300）"`
	DeltaX int `json:"deltaX,omitempty" jsonschema_desc:"横向滚动量（可选）"`
}

// registerPointerTools 注册 click/click_at/hover/drag/type_text/press_key/scroll。
func registerPointerTools(srv *mcp.Server, sink reg.Sink, h *Hub) {
	reg.Reg(srv, sink, "click",
		"点击元素（复刻 chrome-devtools-mcp 的 click）：uid 按元素身份在页面内唯一定位后"+
			"派发点击事件序列（scrollIntoView → pointer/mouse down-up → click）；"+
			"找不到或无法唯一定位则不动手并报 ref 已过期。x/y 为坐标逃生舱（可信鼠标事件）；"+
			"dblClick=true 双击。注意：点击「删除/重置」等按钮会触发真实业务操作。",
		func(ctx context.Context, a clickArgs) (any, error) {
			if err := h.cdpGate(ctx); err != nil {
				return nil, err
			}
			out := map[string]any{}
			var err error
			if a.UID != "" {
				err = h.clickUID(ctx, a.UID, a.DblClick)
			} else if a.X != 0 || a.Y != 0 {
				err = h.trustedClickAt(ctx, a.X, a.Y, a.DblClick)
			} else {
				return nil, errors.New("需要 uid（优先）或 x/y 坐标")
			}
			if err != nil {
				return nil, err
			}
			out["result"] = "ok"
			if a.IncludeSnapshot {
				h.includeSnapshot(ctx, out)
			}
			return out, nil
		})

	reg.Reg(srv, sink, "click_at",
		"在指定坐标处点击（复刻 chrome-devtools-mcp 的 click_at，行为与官方一致："+
			"CDP 可信鼠标事件）；dblClick=true 双击。",
		func(ctx context.Context, a clickAtArgs) (any, error) {
			if err := h.cdpGate(ctx); err != nil {
				return nil, err
			}
			if err := h.trustedClickAt(ctx, a.X, a.Y, a.DblClick); err != nil {
				return nil, err
			}
			out := map[string]any{"result": "ok", "x": a.X, "y": a.Y}
			if a.IncludeSnapshot {
				h.includeSnapshot(ctx, out)
			}
			return out, nil
		})

	reg.Reg(srv, sink, "hover",
		"悬停在指定元素上（复刻 chrome-devtools-mcp 的 hover，行为与官方一致："+
			"元素中心派发 mouseMoved 可信事件），用于触发悬停菜单/提示。",
		func(ctx context.Context, a hoverArgs) (any, error) {
			if err := h.cdpGate(ctx); err != nil {
				return nil, err
			}
			if err := h.hoverUID(ctx, a.UID); err != nil {
				return nil, err
			}
			out := map[string]any{"result": "ok"}
			if a.IncludeSnapshot {
				h.includeSnapshot(ctx, out)
			}
			return out, nil
		})

	reg.Reg(srv, sink, "drag",
		"把一个元素拖拽到另一个元素上（复刻 chrome-devtools-mcp 的 drag，行为与官方一致："+
			"源中心按下 → 平滑移动 → 目标中心释放）。",
		func(ctx context.Context, a dragArgs) (any, error) {
			if err := h.cdpGate(ctx); err != nil {
				return nil, err
			}
			if err := h.dragUID(ctx, a.FromUID, a.ToUID); err != nil {
				return nil, err
			}
			out := map[string]any{"result": "ok"}
			if a.IncludeSnapshot {
				h.includeSnapshot(ctx, out)
			}
			return out, nil
		})

}

// trustedMouseEvent 派发单个可信鼠标事件。
func (h *Hub) trustedMouseEvent(ctx context.Context, ev map[string]any) error {
	wsURL, err := h.CurrentWS(ctx)
	if err != nil {
		return err
	}
	if _, err := h.client.Call(ctx, wsURL, "Input.dispatchMouseEvent", ev, 5*time.Second); err != nil {
		return fmt.Errorf("派发鼠标事件失败: %w", err)
	}
	return nil
}

// trustedClickAt 在坐标处点击（官方 page.mouse.click 等价：move → down → up）。
func (h *Hub) trustedClickAt(ctx context.Context, x, y int, dblClick bool) error {
	count := 1
	if dblClick {
		count = 2
	}
	if err := h.trustedMouseEvent(ctx, map[string]any{"type": "mouseMoved", "x": x, "y": y, "button": "none", "clickCount": 0}); err != nil {
		return err
	}
	for i := 1; i <= count; i++ {
		if err := h.trustedMouseEvent(ctx, map[string]any{"type": "mousePressed", "x": x, "y": y, "button": "left", "clickCount": i, "buttons": 1}); err != nil {
			return err
		}
		if err := h.trustedMouseEvent(ctx, map[string]any{"type": "mouseReleased", "x": x, "y": y, "button": "left", "clickCount": i, "buttons": 0}); err != nil {
			return err
		}
	}
	return nil
}

// clickUID 按身份定位元素并在页面内派发点击事件序列（scrollIntoView →
// pointer/mouse down-up → click；dblClick 追加 dblclick）。
// 找不到或无法唯一定位 → ErrRefPageChanged，不动手（消除错位副作用）。
func (h *Hub) clickUID(ctx context.Context, uid string, dblClick bool) error {
	_, err := h.actOnIdentity(ctx, uid, "click", map[string]any{"dbl": dblClick})
	return err
}

// hoverUID 悬停：按身份定位 → 元素中心派发可信 mouseMoved
// （CSS :hover 只响应可信事件，故坐标经 Input.dispatchMouseEvent）。
func (h *Hub) hoverUID(ctx context.Context, uid string) error {
	x, y, err := h.locateRectCenter(ctx, uid)
	if err != nil {
		return err
	}
	return h.trustedMouseEvent(ctx, map[string]any{"type": "mouseMoved", "x": x, "y": y, "button": "none", "clickCount": 0})
}

// dragUID 拖拽：源中心按下 → 分步移动 → 目标中心释放（官方 elementHandle.drag +
// drop 的可信事件等价实现；官方在 drag 与 drop 间等待 50ms）。
func (h *Hub) dragUID(ctx context.Context, fromUID, toUID string) error {
	fx, fy, err := h.locateRectCenter(ctx, fromUID)
	if err != nil {
		return fmt.Errorf("from_uid %q: %w", fromUID, err)
	}
	tx, ty, err := h.locateRectCenter(ctx, toUID)
	if err != nil {
		return fmt.Errorf("to_uid %q: %w", toUID, err)
	}
	if err := h.trustedMouseEvent(ctx, map[string]any{"type": "mouseMoved", "x": fx, "y": fy, "button": "none", "clickCount": 0}); err != nil {
		return err
	}
	if err := h.trustedMouseEvent(ctx, map[string]any{"type": "mousePressed", "x": fx, "y": fy, "button": "left", "clickCount": 1, "buttons": 1}); err != nil {
		return err
	}
	const steps = 8
	for i := 1; i <= steps; i++ {
		x := fx + (tx-fx)*i/steps
		y := fy + (ty-fy)*i/steps
		if err := h.trustedMouseEvent(ctx, map[string]any{"type": "mouseMoved", "x": x, "y": y, "button": "left", "clickCount": 0, "buttons": 1}); err != nil {
			return err
		}
	}
	time.Sleep(50 * time.Millisecond) // 官方：drag 与 drop 间等待 50ms
	return h.trustedMouseEvent(ctx, map[string]any{"type": "mouseReleased", "x": tx, "y": ty, "button": "left", "clickCount": 1, "buttons": 0})
}
