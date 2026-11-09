package main

import (
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"time"
)

func (m *MatrixManager) injectEvent(data []byte) {
	count := m.recvCount.Add(1)
	var ev map[string]any
	if err := json.Unmarshal(data, &ev); err != nil {
		return
	}
	postEvent, ok := convertInjectEvent(ev)
	if !ok {
		return
	}
	// 拷贝 inject 快照（SW/SH 可能被页面分辨率上报并发更新）
	m.injectMu.Lock()
	inj := *m.inject
	m.injectMu.Unlock()
	body := map[string]any{
		"targets": []map[string]any{{
			"device_id": inj.DeviceID,
			"container": inj.Container,
			"index":     inj.Index,
		}},
		"coord": "norm",
		// sw/sh 省略（传 0）：myt-agent 自动 dump_ui 推算设备真实分辨率，
		// 避免客户端探测的 video 分辨率（1920x1088，16 对齐）与设备真实分辨率（720x1280）不符导致坐标偏移。
		"event": postEvent,
	}
	go m.postInject(body, count, inj.BaseURL, inj.Token)
}

// convertInjectEvent 把内部语义事件转成 myt-agent inject event 格式。
func convertInjectEvent(ev map[string]any) (map[string]any, bool) {
	t, _ := ev["t"].(string)
	switch t {
	case "gesture":
		// 手势识别结果：click / swipe / long_press，一次性 POST 绕过 B 端限流
		gtype, _ := ev["gtype"].(string)
		switch gtype {
		case "click":
			return map[string]any{"type": "click", "nx": ev["x"], "ny": ev["y"]}, true
		case "swipe":
			return map[string]any{
				"type":     "swipe",
				"nx":       ev["x"], "ny": ev["y"],
				"nx2":      ev["x2"], "ny2": ev["y2"],
				"duration": ev["duration"],
			}, true
		case "long_press":
			return map[string]any{
				"type":     "long_press",
				"nx":       ev["x"], "ny": ev["y"],
				"duration": ev["duration"],
			}, true
		}
		return nil, false
	case "touch":
		action, _ := ev["action"].(string)
		typ := map[string]string{"down": "touch_down", "move": "touch_move", "up": "touch_up"}[action]
		if typ == "" {
			return nil, false
		}
		return map[string]any{
			"type": typ,
			"nx":  ev["x"],
			"ny":  ev["y"],
			"tid": ev["tid"],
		}, true
	case "nav":
		action, _ := ev["action"].(string)
		nav := map[string]string{"home": "goHome", "back": "goBack", "recent": "goRecent"}[action]
		if nav == "" {
			return nil, false
		}
		return map[string]any{"type": "nav", "nav": nav}, true
	case "key":
		return map[string]any{"type": "key", "keycode": ev["keycode"]}, true
	}
	return nil, false
}

// postInject 发送 inject 请求，失败丢弃（不阻断主控→被控 UDP 链路）。
func (m *MatrixManager) postInject(body map[string]any, count int64, baseURL, token string) {
	bodyBytes, _ := json.Marshal(body)
	req, err := http.NewRequest("POST", baseURL, strings.NewReader(string(bodyBytes)))
	if err != nil {
		return
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := m.httpClient.Do(req)
	if err != nil {
		if count <= 3 || count%100 == 1 {
			logf("[matrix-inject] POST 失败 #%d: %v\n", count, err)
		}
		return
	}
	defer resp.Body.Close()
	ic := m.injectCount.Add(1)
	if ic <= 3 || ic%100 == 1 {
		logf("[matrix-inject] POST ok #%d status=%d\n", ic, resp.StatusCode)
	}
}

// handleRoleMessage processes a role-switch message: updates primary state
// and target list, then notifies the page via __mxSetPrimary Eval.
func (m *MatrixManager) handleRoleMessage(msg map[string]any) {
	primary, _ := msg["primary"].(bool)
	if primary {
		if targetsRaw, ok := msg["targets"].([]any); ok {
			var targets []int
			for _, t := range targetsRaw {
				if v, ok := t.(float64); ok {
					targets = append(targets, int(v))
				}
			}
			if len(targets) > 0 {
				m.SetTargets(targets)
			}
		}
	}
	m.SetPrimary(primary)
}

// sendLoop drains the event channel and sends to all target addresses.
func (m *MatrixManager) sendLoop() {
	for {
		select {
		case <-m.stopCh:
			return
		case data := <-m.eventCh:
			m.sendToTargets(data)
		}
	}
}

func (m *MatrixManager) sendToTargets(data []byte) {
	if len(m.targetAddrs) == 0 {
		return
	}
	count := m.sentCount.Add(1)
	if count%100 == 1 {
		logf("[matrix] sent event #%d to %d targets\n", count, len(m.targetAddrs))
	}
	for _, addr := range m.targetAddrs {
		m.conn.SetWriteDeadline(time.Now().Add(50 * time.Millisecond))
		if _, err := m.conn.WriteTo(data, addr); err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}
			logf("[matrix] udp send error to %s: %v\n", addr, err)
		}
	}
	m.conn.SetWriteDeadline(time.Time{})
}

// SendNav broadcasts a navigation event to all controlled targets.
// action: "home" / "back" / "recent"。仅主控调用。
func (m *MatrixManager) SendNav(action string) {
	if !m.primary.Load() {
		return
	}
	data, _ := json.Marshal(map[string]any{"t": "nav", "action": action})
	select {
	case m.eventCh <- data:
	default:
	}
	logf("[matrix] send nav: %s\n", action)
}

// BroadcastReset broadcasts a reset event to all controlled targets.
// 用于主控切换后清掊被控残留触点。仅主控调用。
func (m *MatrixManager) BroadcastReset() {
	if !m.primary.Load() {
		return
	}
	data, _ := json.Marshal(map[string]any{"t": "reset"})
	select {
	case m.eventCh <- data:
	default:
	}
	logf("[matrix] broadcast reset\n")
}

// BroadcastDiag broadcasts a diag event to all controlled targets.
// 被控收到后调用 myt-agent 的 __mytControlDiag()，通道状态通过探针回传日志。
// 用于配合 myt-agent 诊断增强版，对照主控/被控的 sendChannel 与 status。
func (m *MatrixManager) BroadcastDiag() {
	if !m.primary.Load() {
		return
	}
	data, _ := json.Marshal(map[string]any{"t": "diag"})
	select {
	case m.eventCh <- data:
	default:
	}
	logf("[matrix] broadcast diag\n")
}
