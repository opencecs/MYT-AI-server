//go:build !windows && !darwin && !linux

package main

import (
	"os"
	"os/signal"
	"syscall"
)

// SimpleUI 非 Win/mac/Linux 平台桩（如 js/wasm 构建）：无窗口，Connect 后阻塞。

func init() {
	_newPlatformUIImpl = func(app *App) PlatformUI {
		return &SimpleUI{app: app}
	}
}

func (ui *SimpleUI) Run() {
	app := ui.app

	logf("[main] SimpleUI not available on this platform\n")

	if app.RTSPURL != "" {
		if err := app.Connect(); err != nil {
			logf("[main] connect failed: %v\n", err)
		}
	}

	// Block until signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	app.Disconnect()
}

// SimpleUI implements MessageHost for basic stub mode
func (ui *SimpleUI) ToggleTopmost()                      {}
func (ui *SimpleUI) ToggleRotate()                       { ui.app.ToggleRotate() }
func (ui *SimpleUI) TogglePanel(bool)                    {}
func (ui *SimpleUI) ToggleFullscreen()                   {}
func (ui *SimpleUI) SyncState()                          {}
func (ui *SimpleUI) ClipboardCopy()                      {}
func (ui *SimpleUI) ClipboardCut()                       {}
func (ui *SimpleUI) ReadClipboardText() string           { return "" }
func (ui *SimpleUI) ArrangeWindows()                     {}
func (ui *SimpleUI) SetIPLocation(string, string)        {}
func (ui *SimpleUI) SendSMS(string, string)              {}
func (ui *SimpleUI) AddContact(string, string)           {}
func (ui *SimpleUI) UploadFile()                         {}
func (ui *SimpleUI) UploadFileBase64(string, string)     {}
func (ui *SimpleUI) BroadcastPrimary(string)             {}
func (ui *SimpleUI) ListCameras() []string               { return nil }
func (ui *SimpleUI) ListMics() []string                  { return nil }
func (ui *SimpleUI) StartCameraPush(string, string, string) bool { return false }
func (ui *SimpleUI) StopCameraPush()                     {}
func (ui *SimpleUI) SetSpeakerOff(bool)                  {}
func (ui *SimpleUI) IsSpeakerOff() bool                  { return false }
func (ui *SimpleUI) CameraPushRunning() bool             { return false }
