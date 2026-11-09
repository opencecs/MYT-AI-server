// v4ws 连接的收尾路径：拨号失败（升级前拒绝）与读循环结束（对端关闭 / 看门狗 /
// 本端 Stop）的错误分类，以及事件帧日志与 resync 写路径。
//
// 协议文档 §9 关闭码对照表的落点：1013 立即重连、1011/1006 退避重连、
// 1008 属确定性错误（走封顶退避 + 日志节流）、401 上报认证失败、503 上报离线。
package v4ws

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/coder/websocket"
)

// handleDialFailure 拨号（含升级前拒绝）失败分类。
func (c *conn) handleDialFailure(dialErr error, resp *http.Response) {
	status := 0
	if resp != nil {
		status = resp.StatusCode
	}
	switch status {
	case http.StatusUnauthorized:
		// 401：Basic Auth 失败，升级前拒绝。退避重连无效，需修配置（默认 admin/myt）
		if c.shouldLogFailure() {
			log.Printf("[v4ws] %s WS 401——Basic Auth 失败（检查 tebox 用户名/密码配置）", c.host)
			c.log("🔑 401 认证失败（检查 Basic Auth 配置），退避重试")
		}
		c.reportState(StateAuthFail)
	case http.StatusServiceUnavailable:
		// 503：连接数已达上限 32（进程级共享），等其他连接释放
		if c.shouldLogFailure() {
			log.Printf("[v4ws] %s WS 503——服务端事件连接数已满（上限 32），退避重试", c.host)
			c.log("⏳ 503 连接数已满，退避重试")
		}
		c.reportState(StateOffline)
	default:
		// 网络不可达/超时/URL 非法等：必须落日志（否则设备被判离线时无从排查），同频节流
		if c.shouldLogFailure() {
			log.Printf("[v4ws] %s 拨号 %s 失败: %v", c.host, c.dialURL(), dialErr)
		}
		c.reportState(StateOffline)
	}
}

// handleReadEnd 读循环结束（对端关闭 / 看门狗 / 本端 Stop）的分类处理。
func (c *conn) handleReadEnd(err error) {
	if err == nil {
		return
	}
	select {
	case <-c.done:
		return // 主动停止：不上报离线
	default:
	}
	switch websocket.CloseStatus(err) {
	case websocket.StatusTryAgainLater: // 1013
		// 订阅队列溢出（慢消费者），缺口无法补齐——必须重连并重建快照。
		// 之前通常已收到 resync-required 帧（那里已触发重连），此处兜底。
		log.Printf("[v4ws] %s 连接被关闭 1013（事件队列溢出，需重连重建快照）", c.host)
		c.log("🔄 1013 事件队列溢出，重连重建快照")
		c.retryImmediate.Store(true)
	case websocket.StatusInternalError: // 1011
		// 快照构建失败（如全部后端不可达）：退避后重连会重新走 hello→snapshot
		log.Printf("[v4ws] %s 连接被关闭 1011（快照构建失败），退避重连", c.host)
		c.log("⚠ 1011 快照构建失败，退避重连")
	case websocket.StatusPolicyViolation: // 1008
		// 协议上 1008 只出现在旧 /ws/clipboard/:id（backend 不存在）。落在 /ws/events
		// 说明服务端实现有变，属确定性错误——不无脑高频重连，走封顶退避 + 节流日志
		if c.shouldLogFailure() {
			log.Printf("[v4ws] %s 连接被关闭 1008（策略违规，检查后端配置）: %v", c.host, err)
		}
		c.log("🚫 1008 策略违规（检查后端配置）")
	case websocket.StatusNormalClosure, websocket.StatusGoingAway:
		c.log("👋 服务端关闭连接，退避重连")
	default:
		// 1006 异常关闭 / 1005 无状态码 / 其他：按网络异常处理
		c.log("❌ 连接断开（%v），退避后重连", err)
	}
	c.reportState(StateOffline)
}

// logEvent 事件帧的 console 日志。
func (c *conn) logEvent(ev *EventFrame) {
	if c.logHook == nil {
		return
	}
	switch ev.Event {
	case EventInstance:
		item, err := ParseInstanceStateData(ev.Data)
		if err != nil {
			c.log("🐳 instance/%s seq=%d", ev.Action, ev.Seq)
			return
		}
		c.log("🐳 实例 %s %s → %s（running=%v）seq=%d",
			InstanceKey(item.Backend, item.ID), ev.Action, item.State, item.Running, ev.Seq)
	case EventClipboard:
		data, err := ParseClipboardData(ev.Data)
		if err != nil {
			c.log("📋 clipboard/%s seq=%d", ev.Action, ev.Seq)
			return
		}
		c.log("📋 剪贴板 %s：%d 字符 seq=%d", InstanceKey(data.Backend, data.ID), len([]rune(data.Text)), ev.Seq)
	case EventSystem:
		c.log("🖥 system/%s seq=%d", ev.Action, ev.Seq)
	default:
		c.log("❓ 未知事件 %s/%s seq=%d", ev.Event, ev.Action, ev.Seq)
	}
}

// SendResync 发送 {"action":"resync"}（限频 10s，间隔不足静默跳过——服务端同样限频）。
func (c *conn) SendResync() {
	c.resyncMu.Lock()
	if time.Since(c.lastResyncAt) < c.resyncMinInterval {
		c.resyncMu.Unlock()
		return
	}
	c.lastResyncAt = time.Now()
	c.resyncMu.Unlock()

	c.writeMu.Lock()
	ws := c.ws
	c.writeMu.Unlock()
	if ws == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := ws.Write(ctx, websocket.MessageText, []byte(`{"action":"resync"}`)); err != nil {
		log.Printf("[v4ws] %s resync 发送失败: %v", c.host, err)
	}
}
