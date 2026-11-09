package main

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	gortsplib "github.com/bluenviron/gortsplib/v5"
	"github.com/bluenviron/gortsplib/v5/pkg/base"
	"github.com/bluenviron/gortsplib/v5/pkg/description"
	"github.com/bluenviron/gortsplib/v5/pkg/format"
	"github.com/bluenviron/gortsplib/v5/pkg/headers"
)

// RTSPClient wraps gortsplib for RTSP streaming.
type RTSPClient struct {
	mu        sync.Mutex
	c         *gortsplib.Client
	connected atomic.Bool
	url       string
	doneCh    chan struct{} // closed when the current client is fully torn down

	// Playarg sent as body of RTSP SETUP request
	// (e.g. "imagelevel#2#platform#1")
	Playarg string

	// Callbacks
	OnH264Frame func(nalus [][]byte, pts time.Duration)
	// OnDescribeH264 DESCRIBE 完成、SDP 解析出 H264 格式后触发（SETUP/PLAY 之前，
	// 流未开始）：sps/pps 来自 SDP sprop-parameter-sets，SDP 不带参数集时为 nil。
	// 调用方据此预灌解码器（见 App.onDescribeSPSPPS）。仅在 Connect 的调用
	// goroutine 同步触发，无并发。
	OnDescribeH264 func(sps, pps []byte)
	OnAACFrame     func(data []byte)
	OnLgcmdData    func(data []byte) // device-initiated commands via lgcmd channel
	OnClose        func()
	OnKicked       func() // called when 454 detected during streaming (immediate)

	// AAC format info (set during setup)
	aacSampleRate int
	aacChannels   int

	// lgcmd 媒体通道（双向）：清晰度切换等 USERMSG 经此 RTP 通道发送
	lgcmdMedia *description.Media
	lgcmdFmt   *format.Generic

	// 会话 ID（PLAY 响应捕获）：SET_PARAMETER 独立连接必须携带，否则 454。
	sessionID string

	// setParamCSeq SET_PARAMETER 独立连接的 CSeq 递增计数（每次连接独立，从 1 起）
	setParamCSeq atomic.Int32

	// timingStart 握手分段计时起点（仅 Connect 调用 goroutine 设置，Connect 结束后不再使用）
	timingStart time.Time
	// timingOnSegment 握手分段回调：(阶段名, 相对计时起点累计耗时ms)。
	// 仅 Connect 内赋值/使用，其余时刻为 nil，无并发访问。
	timingOnSegment func(stage string, elapsedMs int64)
}

// ErrSessionKicked is returned when the RTSP session was taken over by another client.
var ErrSessionKicked = errors.New("session kicked by another client")

// isSessionKickedError checks if an RTSP error indicates the session was taken over (454).
func isSessionKickedError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "454")
}

// NewRTSPClient creates a new RTSP client.
func NewRTSPClient() *RTSPClient {
	return &RTSPClient{}
}

// Connect establishes the RTSP connection and starts receiving media.
// Lock discipline: r.mu is only held briefly when reading/writing shared state
// (r.c, r.url, r.doneCh). It is released before any blocking gortsplib call
// so that Close() can always acquire the lock and call c.Close() to unblock
// pending operations.
func (r *RTSPClient) Connect(rawURL, user, pass string, overTCP bool) error {
	r.mu.Lock()
	if r.c != nil {
		r.mu.Unlock()
		return fmt.Errorf("already connected")
	}
	r.url = rawURL
	r.mu.Unlock()

	u, err := base.ParseURL(rawURL)
	if err != nil {
		return fmt.Errorf("rtsp parse url: %w", err)
	}

	var protoPtr *gortsplib.Protocol
	if overTCP {
		proto := gortsplib.ProtocolTCP
		protoPtr = &proto
	}

	var playargBytes []byte
	if r.Playarg != "" {
		playargBytes = []byte(r.Playarg)
	}

	c := &gortsplib.Client{
		Scheme:        u.Scheme,
		Host:          u.Host,
		Protocol:      protoPtr,
		AnyPortEnable: true,
		ReadTimeout:   3600 * time.Second,
		WriteTimeout:  30 * time.Second,
		// 丢包感知帧门控丢包源：RTP 序号缺口 → 参考链可能断裂 → 冻结渲染等 IDR
		// 重同步（frame_gate.go）。默认实现只打 std log，这里接入门控状态机。
		OnPacketsLost: func(lost uint64) {
			gateOnPacketsLost(lost)
		},
		OnRequest: func(req *base.Request) {
			if playargBytes != nil && req.Method == base.Setup && len(req.Body) == 0 {
				req.Body = playargBytes
			}
		},
		OnResponse: func(res *base.Response) {
			if r.timingOnSegment != nil {
				r.timingOnSegment("response", time.Since(r.timingStart).Milliseconds())
			}
			if res.StatusCode != 200 {
				logf("[rtsp] OnResponse: status=%d\n", res.StatusCode)
			}
			// 401 未授权（首次请求无认证）：gortsplib 在 c.do() 内自动补认证重发，
			// 此后的 DESCRIBE/SETUP/PLAY 均带 Authorization 头（重试后的第二序列）
			if res.StatusCode == 401 && r.timingOnSegment != nil {
				r.timingOnSegment("401-unauthorized, second attempt follows", time.Since(r.timingStart).Milliseconds())
			}
			if res.StatusCode == 454 {
				logf("[rtsp] 454 detected - session kicked (connected=%v)\n", r.connected.Load())
				if r.OnKicked != nil {
					r.OnKicked()
				}
				return
			}
			if res.StatusCode == base.StatusOK && len(res.Body) > 0 {
				origLen := len(res.Body)
				res.Body = sanitizeSDP(res.Body)
				if len(res.Body) != origLen {
					logf("[rtsp] sanitizeSDP: %d -> %d bytes\n", origLen, len(res.Body))
				}
			}
		},
	}

	if user != "" {
		u.User = url.UserPassword(user, pass)
	}

	// 握手分段计时：各阶段点在 Connect 内打点，结束后输出一行汇总
	// （会话标识=URL 的 host:port，便于 v2 批量投屏多会话并行对齐）。
	var lastSegment string
	var prevElapsed int64
	seg := func(stage string, elapsed int64) {
		logf("[rtsp] %s %s=%dms\n", u.Host, stage, elapsed-prevElapsed)
		lastSegment = fmt.Sprintf("%s=%dms", stage, elapsed)
		prevElapsed = elapsed
	}
	var summary string
	r.timingOnSegment = func(stage string, elapsedMs int64) {
		seg(stage, elapsedMs)
		summary = summary + " " + lastSegment
	}
	defer func() {
		if summary != "" {
			logf("[rtsp] %s handshake total=%dms:%s\n", u.Host, time.Since(r.timingStart).Milliseconds(), summary)
		}
		r.timingOnSegment = nil
	}()

	// Store client so Close() can call c.Close() while we're blocked.
	r.mu.Lock()
	r.c = c
	r.mu.Unlock()

	r.timingStart = time.Now()
	if err := c.Start(); err != nil {
		r.mu.Lock()
		r.c = nil
		r.mu.Unlock()
		return fmt.Errorf("rtsp connect: %w", err)
	}
	if r.timingOnSegment != nil {
		r.timingOnSegment("conn", time.Since(r.timingStart).Milliseconds())
	}

	desc, _, err := c.Describe(u)
	if err != nil {
		c.Close()
		r.mu.Lock()
		r.c = nil
		r.mu.Unlock()
		return fmt.Errorf("rtsp describe: %w", err)
	}

	if r.timingOnSegment != nil {
		r.timingOnSegment("describe", time.Since(r.timingStart).Milliseconds())
	}
	logf("[rtsp] DESCRIBE ok, %d media streams\n", len(desc.Medias))

	var h264Media *description.Media
	var h264Format *format.H264
	var aacMedia *description.Media
	var aacFormat *format.MPEG4Audio
	var lgcmdMedia *description.Media
	var lgcmdFormat *format.Generic

	var h264F *format.H264
	if m := desc.FindFormat(&h264F); m != nil {
		h264Media = m
		h264Format = h264F
		// 通知调用方 SDP 参数集（SETUP/PLAY 之前，流未开始，无竞态）
		if r.OnDescribeH264 != nil {
			r.OnDescribeH264(h264F.SPS, h264F.PPS)
		}
	}
	var aacF *format.MPEG4Audio
	if m := desc.FindFormat(&aacF); m != nil {
		aacMedia = m
		aacFormat = aacF
	}
	var lgcmdF *format.Generic
	if m := desc.FindFormat(&lgcmdF); m != nil {
		lgcmdMedia = m
		lgcmdFormat = lgcmdF
	}

	if h264Media == nil && aacMedia == nil {
		logf("[rtsp] warning: no H264 or AAC media found in SDP\n")
	}
	if lgcmdMedia != nil {
		logf("[rtsp] lgcmd channel found\n")
	}

	// Setup media streams (track 454 errors to detect session takeover)
	var sessionKicked bool

	if h264Media != nil && h264Format != nil {
		logf("[rtsp] setting up H264 media...\n")
		if err := r.setupH264(c, desc, h264Media, h264Format); err != nil {
			logf("[rtsp] h264 setup failed: %v\n", err)
			if isSessionKickedError(err) {
				sessionKicked = true
			}
		} else {
			if r.timingOnSegment != nil {
				r.timingOnSegment("setup0", time.Since(r.timingStart).Milliseconds())
			}
			logf("[rtsp] H264 setup ok\n")
		}
	}

	if aacMedia != nil && aacFormat != nil {
		logf("[rtsp] setting up AAC media...\n")
		if err := r.setupAAC(c, desc, aacMedia, aacFormat); err != nil {
			logf("[rtsp] aac setup failed: %v\n", err)
			if isSessionKickedError(err) {
				sessionKicked = true
			}
		} else {
			if r.timingOnSegment != nil {
				r.timingOnSegment("setup1", time.Since(r.timingStart).Milliseconds())
			}
			logf("[rtsp] AAC setup ok\n")
		}
	}

	if lgcmdMedia != nil && lgcmdFormat != nil {
		logf("[rtsp] setting up lgcmd media...\n")
		if err := r.setupLgcmd(c, desc, lgcmdMedia, lgcmdFormat); err != nil {
			logf("[rtsp] lgcmd setup failed: %v\n", err)
			if isSessionKickedError(err) {
				sessionKicked = true
			}
		} else {
			r.mu.Lock()
			r.lgcmdMedia = lgcmdMedia // 保存用于反向发送 USERMSG（清晰度切换）
			r.lgcmdFmt = lgcmdFormat
			r.mu.Unlock()
			if r.timingOnSegment != nil {
				r.timingOnSegment("setup2", time.Since(r.timingStart).Milliseconds())
			}
			logf("[rtsp] lgcmd setup ok (PT=%d)\n", lgcmdFormat.PayloadType())
		}
	}

	logf("[rtsp] sending PLAY...\n")
	playResp, err := c.Play(nil)
	if err != nil {
		c.Close()
		r.mu.Lock()
		r.c = nil
		r.mu.Unlock()
		if sessionKicked {
			return ErrSessionKicked
		}
		return fmt.Errorf("rtsp play: %w", err)
	}
	// 记录会话 ID：SET_PARAMETER 等独立连接的请求必须携带 Session 头，
	// 否则严格实现的设备返回 454 Session Not Found（清晰度切换失效的根因）。
	if v, ok := playResp.Header["Session"]; ok && len(v) > 0 {
		var sx headers.Session
		if err := sx.Unmarshal(v); err == nil && sx.Session != "" {
			r.mu.Lock()
			r.sessionID = sx.Session
			r.mu.Unlock()
			logf("[rtsp] session id captured: %s\n", sx.Session)
		}
	}
	if r.timingOnSegment != nil {
		r.timingOnSegment("play", time.Since(r.timingStart).Milliseconds())
	}
	logf("[rtsp] PLAY ok, streaming\n")

	// Create doneCh only now — if Connect failed above, there's no Wait()
	// goroutine to wait for, so Close() skips the 3-second wait entirely.
	r.mu.Lock()
	doneCh := make(chan struct{})
	r.doneCh = doneCh
	r.connected.Store(true)
	r.mu.Unlock()

	// Background goroutine to detect RTSP disconnection.
	go func() {
		c.Wait()
		r.mu.Lock()
		if r.c != c {
			r.mu.Unlock()
			close(doneCh)
			return
		}
		wasConnected := r.connected.Swap(false)
		r.c = nil
		r.mu.Unlock()

		close(doneCh)

		if wasConnected {
			logf("[rtsp] connection lost\n")
			if r.OnClose != nil {
				r.OnClose()
			}
		}
	}()

	return nil
}

// Close shuts down the RTSP connection.
func (r *RTSPClient) Close() {
	r.mu.Lock()
	c := r.c
	doneCh := r.doneCh
	r.c = nil
	r.doneCh = nil
	r.connected.Store(false)
	r.mu.Unlock()

	if c != nil {
		c.Close()

		if doneCh != nil {
			select {
			case <-doneCh:
			case <-time.After(3 * time.Second):
			}
		}
	}
}

// IsConnected returns connection state.
func (r *RTSPClient) IsConnected() bool {
	return r.connected.Load()
}

// WaitDone returns a channel that is closed when the current RTSP client
// is fully torn down (Wait() goroutine has exited).
func (r *RTSPClient) WaitDone() <-chan struct{} {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.doneCh == nil {
		ch := make(chan struct{})
		close(ch)
		return ch
	}
	return r.doneCh
}

// GetParameter 查询设备 RTSP 参数（独立连接，带 Session 头）。诊断用。
func (r *RTSPClient) GetParameter(key string) (string, error) {
	r.mu.Lock()
	rawURL := r.url
	sessionID := r.sessionID
	r.mu.Unlock()
	if rawURL == "" {
		return "", fmt.Errorf("not connected")
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	host := u.Hostname()
	port := u.Port()
	if port == "" {
		port = "554"
	}
	var sessLine string
	if sessionID != "" {
		sessLine = fmt.Sprintf("Session: %s\r\n", sessionID)
	}
	body := fmt.Sprintf("%s\r\n", key)
	reqStr := fmt.Sprintf("GET_PARAMETER %s RTSP/1.0\r\n"+
		"CSeq: %d\r\n"+
		"%s"+
		"Content-Type: text/parameters\r\n"+
		"Content-Length: %d\r\n"+
		"\r\n"+
		"%s", rawURL, r.setParamCSeq.Add(1), sessLine, len(body), body)
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, port), 3*time.Second)
	if err != nil {
		return "", fmt.Errorf("GET_PARAMETER connect: %w", err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := conn.Write([]byte(reqStr)); err != nil {
		return "", fmt.Errorf("GET_PARAMETER write: %w", err)
	}
	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	if err != nil {
		return "", fmt.Errorf("GET_PARAMETER read: %w", err)
	}
	return string(buf[:n]), nil
}


