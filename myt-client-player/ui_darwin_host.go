//go:build darwin

package main

import "time"

// ─── Render loop ────────────────────────────────────────────────────────

func (ui *DarwinUI) renderLoop() {
	ticker := time.NewTicker(16 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			func() {
				defer func() {
					if r := recover(); r != nil {
						logf("[renderLoop] recovered: %v\n", r)
					}
				}()
				ui.processEvents()
				ui.processSidebarMessages()
				ui.processDropPaths()
				if ren := ui.app.MetalRen.Load(); ren != nil {
					ren.Present()
				}
			}()
		case <-ui.app.shutdownCh:
			return
		}
	}
}

// ─── Resize ────────────────────────────────────────────────────────────

func (ui *DarwinUI) handleResize(w, h int) {
	ui.mu.Lock()
	fs := ui.fullscreen
	ui.mu.Unlock()
	if !fs {
		h -= darwinNavBarH
	}
	if h < 0 {
		h = 0
	}
	if ren := ui.app.MetalRen.Load(); ren != nil {
		ren.UpdateSize(int32(w), int32(h))
	}
}

// ─── State sync ────────────────────────────────────────────────────────

func (ui *DarwinUI) syncSidebarState() {
	js := "if(window.updateState)updateState(" + ui.app.stateJSON() + ")"
	cEvalSidebarJS(js)
}

func (ui *DarwinUI) updateConnOverlay(state, retry int) {
	text := connStateText(state, retry)
	clickable := state == ConnKicked
	if state == ConnConnected && !ui.app.firstFrameSent.Load() {
		text = "已连接，等待画面..."
	}
	if text == "" {
		cHideConnOverlay()
	} else {
		cShowConnOverlay(text, clickable)
	}
}

// ─── MessageHost interface ──────────────────────────────────────────────

func (ui *DarwinUI) ToggleTopmost() {
	ui.app.ToggleTopmost()
	on := ui.app.Topmost.Load()
	cSetTopmost(on)
	ui.syncSidebarState()
}

func (ui *DarwinUI) ToggleRotate()           { ui.doToggleRotate() }
func (ui *DarwinUI) TogglePanel(expand bool) { ui.doTogglePanel(expand) }
func (ui *DarwinUI) ToggleFullscreen()       { ui.doToggleFullscreen() }
func (ui *DarwinUI) SyncState()              { ui.syncSidebarState() }

func (ui *DarwinUI) ReadClipboardText() string { return darwinReadClipboard() }

func (ui *DarwinUI) ArrangeWindows() { ui.doArrangeWindows() }

func (ui *DarwinUI) SetIPLocation(ip, lang string) { darwinSetIPLocation(ui.app, ip, lang) }
func (ui *DarwinUI) SendSMS(address, body string)   { darwinSendSMS(ui.app, address, body) }
func (ui *DarwinUI) AddContact(name, phone string)  { darwinAddContact(ui.app, name, phone) }
func (ui *DarwinUI) UploadFile()                     { darwinUploadFile(ui.app) }
func (ui *DarwinUI) UploadFileBase64(name, b64 string) { darwinUploadFileBase64(ui.app, name, b64) }

func (ui *DarwinUI) BroadcastPrimary(addr string) {
	broadcastPrimary(addr)
}
func (ui *DarwinUI) ListCameras() []string               { return nil }
func (ui *DarwinUI) ListMics() []string                   { return nil }
func (ui *DarwinUI) StartCameraPush(string, string, string) bool  { return false }
func (ui *DarwinUI) StopCameraPush()                      {}
func (ui *DarwinUI) SetSpeakerOff(bool)                   {}
func (ui *DarwinUI) IsSpeakerOff() bool                    { return false }
func (ui *DarwinUI) CameraPushRunning() bool               { return false }
