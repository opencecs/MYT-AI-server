package main

import (
	"errors"
	"runtime"
	"sync/atomic"
	"time"
)

// ─── Connection state ─────────────────────────────────────────────────────────

func (a *App) setConnState(state int, retry int) {
	logf("[conn] setConnState: state=%d retry=%d connected=%v\n", state, retry, a.Connected.Load())
	atomic.StoreInt32(&a.ConnState, int32(state))
	atomic.StoreInt32(&a.ConnRetry, int32(retry))

	if r := a.activeRenderer(); r != nil {
		if state == ConnConnected {
			r.SetOverlay("")
		} else {
			r.SetOverlay(connStateText(state, retry))
		}
	}

	if a.OnConnStateChange != nil {
		a.OnConnStateChange(state, retry)
	}

	switch state {
	case ConnDisconnected:
		if a.Connected.Load() {
			a.Connected.Store(false)
			a.connMu.Lock()
			if !a.reconnecting {
				a.reconnecting = true
				a.connMu.Unlock()
				go a.autoReconnect(retry)
			} else {
				a.connMu.Unlock()
			}
		}
	case ConnError:
		a.Connected.Store(false)
		a.connMu.Lock()
		if !a.reconnecting {
			a.reconnecting = true
			a.connMu.Unlock()
			go a.autoReconnect(retry)
		} else {
			a.connMu.Unlock()
		}
	case ConnKicked:
		a.Connected.Store(false)
		// No auto-reconnect - another client has taken over
	case ConnConnected:
		a.Connected.Store(true)
		a.startClipboardSync()
	}
}

func (a *App) autoReconnect(retryCount int) {
	defer func() {
		a.connMu.Lock()
		a.reconnecting = false
		a.connMu.Unlock()
	}()

	for {
		// If kicked, stop reconnecting immediately
		if a.kicked.Load() {
			logf("[conn] kicked - stopping reconnect\n")
			return
		}

		// MaxRetry=0 means infinite reconnect
		if a.MaxRetry > 0 && retryCount >= a.MaxRetry {
			logf("[conn] max retries (%d) reached\n", a.MaxRetry)
			a.setConnState(ConnKicked, retryCount)
			return
		}

		delay := a.retryDelay
		if retryCount > 0 {
			delay = a.retryDelay + retryCount*2
		}
		if delay > 30 {
			delay = 30
		}

		logf("[conn] reconnect in %ds (attempt %d)\n", delay, retryCount+1)
		a.setConnState(ConnRetrying, retryCount+1)
		select {
		case <-time.After(time.Duration(delay) * time.Second):
		case <-a.shutdownCh:
			return
		}

		if a.Connected.Load() || a.kicked.Load() {
			return
		}

		logln("[conn] stopping old connection...")
		a.Disconnect()
		runtime.GC()

		select {
		case <-a.RTSP.WaitDone():
		case <-time.After(5 * time.Second):
		}

		logln("[conn] starting new connection...")
		a.resetDisconnected()

		// Don't attempt reconnection if the app is shutting down.
		select {
		case <-a.shutdownCh:
			logf("[conn] shutdown during reconnect, stopping\n")
			return
		default:
		}

		if err := a.Connect(); err != nil {
			if a.kicked.Load() {
				return
			}
			select {
			case <-a.shutdownCh:
				return
			default:
			}
			logf("[conn] reconnect failed: %v\n", err)
			retryCount++
			continue
		}
		return
	}
}

// ManualReconnect is called when the user clicks to retry after being kicked.
func (a *App) ManualReconnect() {
	a.kicked.Store(false)
	a.connMu.Lock()
	if a.reconnecting {
		a.connMu.Unlock()
		return
	}
	a.reconnecting = true
	a.connMu.Unlock()

	a.Disconnect()
	select {
	case <-a.RTSP.WaitDone():
	case <-time.After(3 * time.Second):
	}
	a.resetDisconnected()
	go a.autoReconnect(0)
}

// reconnectRTSP 清晰度切换专用：只重连 RTSP 层（保解码器/渲染器/控制通道），
// 档位经 SETUP playarg 的 imagelevel 生效（设备按会话解析，SET_PARAMETER 不重解析）。
// 各档位分辨率相同（1280x720），解码器无需重建，画面保持最后一帧平滑过渡。
func (a *App) reconnectRTSP() {
	if !a.claritySwitching.CompareAndSwap(false, true) {
		return
	}
	defer a.claritySwitching.Store(false)

	// 快速重连期间避免 autoReconnect 并发介入（OnClose 故意关闭不触发，此处双保险）
	a.connMu.Lock()
	if a.reconnecting {
		a.connMu.Unlock()
		return
	}
	a.connMu.Unlock()

	logf("[clarity] reconnecting RTSP with playarg: %s\n", a.Playarg)
	if r := a.activeRenderer(); r != nil {
		r.SetOverlay("⟳ 切换清晰度...")
	}
	a.lastFrame.Store(time.Now().UnixNano())

	a.RTSP.Close()
	select {
	case <-a.RTSP.WaitDone():
	case <-time.After(2 * time.Second):
	}

	// 关键：同步最新 playarg 到 RTSP 客户端（Connect 读 r.Playarg 注入 SETUP body，
	// 只在 setupRTSPCallbacks 里同步过一次——漏了这步重连发的还是旧档位，
	// 设备永远解析出 imagelevel#1）
	a.RTSP.Playarg = a.Playarg

	if err := a.RTSP.Connect(a.RTSPURL, a.User, a.Pass, a.RTSPTCP); err != nil {
		logf("[clarity] RTSP reconnect failed: %v, falling back to full reconnect\n", err)
		if errors.Is(err, ErrSessionKicked) {
			a.setConnState(ConnKicked, 0)
			return
		}
		// 兜底走全量重连（重试延迟由既有策略管理）
		a.setConnState(ConnError, 0)
		return
	}

	// overlayQuiet：跳过 UI 层"等待画面"overlay（切换语义），改为清除提示
	a.overlayQuiet.Store(true)
	a.setConnState(ConnConnected, 0)
	a.overlayQuiet.Store(false)
	// 清除切换提示（setConnState(Connected) 已清渲染器 overlay，这里确保 UI 层干净）；
	// 不重置 firstFrameSent：切换语义下旧帧定格过渡，新帧到达后自然覆盖渲染
	a.lastFrame.Store(time.Now().UnixNano())
	level := int(atomic.LoadInt32(&a.Clarity))
	logf("[clarity] RTSP reconnected, level=%d\n", level)
	if a.OnClaritySwitched != nil {
		a.OnClaritySwitched(level)
	}
}
