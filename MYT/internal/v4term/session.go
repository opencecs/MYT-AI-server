// Package v4term 实现 V4 实例终端的 WS 会话客户端：连 tobox SDK 的
// `GET /android/:id/terminal`（终端 TTY 双向桥接）。target 查询参数选择
// shell 类型："android" = 安卓宿主 shell（即 adb shell 体验，uid=2000
// shell，实测 2026-10-22）；缺省 = 容器内 busybox shell（`~ #`，uid=0）。
//
// 协议（tobox 官方文档「终端（WebSocket）」，与 myt xterm 客户端兼容）：
//   - 服务端 → 客户端：二进制帧 = 终端输出；文本帧 = 提示信息（如
//     `[终端会话已结束]`）或错误文本（随后通常伴随 close 帧）。
//   - 客户端 → 服务端：文本帧 = raw stdin（**不是** JSON stdin——实测
//     2026-10-22 tobox v3 直接把文本帧原文写入 shell）；JSON 控制帧 =
//     `{"type":"resize","cols":N,"rows":N}`；login 帧被服务端忽略。
//   - 服务端 5s ping（库自动回 pong）；shell 退出发提示后 1001 关闭；
//     通道/实例错误发文本 + 1008 关闭；WS 断开服务端即 Kill exec。
package v4term

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// dialTimeout WS 建连超时
const dialTimeout = 10 * time.Second

// Session 一个 V4 终端 WS 会话。
// onData 输出回调（原始字节）；onExit 退出回调（code=0 正常 / 1 异常）。
type Session struct {
	ws      *websocket.Conn
	writeMu sync.Mutex
	onData  func(data []byte)
	onExit  func(code int)

	closeOnce sync.Once
	exitOnce  sync.Once
}

// Dial 连接 V4 终端 WS 并开始读循环。url 形如
// ws://host:8088/android/<实例id>/terminal；target 追加为 `target=` 查询
// 参数（"android" = 直接进安卓 shell，即 adb shell 体验；空串 = 容器内
// busybox shell，SDK 默认）；user/password 为 Basic Auth 凭据（服务端
// auth 关闭时服务端忽略之，客户端照发无害）。
func Dial(url, target, user, password string, cols, rows int, onData func([]byte), onExit func(code int)) (*Session, error) {
	if target != "" {
		sep := "?"
		if strings.Contains(url, "?") {
			sep = "&"
		}
		url += sep + "target=" + target
	}
	if cols > 0 || rows > 0 {
		if cols <= 0 {
			cols = 80
		}
		if rows <= 0 {
			rows = 24
		}
		sep := "?"
		if strings.Contains(url, "?") {
			sep = "&"
		}
		url += fmt.Sprintf("%scols=%d&rows=%d", sep, cols, rows)
	}
	ctx, cancel := context.WithTimeout(context.Background(), dialTimeout)
	defer cancel()
	h := http.Header{}
	if user != "" || password != "" {
		h.Set("Authorization", "Basic "+basicAuth(user, password))
	}
	ws, resp, err := websocket.Dial(ctx, url, &websocket.DialOptions{HTTPHeader: h})
	if err != nil {
		if resp != nil {
			return nil, classifyDialError(resp.StatusCode, resp.Body)
		}
		return nil, fmt.Errorf("连接终端服务失败: %w", err)
	}
	s := &Session{ws: ws, onData: onData, onExit: onExit}
	go s.readLoop()
	return s, nil
}

// classifyDialError 把升级失败响应转为可读错误（读小体积 JSON 响应体的 message）
func classifyDialError(status int, body interface{ Read([]byte) (int, error) }) error {
	msg := ""
	if body != nil {
		buf := make([]byte, 512)
		if n, _ := body.Read(buf); n > 0 {
			var envelope struct {
				Message string `json:"message"`
			}
			if json.Unmarshal(buf[:n], &envelope) == nil && envelope.Message != "" {
				msg = envelope.Message
			}
		}
	}
	switch status {
	case http.StatusUnauthorized:
		if msg != "" {
			return fmt.Errorf("终端认证失败（401）: %s", msg)
		}
		return errors.New("终端认证失败（401），检查 Basic Auth 凭据")
	case http.StatusNotFound:
		if msg != "" {
			return fmt.Errorf("终端打开失败（404）: %s", msg)
		}
		return errors.New("终端打开失败（404），实例不存在")
	case http.StatusConflict:
		if msg != "" {
			return fmt.Errorf("终端打开失败（409）: %s", msg)
		}
		return errors.New("终端打开失败（409），实例未运行")
	case http.StatusNotImplemented:
		if msg != "" {
			return fmt.Errorf("终端打开失败（501）: %s", msg)
		}
		return errors.New("终端打开失败（501），设备端终端功能未启用（terminal.enabled）")
	}
	if msg != "" {
		return fmt.Errorf("终端打开失败（HTTP %d）: %s", status, msg)
	}
	return fmt.Errorf("终端打开失败（HTTP %d）", status)
}

// readLoop 读循环：二进制帧 → onData；文本帧多为提示（shell 退出前
// `[终端会话已结束]`），交由 close 分类统一走 onExit——文本帧不重复上报。
func (s *Session) readLoop() {
	exitCode := 0
	for {
		mt, data, err := s.ws.Read(context.Background())
		if err != nil {
			s.finish(classifyClose(websocket.CloseStatus(err), exitCode))
			return
		}
		if len(data) == 0 {
			continue
		}
		if mt == websocket.MessageBinary {
			s.onData(data)
			continue
		}
		// 文本帧：`[终端会话已结束]` 是 shell 正常退出的前奏（随后 1001 close），
		// 无需上报内容本身；其他文本（1008 前的错误说明）在 classifyClose 里
		// 也只映射为退出码。此处忽略文本载荷。
	}
}

// classifyClose 把 WS 关闭状态映射为退出码：
//   - 1001（服务端正常关，exec 退出/无实例就绪）→ 0
//   - 1008（策略违规，通道/实例问题，服务端已发错误文本）→ 1
//   - 其余（1006 网络断等）→ 1
func classifyClose(code websocket.StatusCode, normal int) int {
	if code == websocket.StatusGoingAway || code == websocket.StatusNormalClosure {
		return normal
	}
	return 1
}

// Write 发送 raw stdin（xterm 按键数据）——**文本帧原文**，服务端直接写入
// shell（该 SDK 版本不解析 JSON stdin 帧，实测会原样透传）。
func (s *Session) Write(data []byte) error {
	if len(data) == 0 {
		return nil
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return s.ws.Write(context.Background(), websocket.MessageText, data)
}

// Resize 同步终端窗口大小（JSON 控制帧；0 尺寸忽略）
func (s *Session) Resize(cols, rows int) error {
	if cols <= 0 || rows <= 0 {
		return nil
	}
	payload, err := json.Marshal(resizeFrame{Type: "resize", Cols: cols, Rows: rows})
	if err != nil {
		return err
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return s.ws.Write(context.Background(), websocket.MessageText, payload)
}

// Close 关闭会话（1000 正常关闭；服务端收到后 Kill exec）
func (s *Session) Close() error {
	s.closeOnce.Do(func() {
		_ = s.ws.Close(websocket.StatusNormalClosure, "client close")
	})
	return nil
}

// finish 结束会话（退出回调只触发一次）
func (s *Session) finish(code int) {
	s.closeOnce.Do(func() {
		_ = s.ws.Close(websocket.StatusNormalClosure, "session end")
	})
	s.exitOnce.Do(func() {
		if s.onExit != nil {
			s.onExit(code)
		}
	})
}

// resizeFrame resize 控制帧
type resizeFrame struct {
	Type string `json:"type"`
	Cols int    `json:"cols"`
	Rows int    `json:"rows"`
}

// basicAuth 生成 Basic Auth 头值
func basicAuth(user, password string) string {
	return base64Encode(user + ":" + password)
}
