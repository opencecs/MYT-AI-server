// tebox 控制 UDP 客户端（9905）。
//
// 协议（对照 tebox INTEGRATION.md 第 3 节与 cast_view.c）：
//   - 每个事件一个 JSON 数据报（行结尾 \n），外层统一包 {"id":"<实例id>","seq":<递增>,...}；
//   - seq 由本端递增，服务端丢弃 seq 不大于上一个的包（防乱序）；
//   - 触摸容错：按住期间每 200ms 按当前位置补发 move（服务端 1s 收不到事件自动抬起，
//     防丢 up），up 连发两次（UDP 可能丢包，重复 up 无副作用）；
//     事件出错回 {"ok":false,...}，成功不回包。
//
// JSON 事件用强类型 struct 组装（encoding/json 自动平铺匿名字段），与上层仅通过
// 类型化方法 + 回调解耦。
package teboxcast

import (
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// 协议常量（cast_view.c 同款节奏）
const (
	// TouchKeepaliveInterval 按住期间 move 补发间隔
	TouchKeepaliveInterval = 200 * time.Millisecond
	// MaxDatagramSize 单数据报上限（超过丢弃，防 UDP 分片）
	MaxDatagramSize = 60000
)

// eventCommon 所有事件的公共包装字段（匿名嵌入自动平铺进 JSON）。
type eventCommon struct {
	ID  string `json:"id"`
	Seq uint64 `json:"seq"`
}

type touchEvent struct {
	eventCommon
	Type   string  `json:"type"`
	Action string  `json:"action"`
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
}

type keyEvent struct {
	eventCommon
	Type string `json:"type"`
	Key  string `json:"key"`
}

type keycodeEvent struct {
	eventCommon
	Type    string `json:"type"`
	Keycode int    `json:"keycode"`
	Meta    int    `json:"meta"`
}

type textEvent struct {
	eventCommon
	Type string `json:"type"`
	Text string `json:"text"`
}

type clipboardEvent struct {
	eventCommon
	Type  string `json:"type"`
	Text  string `json:"text"`
	Paste bool   `json:"paste,omitempty"`
}


// Control tebox 控制 UDP 客户端：JSON 事件发送 + 回包接收。
type Control struct {
	// Logf 日志回调（可选，空=静默）。
	Logf func(format string, args ...any)

	id   string
	host string
	port int
	conn *net.UDPConn

	seq       atomic.Uint64
	done      chan struct{}
	closeOnce sync.Once
	wg        sync.WaitGroup

	touchMu   sync.Mutex
	lastX     float64
	lastY     float64
	kaRunning bool
}

// NewControl 创建控制客户端（未连接；调用 Start）。
func NewControl(host string, port int, instanceID string) *Control {
	return &Control{
		id:   instanceID,
		done: make(chan struct{}),
		conn: nil,
		host: host,
		port: port,
	}
}

// Start 建立 UDP socket 并启动读循环。
func (c *Control) Start() error {
	addr, err := net.ResolveUDPAddr("udp", net.JoinHostPort(c.host, fmt.Sprintf("%d", c.port)))
	if err != nil {
		return fmt.Errorf("teboxctrl resolve: %w", err)
	}
	// 连接式 UDP socket：Write 发往对端，Read 收回包/剪贴板推送。
	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		return fmt.Errorf("teboxctrl dial: %w", err)
	}
	c.conn = conn

	c.wg.Add(1)
	go c.readLoop()
	return nil
}

// Close 停止读循环/保活并关闭 socket（幂等）。
//
// 必须先关 socket 再 wg.Wait：readLoop 阻塞在 conn.Read（UDP 无回包时永久
// 阻塞，close(done) 不会唤醒阻塞中的 Read），先 Wait 会死锁——Ctrl+C 关停
// 卡死的根因。conn.Close() 让 Read 立即返回错误 → readLoop 检查 done 后退出。
func (c *Control) Close() {
	c.closeOnce.Do(func() { close(c.done) })
	c.stopKeepalive()
	if c.conn != nil {
		c.conn.Close()
	}
	c.wg.Wait()
}

func (c *Control) logf(format string, args ...any) {
	if c.Logf != nil {
		c.Logf("[teboxctrl] "+format+"\n", args...)
	}
}

// sendEvent 组包（id + seq + 事件体）并发送；UDP 发送错误忽略（无连接可断，
// cast_view.c 同款：API 重启期间事件静默丢弃）。
func (c *Control) sendEvent(ev any) {
	if c.conn == nil {
		return
	}
	data, err := json.Marshal(ev)
	if err != nil {
		return
	}
	data = append(data, '\n')
	if len(data) > MaxDatagramSize {
		c.logf("event too large for UDP (%d bytes), dropped", len(data))
		return
	}
	if _, err := c.conn.Write(data); err != nil {
		c.logf("send failed: %v", err)
	}
}

// nextSeq 递增序号（防乱序：服务端丢弃 seq 不大于上一个的包）。
func (c *Control) nextSeq() uint64 { return c.seq.Add(1) }

// Touch 触摸事件：action 取 down/move/up，x/y 为 0–1 比例坐标（左上角原点）。
// down 启动 200ms move 保活；up 连发两次并停止保活。
func (c *Control) Touch(action string, x, y float64) {
	switch action {
	case "down":
		c.startKeepalive(x, y)
	case "move":
		c.touchMu.Lock()
		c.lastX, c.lastY = x, y
		c.touchMu.Unlock()
	case "up":
		c.stopKeepalive()
		// 双发：丢一个 up 会让手指一直按着（服务端 1s 看门狗才兜底释放）
		c.sendEvent(touchEvent{eventCommon: c.common(), Type: "touch", Action: "up", X: x, Y: y})
	}
	c.sendEvent(touchEvent{eventCommon: c.common(), Type: "touch", Action: action, X: x, Y: y})
}

// Key 导航键（back/home/menu/power/volume_up/volume_down/mute 或 QEMU qcode），按下并松开。
func (c *Control) Key(key string) {
	c.sendEvent(keyEvent{eventCommon: c.common(), Type: "key", Key: key})
}

// Keycode Android KEYCODE_* 事件（meta 位：0x41=Shift，0x3000=Ctrl）。
func (c *Control) Keycode(keycode, meta int) {
	c.sendEvent(keycodeEvent{eventCommon: c.common(), Type: "keycode", Keycode: keycode, Meta: meta})
}

// Text 文本输入（进当前焦点输入框；中文等经剪贴板粘贴，会覆盖 guest 剪贴板）。
func (c *Control) Text(text string) {
	c.sendEvent(textEvent{eventCommon: c.common(), Type: "text", Text: text})
}

// Clipboard 设置 guest 剪贴板；paste=true 时同时粘贴。
func (c *Control) Clipboard(text string, paste bool) {
	c.sendEvent(clipboardEvent{eventCommon: c.common(), Type: "clipboard", Text: text, Paste: paste})
}

// common 组装公共字段（每次调用递增 seq）。
func (c *Control) common() eventCommon {
	return eventCommon{ID: c.id, Seq: c.nextSeq()}
}

// startKeepalive 按住期间每 200ms 按最近坐标补发 move。
func (c *Control) startKeepalive(x, y float64) {
	c.touchMu.Lock()
	defer c.touchMu.Unlock()
	c.lastX, c.lastY = x, y
	if c.kaRunning {
		return
	}
	c.kaRunning = true
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		ticker := time.NewTicker(TouchKeepaliveInterval)
		defer ticker.Stop()
		for {
			select {
			case <-c.done:
				return
			case <-ticker.C:
			}
			c.touchMu.Lock()
			stillHeld := c.kaRunning
			mx, my := c.lastX, c.lastY
			c.touchMu.Unlock()
			if !stillHeld {
				return
			}
			c.sendEvent(touchEvent{eventCommon: c.common(), Type: "touch", Action: "move", X: mx, Y: my})
		}
	}()
}

func (c *Control) stopKeepalive() {
	c.touchMu.Lock()
	c.kaRunning = false
	c.touchMu.Unlock()
}


// readLoop 接收回包（按 \n 分行解析 JSON）。
func (c *Control) readLoop() {
	defer c.wg.Done()
	buf := make([]byte, 64*1024)
	var line []byte
	for {
		n, err := c.conn.Read(buf)
		if err != nil {
			select {
			case <-c.done:
				return
			default:
			}
			// ECONNREFUSED 等（API 短暂重启）：继续收，cast_view.c 同款
			c.logf("read error: %v", err)
			time.Sleep(200 * time.Millisecond)
			continue
		}
		for i := 0; i < n; i++ {
			if buf[i] != '\n' {
				line = append(line, buf[i])
				continue
			}
			c.handleLine(line)
			line = line[:0]
		}
	}
}

// handleLine 解析一行回包：错误行 → 日志。
func (c *Control) handleLine(line []byte) {
	if len(line) == 0 {
		return
	}
	var probe struct {
		OK    *bool  `json:"ok"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(line, &probe); err != nil {
		return
	}
	if probe.OK != nil && !*probe.OK {
		c.logf("server error: %s", probe.Error)
	}
}
