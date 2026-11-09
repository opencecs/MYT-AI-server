package main

import (
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sync/atomic"
	"time"
	"unicode/utf8"
)

// --- 看门狗、设备地址查询、控制命令、事件转发 ---

func (a *App) startStallWatchdog() {
	if a.StallTimeout <= 0 {
		return
	}
	duration := time.Duration(a.StallTimeout) * time.Second
	a.stallMu.Lock()
	if a.stallTimer != nil {
		a.stallTimer.Reset(duration)
		a.stallMu.Unlock()
		return
	}
	a.lastFrame.Store(time.Now().UnixNano())
	a.stallTimer = time.AfterFunc(duration, a.onStallTick)
	a.stallMu.Unlock()
}

// onStallTick 每次 stallTimeout（默认 15s）到期检查一次。
// 阈值设计：设备 heartbeat#5 保活推帧（playarg 传入），5s 阈值与心跳周期踩线，
// 多窗口负载下心跳帧一延迟就误断连 → 重连风暴打爆设备。15s = 3 个心跳周期余量。
// 两级恢复：回退软解（mpv force_fallback，只执行一次）→ 断连重连。
// 回退软解在重连之前：GPU 硬解对该设备流不工作时，切 CPU 软解能出画面
// （CPU 软解容错强，用户已验证稳定）；软解后仍无帧才断连重连。
// （设备端 hwcodec 不处理 RTCP PLI，已移除原先的「PLI 救一次」环节。）
func (a *App) onStallTick() {
	if !a.Connected.Load() {
		return
	}
	noFrameFor := time.Since(time.Unix(0, a.lastFrame.Load()))
	stallTimeout := time.Duration(a.StallTimeout) * time.Second
	if noFrameFor < stallTimeout {
		// 正常流帧：续期继续看护
		a.stallMu.Lock()
		if a.stallTimer != nil {
			a.stallTimer.Reset(stallTimeout)
		}
		a.stallMu.Unlock()
		return
	}

	// 第一级：回退软解（mpv force_fallback 等价物，只执行一次）。
	// 当前硬解中 → 切 CPU 软解。软解容错强能出画面。
	if !a.swFallbackDone.CompareAndSwap(false, true) {
		if dec := a.Decoder.Load(); dec != nil && !dec.IsSoftware() {
			if dec.ReopenSoftware() {
				logf("[fallback] GPU decode stalled %ds, switched to software decoding\n", int(noFrameFor.Seconds()))
				a.stallMu.Lock()
				if a.stallTimer != nil {
					a.stallTimer.Reset(5 * time.Second)
				}
				a.stallMu.Unlock()
				return
			}
		}
	}

	// 第二级：软解后仍无帧（或已回退过）→ 断连重连
	logf("[app] stall timeout: no video for %ds, reconnecting\n", int(noFrameFor.Seconds()))
	a.setConnState(ConnDisconnected, 0)
}

func (a *App) stopStallWatchdog() {
	a.stallMu.Lock()
	if a.stallTimer != nil {
		a.stallTimer.Stop()
		a.stallTimer = nil
	}
	a.stallMu.Unlock()
}

func (a *App) DeviceHost() string {
	if p := a.rtspHost.Load(); p != nil {
		return *p
	}
	return ""
}

func (a *App) DeviceCtrlPort() string {
	if p := a.rtspPort.Load(); p != nil {
		return *p
	}
	return fmt.Sprintf("%d", a.CPort)
}

func (a *App) DeviceApiAddr() string {
	if a.ApiPort <= 0 {
		return ""
	}
	return fmt.Sprintf("%s:%d", a.DeviceHost(), a.ApiPort)
}

func (a *App) CanControlInput() bool {
	a.ctrlMu.Lock()
	defer a.ctrlMu.Unlock()
	return !a.Controlled || a.PrimaryAddr == a.SelfAddr || len(a.CtrlItems) > 0
}

func (a *App) SetPrimaryAddr(primary string) {
	a.ctrlMu.Lock()
	a.PrimaryAddr = primary
	if a.SelfAddr != "" && primary == a.SelfAddr {
		items := make([]ctrlItem, 0, len(a.AllCtrlItems))
		for _, it := range a.AllCtrlItems {
			if it.Addr != a.SelfAddr {
				items = append(items, ctrlItem{Addr: it.Addr, On: true})
			}
		}
		a.CtrlItems = items
	} else if a.Controlled && len(a.AllCtrlItems) > 0 {
		a.CtrlItems = nil
	}
	items := make([]ctrlItem, len(a.CtrlItems))
	copy(items, a.CtrlItems)
	a.ctrlMu.Unlock()
	if sink := a.Ctrl.Load(); sink != nil {
		// tebox 模式无矩阵控制列表，仅 RTSP 的 ControlChannel 需要应用
		if ctrl, ok := (*sink).(*ControlChannel); ok {
			applyCtrlList(items, ctrl)
		}
	}
}

func (a *App) SendCmd(cmd, para uint32) {
	if !a.CanControlInput() {
		return
	}
	if ctrl := a.Ctrl.Load(); ctrl != nil {
		(*ctrl).Send(BuildKeyCmd(cmd, para))
	}
}

func (a *App) SendKeyboard(pressed bool, scancode uint16, sdlkey int32, unicode uint32, sdlmod uint16) {
	if !a.CanControlInput() {
		return
	}
	if ctrl := a.Ctrl.Load(); ctrl != nil {
		(*ctrl).Send(BuildKeyboard(pressed, scancode, sdlkey, unicode, sdlmod))
	}
}

func (a *App) SendText(text string) {
	if !a.CanControlInput() {
		return
	}
	ctrl := a.Ctrl.Load()
	if ctrl == nil {
		return
	}
	// 单次消息体最多59字节，长文本分段发送
	for len(text) > 0 {
		chunk := text
		if len(chunk) > 59 {
			// 在59字节内按UTF-8 rune边界切割
			chunk = text[:59]
			for !utf8.RuneStart(text[len(chunk)]) && len(chunk) > 0 {
				chunk = chunk[:len(chunk)-1]
			}
			if len(chunk) == 0 {
				chunk = text[:59]
			}
		}
		(*ctrl).Send(BuildKeyText(chunk))
		text = text[len(chunk):]
		if len(text) > 0 {
			time.Sleep(30 * time.Millisecond)
		}
	}
}

func (a *App) SetClarity(level int) {
	old := atomic.SwapInt32(&a.Clarity, int32(level))
	if int(old) == level {
		return
	}
	// 档位写进 SETUP playarg 的 imagelevel（设备按会话解析），未连接时只存档，
	// 下次 Connect 自然生效。已连接则触发 RTSP 层快速重连应用新档。
	a.Playarg = rewriteImageLevel(a.Playarg, level)
	if a.RTSP != nil && a.RTSP.IsConnected() {
		go a.reconnectRTSP()
	}
}

var imageLevelRe = regexp.MustCompile(`imagelevel#\d+`)

// rewriteImageLevel 把 playarg 中的 imagelevel 档位替换为 level；缺失时追加。
func rewriteImageLevel(playarg string, level int) string {
	repl := fmt.Sprintf("imagelevel#%d", level)
	if imageLevelRe.MatchString(playarg) {
		return imageLevelRe.ReplaceAllString(playarg, repl)
	}
	if playarg == "" {
		return repl
	}
	return playarg + "#" + repl
}

func (a *App) ToggleRotate() {
	newVal := !a.Rotated.Load()
	a.Rotated.Store(newVal)
	a.setRendererRotation(newVal)
}

func (a *App) saveScreenshot() {
	// 直接调设备端截图接口（task=snap），复用首页截图卡片同款，不依赖渲染帧。
	// mytos 端口优先取 -apiport（主程序从容器 PortBindings 查表的准确 9082
	// 映射——NAT 随机映射下与 vport 无固定差值，V2 容器实测差 +2）；未传时
	// 回退旧公式 vport-2（本地桥接容器 9082 与 10000 映射差固定 -2，实测验证）。
	ip := *flagIP
	mytosPort := *flagApiPort
	if mytosPort <= 0 {
		vport := *flagVPort
		if ip == "" || vport < 2 {
			logf("[screenshot] no device ip/vport\n")
			return
		}
		mytosPort = vport - 2
	}
	if ip == "" {
		logf("[screenshot] no device ip\n")
		return
	}
	url := fmt.Sprintf("http://%s:%d/task=snap&level=3", ip, mytosPort)
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		logf("[screenshot] fetch failed: %v\n", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		logf("[screenshot] fetch status %d\n", resp.StatusCode)
		return
	}
	imgBytes, err := io.ReadAll(resp.Body)
	if err != nil || len(imgBytes) == 0 {
		logf("[screenshot] read body failed: %v len=%d\n", err, len(imgBytes))
		return
	}
	// 旋转状态：设备截图是原始方向，player 旋转后需转 90°CW。
	finalBytes := imgBytes
	if a.Rotated.Load() {
		if rotated, err := rotateJPEG90CW(imgBytes); err == nil {
			finalBytes = rotated
		}
	}
	path := saveScreenshotJPEG(finalBytes)
	if path != "" {
		a.screenshotMu.Lock()
		a.screenshotPath = path
		a.screenshotMu.Unlock()
		if a.OnScreenshot != nil {
			a.OnScreenshot()
		}
		a.SendCmd(CmdAndroidKey, KeySnapshot)
		logf("[screenshot] saved via device snap: %s\n", path)
		}
}

func (a *App) ToggleTopmost() { a.Topmost.Store(!a.Topmost.Load()) }

func (a *App) OnMouseEvent(msg []byte) {
	if !a.CanControlInput() {
		return
	}
	if ctrl := a.Ctrl.Load(); ctrl != nil {
		(*ctrl).Send(msg)
	}
}

func (a *App) OnKeyboardEvent(msg []byte) {
	if !a.CanControlInput() {
		return
	}
	if ctrl := a.Ctrl.Load(); ctrl != nil {
		(*ctrl).Send(msg)
	}
}
