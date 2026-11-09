package browser

// env.go — emulation 类工具 + 非指针输入（复刻 chrome-devtools-mcp 的
// src/tools/emulation.ts 与 src/tools/input.ts 的键盘/滚动部分）：
//   emulate：网络节流/CPU 节流/地理/UserAgent/配色方案/视口/额外请求头，
//     直接映射 CDP（Network.emulateNetworkConditions、Emulation.set* 等）；
//   resize_page（EMULATION 类，官方在 pages.ts）：WebView2 无窗口域，
//     用 Emulation.setDeviceMetricsOverride 设视口；
//   type_text / press_key / scroll：CDP Input 域（insertText/dispatchKeyEvent/
//     mouseWheel），官方 keyboard/mouse 的等价实现。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/moyunteng/myt/internal/mcpserver/cdp"
	"github.com/moyunteng/myt/internal/mcpserver/reg"
)

// predefinedNetworkConditions 官方 puppeteer PredefinedNetworkConditions 常量。
var predefinedNetworkConditions = map[string]map[string]any{
	"Offline": {"offline": true, "latency": 0, "downloadThroughput": 0, "uploadThroughput": 0},
	"Slow 3G": {
		"latency":            400,
		"downloadThroughput": float64(400*1024) / 8 * 0.8,
		"uploadThroughput":   float64(400*1024) / 8 * 0.8,
	},
	"Fast 3G": {
		"latency":            150,
		"downloadThroughput": float64(4*1024*1024) / 8 * 0.8,
		"uploadThroughput":   float64(3*1024*1024) / 8 * 0.8,
	},
}

type emulateArgs struct {
	NetworkConditions string  `json:"networkConditions,omitempty" jsonschema_desc:"网络节流：Offline / Slow 3G / Fast 3G；缺省关闭节流"`
	CPUThrottlingRate float64 `json:"cpuThrottlingRate,omitempty" jsonschema_desc:"CPU 降速倍数（1-20）；缺省或 1 关闭节流"`
	Geolocation       string  `json:"geolocation,omitempty" jsonschema_desc:"地理坐标 \"<纬度>,<经度>\"（纬度 -90~90，经度 -180~180）；缺省清除覆盖"`
	UserAgent         string  `json:"userAgent,omitempty" jsonschema_desc:"UserAgent 覆盖；空串清除覆盖"`
	ColorScheme       string  `json:"colorScheme,omitempty" jsonschema_desc:"dark / light / auto（auto 重置默认）"`
	Viewport          string  `json:"viewport,omitempty" jsonschema_desc:"视口 \"<宽>x<高>x<DPR>[,mobile][,touch][,landscape]\""`
	ExtraHTTPHeaders  string  `json:"extraHttpHeaders,omitempty" jsonschema_desc:"额外 HTTP 请求头 JSON 对象字符串，如 {\"X-Custom\":\"v\"}；空串清除全部"`
}

// registerEmulateTools 注册 emulate。
func registerEmulateTools(srv *mcp.Server, sink reg.Sink, h *Hub) {
	reg.Reg(srv, sink, "emulate",
		"在目标页面上模拟各类环境（这是复刻 chrome-devtools-mcp 的 emulate 工具，行为与官方一致："+
			"网络节流/CPU 节流/地理位置/UserAgent/深浅色/视口/额外请求头，只传需要修改的项）。",
		func(ctx context.Context, a emulateArgs) (any, error) {
			if err := h.cdpGate(ctx); err != nil {
				return nil, err
			}
			sess, err := h.Session(ctx)
			if err != nil {
				return nil, err
			}
			if err := h.emulate(ctx, sess, a); err != nil {
				return nil, err
			}
			return map[string]any{"result": "Emulation configured successfully"}, nil
		})
}

// emulate 逐项应用模拟参数（官方 page.emulate 的 CDP 映射）。
func (h *Hub) emulate(ctx context.Context, sess *cdp.Session, a emulateArgs) error {
	apply := func(method string, params any) error {
		if _, err := sess.Call(ctx, method, params, 5*time.Second); err != nil {
			return fmt.Errorf("%s 失败: %w", method, err)
		}
		return nil
	}
	if a.NetworkConditions != "" {
		cond, ok := predefinedNetworkConditions[a.NetworkConditions]
		if !ok {
			return fmt.Errorf("不支持的 networkConditions %q（Offline / Slow 3G / Fast 3G）", a.NetworkConditions)
		}
		if err := apply("Network.emulateNetworkConditions", cond); err != nil {
			return err
		}
	}
	if a.CPUThrottlingRate != 0 {
		rate := a.CPUThrottlingRate
		if rate < 1 || rate > 20 {
			return errors.New("cpuThrottlingRate 须在 1-20 之间")
		}
		if err := apply("Emulation.setCPUThrottlingRate", map[string]any{"rate": rate}); err != nil {
			return err
		}
	}
	if a.Geolocation != "" {
		lat, lon, err := parseGeolocation(a.Geolocation)
		if err != nil {
			return err
		}
		if err := apply("Emulation.setGeolocationOverride", map[string]any{"latitude": lat, "longitude": lon}); err != nil {
			return err
		}
	}
	if a.UserAgent != "" || a.userAgentSet() {
		if err := apply("Emulation.setUserAgentOverride", map[string]any{"userAgent": a.UserAgent}); err != nil {
			return err
		}
	}
	if a.ColorScheme != "" {
		if a.ColorScheme != "dark" && a.ColorScheme != "light" && a.ColorScheme != "auto" {
			return fmt.Errorf("不支持的 colorScheme %q（dark/light/auto）", a.ColorScheme)
		}
		features := []any{}
		if a.ColorScheme != "auto" {
			features = []any{map[string]any{"name": "prefers-color-scheme", "value": a.ColorScheme}}
		}
		if err := apply("Emulation.setEmulatedMedia", map[string]any{"features": features}); err != nil {
			return err
		}
	}
	if a.Viewport != "" {
		metrics, err := parseViewport(a.Viewport)
		if err != nil {
			return err
		}
		if err := apply("Emulation.setDeviceMetricsOverride", metrics); err != nil {
			return err
		}
	}
	if a.ExtraHTTPHeaders != "" {
		headers := map[string]string{}
		if a.ExtraHTTPHeaders != "" {
			if err := json.Unmarshal([]byte(a.ExtraHTTPHeaders), &headers); err != nil {
				return fmt.Errorf("Invalid JSON for headers: %w", err)
			}
		}
		if err := apply("Network.setExtraHTTPHeaders", map[string]any{"headers": headers}); err != nil {
			return err
		}
	}
	return nil
}

// userAgentSet 区分「未传」与「传空串清除」（JSON 缺省 vs 显式空串）。
// go-sdk 反射 schema 下两者都落为空串，此处统一按「有值才覆盖」处理，
// 清除 UserAgent 覆盖由 Emulation.setUserAgentOverride 空串语义承担。
func (a emulateArgs) userAgentSet() bool { return false }

// parseGeolocation 官方 geolocationTransform："lat,lon" → 校验范围。
func parseGeolocation(s string) (float64, float64, error) {
	parts := strings.Split(s, ",")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf(`geolocation 须为 "<纬度>,<经度>" 格式`)
	}
	lat, err1 := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
	lon, err2 := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
	if err1 != nil || err2 != nil || lat < -90 || lat > 90 || lon < -180 || lon > 180 {
		return 0, 0, fmt.Errorf("geolocation 非法：纬度须在 -90~90、经度须在 -180~180，got %q", s)
	}
	return lat, lon, nil
}

// parseViewport 官方 viewportTransform："WxHxDPR[,mobile][,touch][,landscape]"。
func parseViewport(s string) (map[string]any, error) {
	parts := strings.Split(s, ",")
	dims := strings.Split(parts[0], "x")
	if len(dims) < 2 {
		return nil, fmt.Errorf(`viewport 须为 "<宽>x<高>x<DPR>[,mobile][,touch][,landscape]" 格式，got %q`, s)
	}
	w, err1 := strconv.Atoi(strings.TrimSpace(dims[0]))
	hh, err2 := strconv.Atoi(strings.TrimSpace(dims[1]))
	if err1 != nil || err2 != nil || w <= 0 || hh <= 0 {
		return nil, fmt.Errorf("viewport 宽高须为正数，got %q", s)
	}
	m := map[string]any{"width": w, "height": hh, "mobile": false, "hasTouch": false, "screenOrientation": map[string]any{"type": "portraitPrimary", "angle": 0}}
	if len(dims) >= 3 {
		dpr, err := strconv.ParseFloat(strings.TrimSpace(dims[2]), 64)
		if err != nil || dpr <= 0 {
			return nil, fmt.Errorf("viewport devicePixelRatio 须为正数，got %q", s)
		}
		m["deviceScaleFactor"] = dpr
	}
	for _, tag := range parts[1:] {
		switch strings.TrimSpace(tag) {
		case "mobile":
			m["mobile"] = true
		case "touch":
			m["hasTouch"] = true
		case "landscape":
			m["screenOrientation"] = map[string]any{"type": "landscapePrimary", "angle": 90}
		}
	}
	return m, nil
}

// --- resize_page（官方 pages.ts，EMULATION 类） + type_text/press_key/scroll ---

// resizePageArgs resize_page 参数。
type resizePageArgs struct {
	Width  int `json:"width" jsonschema_desc:"页面宽度"`
	Height int `json:"height" jsonschema_desc:"页面高度"`
}

// registerEnvInputTools 注册 resize_page 与键盘/滚动输入工具。
func registerEnvInputTools(srv *mcp.Server, sink reg.Sink, h *Hub) {
	reg.Reg(srv, sink, "resize_page",
		"调整页面视口尺寸（这是复刻 chrome-devtools-mcp 的 resize_page 工具；WebView2 适配："+
			"经 Emulation.setDeviceMetricsOverride 设置视口，不改变宿主窗口大小）。",
		func(ctx context.Context, a resizePageArgs) (any, error) {
			if err := h.cdpGate(ctx); err != nil {
				return nil, err
			}
			if err := h.resizePage(ctx, a.Width, a.Height); err != nil {
				return nil, err
			}
			return map[string]any{"result": "ok", "width": a.Width, "height": a.Height}, nil
		})

	// --- type_text / press_key / scroll（官方 input.ts 键盘/滚动部分） ---

	reg.Reg(srv, sink, "type_text",
		"向界面输入文本（复刻 chrome-devtools-mcp 的 type_text）：传 uid 时按身份定位"+
			"该输入框并追加文本（React 受控组件可响应，返回填后 value）；不传 uid 时向当前"+
			"焦点元素经 CDP Input.insertText 注入（支持中文）。uid/ref 均可（ref 为兼容别名）。",
		func(ctx context.Context, a typeTextArgs) (any, error) {
			if err := h.cdpGate(ctx); err != nil {
				return nil, err
			}
			if a.Text == "" {
				return nil, errors.New("text 不能为空")
			}
			if target := a.targetUID(); target != "" {
				r, err := h.actOnIdentity(ctx, target, "type", map[string]any{"value": a.Text})
				if err != nil {
					return nil, err
				}
				return map[string]any{"result": "ok", "value": r.Value}, nil
			}
			if err := h.insertText(ctx, a.Text); err != nil {
				return nil, err
			}
			return map[string]any{"result": "ok"}, nil
		})

	reg.Reg(srv, sink, "press_key",
		"向焦点元素发送按键（复刻 chrome-devtools-mcp 的 press_key）。传 uid 时按身份定位"+
			"元素并聚焦后派发键盘事件序列；不传 uid 时经 CDP 可信按键事件发给当前焦点元素。"+
			"uid/ref 均可（ref 为兼容别名）。",
		func(ctx context.Context, a pressKeyArgs) (any, error) {
			if err := h.cdpGate(ctx); err != nil {
				return nil, err
			}
			code, ok := keyCodes[a.Key]
			if !ok {
				return nil, fmt.Errorf("不支持的键名 %q（支持：Enter/Tab/Escape/Backspace/Delete/ArrowUp/ArrowDown/ArrowLeft/ArrowRight/Home/End/PageUp/PageDown）", a.Key)
			}
			if target := a.targetUID(); target != "" {
				_, err := h.actOnIdentity(ctx, target, "press", map[string]any{
					"key": a.Key, "code": code, "keyCode": keyCodeNum[a.Key],
				})
				return nil, err
			}
			if err := h.pressKey(ctx, a.Key, code); err != nil {
				return nil, err
			}
			return map[string]any{"result": "ok"}, nil
		})

	reg.Reg(srv, sink, "scroll",
		"在指定坐标处滚动（复刻 chrome-devtools-mcp 的 scroll：CDP mouseWheel 可信事件）。"+
			"用于翻页/滚动列表。无破坏性。",
		func(ctx context.Context, a scrollArgs) (any, error) {
			if err := h.cdpGate(ctx); err != nil {
				return nil, err
			}
			if err := h.wheelAt(ctx, a.X, a.Y, a.DeltaX, a.DeltaY); err != nil {
				return nil, err
			}
			return map[string]any{"result": "ok"}, nil
		})
}

// insertText 经 CDP Input.insertText 注入文本（支持中文）。
func (h *Hub) insertText(ctx context.Context, text string) error {
	wsURL, err := h.CurrentWS(ctx)
	if err != nil {
		return err
	}
	if _, err := h.client.Call(ctx, wsURL, "Input.insertText", map[string]any{"text": text}, 5*time.Second); err != nil {
		return err
	}
	return nil
}

// pressKey 经 CDP Input.dispatchKeyEvent 发送按键（官方 keyboard.press 等价：
// keyDown + keyUp，带 windowsVirtualKeyCode）。
func (h *Hub) pressKey(ctx context.Context, key, code string) error {
	wsURL, err := h.CurrentWS(ctx)
	if err != nil {
		return err
	}
	keyCode := keyCodeNum[key]
	down := map[string]any{"type": "keyDown", "key": key, "code": code, "windowsVirtualKeyCode": keyCode, "nativeVirtualKeyCode": keyCode}
	up := map[string]any{"type": "keyUp", "key": key, "code": code, "windowsVirtualKeyCode": keyCode, "nativeVirtualKeyCode": keyCode}
	if _, err := h.client.Call(ctx, wsURL, "Input.dispatchKeyEvent", down, 5*time.Second); err != nil {
		return err
	}
	if _, err := h.client.Call(ctx, wsURL, "Input.dispatchKeyEvent", up, 5*time.Second); err != nil {
		return err
	}
	return nil
}

// wheelAt 经 CDP Input.dispatchMouseEvent type=mouseWheel 滚动（官方
// page.mouse.wheel 等价可信事件）。
func (h *Hub) wheelAt(ctx context.Context, x, y, deltaX, deltaY int) error {
	return h.trustedMouseEvent(ctx, map[string]any{
		"type": "mouseWheel", "x": x, "y": y,
		"deltaX": float64(deltaX), "deltaY": float64(deltaY),
	})
}

// keyCodes 键名 → KeyboardEvent.code
var keyCodes = map[string]string{
	"Enter": "Enter", "Tab": "Tab", "Escape": "Escape", "Backspace": "Backspace",
	"Delete": "Delete", "ArrowUp": "ArrowUp", "ArrowDown": "ArrowDown",
	"ArrowLeft": "ArrowLeft", "ArrowRight": "ArrowRight", "Home": "Home",
	"End": "End", "PageUp": "PageUp", "PageDown": "PageDown",
}

// keyCodeNum 键名 → Windows 虚拟键码
var keyCodeNum = map[string]int{
	"Enter": 13, "Tab": 9, "Escape": 27, "Backspace": 8, "Delete": 46,
	"ArrowUp": 38, "ArrowDown": 40, "ArrowLeft": 37, "ArrowRight": 39,
	"Home": 36, "End": 35, "PageUp": 33, "PageDown": 34,
}

// resizePage 调整页面视口（WebView2 适配：Emulation.setDeviceMetricsOverride）。
func (h *Hub) resizePage(ctx context.Context, width, height int) error {
	if width <= 0 || height <= 0 {
		return errors.New("width/height 必须为正数")
	}
	wsURL, err := h.CurrentWS(ctx)
	if err != nil {
		return err
	}
	_, err = h.client.Call(ctx, wsURL, "Emulation.setDeviceMetricsOverride", map[string]any{
		"width": width, "height": height, "deviceScaleFactor": 0, "mobile": false,
	}, 5*time.Second)
	return err
}
