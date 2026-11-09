package main

import (
	"encoding/json"
	"sync/atomic"
	"unicode"
)

// ─── WebView message dispatch ─────────────────────────────────────────────────

type MessageHost interface {
	ToggleTopmost()
	ToggleRotate()
	TogglePanel(expand bool)
	ToggleFullscreen()
	SyncState()
	ClipboardCopy()
	ClipboardCut()
	ReadClipboardText() string
	ArrangeWindows()
	SetIPLocation(ip, lang string)
	SendSMS(address, body string)
	AddContact(name, phone string)
	UploadFile()
	UploadFileBase64(name, b64 string)
	BroadcastPrimary(addr string)
	ListCameras() []string
	ListMics() []string
	StartCameraPush(camName, micName, src string) bool
	StopCameraPush()
	SetSpeakerOff(off bool)
	IsSpeakerOff() bool
	CameraPushRunning() bool
}

func (a *App) dispatchMessage(raw string, host MessageHost) {
	logf("[dispatch] raw=%s\n", raw)
	var msg map[string]any
	if err := json.Unmarshal([]byte(raw), &msg); err != nil {
		return
	}
	cmd, _ := msg["cmd"].(string)
	argStr, _ := msg["arg"].(string)

	switch cmd {
	case "back":
		logf("[dispatch] back: WebCmdForward=%v\n", a.WebCmdForward != nil)
		if a.WebCmdForward != nil {
			a.WebCmdForward("goBack")
		} else {
			a.SendCmd(CmdAndroidKey, KeyBack)
		}
	case "home":
		logf("[dispatch] home: WebCmdForward=%v\n", a.WebCmdForward != nil)
		if a.WebCmdForward != nil {
			a.WebCmdForward("goHome")
		} else {
			a.SendCmd(CmdAndroidKey, KeyHome)
		}
	case "recent":
		logf("[dispatch] recent: WebCmdForward=%v\n", a.WebCmdForward != nil)
		if a.WebCmdForward != nil {
			a.WebCmdForward("goClean")
		} else {
			a.SendCmd(CmdAndroidKey, KeyRecent)
		}
	case "topmost":
		host.ToggleTopmost()
	case "rotate":
		host.ToggleRotate()
	case "fullscreen":
		host.ToggleFullscreen()
	case "clarity":
		switch argStr {
		case "1":
			a.SetClarity(1)
		case "2":
			a.SetClarity(2)
		case "3":
			a.SetClarity(3)
		case "4":
			a.SetClarity(4)
		}
	case "panel":
		host.TogglePanel(argStr == "expand")
		if argStr == "expand" {
			host.SyncState()
		}
	case "screenshot":
		go a.saveScreenshot()
	case "vol_up":
		a.SendCmd(CmdAndroidKey, KeyVolUp)
	case "vol_down":
		a.SendCmd(CmdAndroidKey, KeyVolDown)
	case "kbd":
		a.dispatchKeyboard(msg, host)
	case "text":
		text, _ := msg["text"].(string)
		if text != "" {
			go a.SendText(text)
		}
	case "arrange":
		go host.ArrangeWindows()
	case "ctrl_toggle":
		a.ctrlMu.Lock()
		for i := range a.CtrlItems {
			if a.CtrlItems[i].Addr == argStr {
				a.CtrlItems[i].On = !a.CtrlItems[i].On
				break
			}
		}
		items := a.CtrlItems
		a.ctrlMu.Unlock()
		if sink := a.Ctrl.Load(); sink != nil {
			// tebox 模式无矩阵控制列表，仅 RTSP 的 ControlChannel 需要应用
			if ctrl, ok := (*sink).(*ControlChannel); ok {
				applyCtrlList(items, ctrl)
			}
		}
	case "set_primary":
		if argStr != "" {
			a.SetPrimaryAddr(argStr)
			host.BroadcastPrimary(argStr)
			host.SyncState()
		}
		return
	case "ctrl_remove":
		a.ctrlMu.Lock()
		for i := range a.CtrlItems {
			if a.CtrlItems[i].Addr == argStr {
				a.CtrlItems = append(a.CtrlItems[:i], a.CtrlItems[i+1:]...)
				break
			}
		}
		items := a.CtrlItems
		a.ctrlMu.Unlock()
		if sink := a.Ctrl.Load(); sink != nil {
			// tebox 模式无矩阵控制列表，仅 RTSP 的 ControlChannel 需要应用
			if ctrl, ok := (*sink).(*ControlChannel); ok {
				applyCtrlList(items, ctrl)
			}
		}
	case "ip_location":
		lang, _ := msg["lang"].(string)
		go host.SetIPLocation(argStr, lang)
	case "send_sms":
		address, _ := msg["address"].(string)
		mbody, _ := msg["mbody"].(string)
		go host.SendSMS(address, mbody)
	case "add_contact":
		name, _ := msg["name"].(string)
		tel, _ := msg["tel"].(string)
		go host.AddContact(name, tel)
	case "upload_file":
		// 侧边栏勾选「自动授予运行时权限」随消息传入（对话框是模态的，读取时序安全）
		autogrant, _ := msg["autogrant"].(bool)
		apkAutoGrant.Store(autogrant)
		go host.UploadFile()
	case "upload_file_base64":
		name, _ := msg["name"].(string)
		b64, _ := msg["data"].(string)
		autogrant, _ := msg["autogrant"].(bool)
		apkAutoGrant.Store(autogrant)
		if name != "" && b64 != "" {
			go host.UploadFileBase64(name, b64)
		}
	case "cam_start", "cam_switch":
		micName, _ := msg["mic"].(string)
		src, _ := msg["src"].(string)
		if !isPseudoSrc(src) {
			src = camSrcCam // 非法值回落真实摄像头
		}
		if src != camSrcCam {
			argStr = "" // 伪源不携带摄像头名
		}
		go func() {
			host.StartCameraPush(argStr, micName, src)
			host.SyncState()
		}()
	case "cam_stop":
		go func() {
			host.StopCameraPush()
			host.SyncState()
		}()
	case "cam_list":
		cameras := host.ListCameras()
		if h, ok := host.(interface{ EvalCameraList([]string) }); ok {
			h.EvalCameraList(cameras)
		}
	case "mic_list":
		mics := host.ListMics()
		if h, ok := host.(interface{ EvalMicList([]string) }); ok {
			h.EvalMicList(mics)
		}
	case "speaker_toggle":
		off := argStr == "1"
		host.SetSpeakerOff(off)
		host.SyncState()
	case "km_ui":
		// 键位映射 UI 状态拉取(侧边栏「键位」tab 打开时)
		if h, ok := host.(interface{ EvalKeymapUI() }); ok {
			h.EvalKeymapUI()
		}
	case "km_enable":
		if a.Keymap != nil {
			a.Keymap.setEnabled(argStr == "1")
		}
		host.SyncState()
	case "km_hints":
		// 运行态按键提醒开关(雷电「在游戏中显示按键提醒」同款)
		if h, ok := host.(interface{ KeymapHintsToggle(bool) }); ok {
			h.KeymapHintsToggle(argStr == "1")
		}
	case "km_preset_new":
		if a.Keymap != nil {
			name, _ := msg["name"].(string) // 可空:引擎兜底「新方案」并自动编号
			a.Keymap.createPreset(name)
		}
		if h, ok := host.(interface{ EvalKeymapUI() }); ok {
			h.EvalKeymapUI()
		}
	case "km_preset_rename":
		if a.Keymap != nil {
			name, _ := msg["name"].(string)
			if name != "" && argStr != "" {
				a.Keymap.renamePreset(argStr, name)
				// 重命名同步到编辑中的工作副本,防「完成」保存时旧名覆盖回去
				if h, ok := host.(interface{ KeymapWorkRenamed(string, string) }); ok {
					h.KeymapWorkRenamed(argStr, name)
				}
			}
		}
		if h, ok := host.(interface{ EvalKeymapUI() }); ok {
			h.EvalKeymapUI()
		}
	case "km_preset_delete":
		if a.Keymap != nil {
			a.Keymap.deletePreset(argStr)
			// 清掉指向被删预设的编辑会话:工作副本持旧 ID,「完成」会把它重新写盘
			if h, ok := host.(interface{ KeymapPresetDeleted(string) }); ok {
				h.KeymapPresetDeleted(argStr)
			}
		}
		if h, ok := host.(interface{ EvalKeymapUI() }); ok {
			h.EvalKeymapUI()
		}
	case "km_preset_select":
		if a.Keymap != nil {
			a.Keymap.selectPreset(argStr)
		}
		if h, ok := host.(interface{ EvalKeymapUI() }); ok {
			h.EvalKeymapUI()
		}
	case "km_edit":
		// 进入/退出覆盖层编辑模式(退出时保存工作副本;其他平台 no-op)
		if h, ok := host.(interface{ KeymapSetEditing(bool) }); ok {
			h.KeymapSetEditing(argStr == "1")
		}
	case "km_cancel":
		// 取消编辑:丢弃工作副本,不保存
		if h, ok := host.(interface{ KeymapCancelEdit() }); ok {
			h.KeymapCancelEdit()
		}
	case "km_add":
		if h, ok := host.(interface{ KeymapAddEntry(string) }); ok {
			h.KeymapAddEntry(argStr)
		}
	case "km_sel":
		if h, ok := host.(interface{ KeymapSelect(string) }); ok {
			h.KeymapSelect(argStr)
		}
	case "km_del":
		if h, ok := host.(interface{ KeymapDeleteEntry(string) }); ok {
			h.KeymapDeleteEntry(argStr)
		}
	case "km_bind":
		if h, ok := host.(interface{ KeymapBindCode(string, int) }); ok {
			slot := -1
			if s, ok := msg["slot"].(float64); ok {
				slot = int(s)
			}
			h.KeymapBindCode(argStr, slot)
		}
	case "km_prop":
		if h, ok := host.(interface{ KeymapSetProp(string, string, string) }); ok {
			prop, _ := msg["prop"].(string)
			val, _ := msg["val"].(string)
			h.KeymapSetProp(argStr, prop, val)
		}
	case "km_save":
		// 编辑器保存工作副本
		if a.Keymap != nil {
			if data, err := json.Marshal(msg["profile"]); err == nil {
				var prof KeymapProfile
				if json.Unmarshal(data, &prof) == nil {
					a.Keymap.saveProfile(&prof)
				}
			}
		}
		if h, ok := host.(interface{ EvalKeymapUI() }); ok {
			h.EvalKeymapUI()
		}
	case "km_export":
		if a.Keymap != nil {
			if path := a.Keymap.exportCurrent(); path != "" {
				if h, ok := host.(interface{ KeymapExported(string) }); ok {
					h.KeymapExported(path)
				}
			}
		}
	case "km_import":
		if a.Keymap != nil {
			if data, ok := msg["data"].(string); ok && data != "" {
				a.Keymap.importFromJSON([]byte(data))
			}
		}
		if h, ok := host.(interface{ EvalKeymapUI() }); ok {
			h.EvalKeymapUI()
		}
	}
}

func (a *App) dispatchKeyboard(msg map[string]any, host MessageHost) {
	typ, _ := msg["type"].(string)
	key, _ := msg["key"].(string)
	isPressed := (typ == "down")

	sym := jsKeyToSDL(key)
	if sym == 0 && len(key) == 1 {
		sym = int(unicode.ToLower(rune(key[0])))
	}

	code, _ := msg["code"].(string)
	scancode := jsCodeToSDLScancode(code)

	mod := 0
	if ctrl, ok := msg["ctrlKey"].(bool); ok && ctrl {
		mod |= ModCtrl
	}
	if shift, ok := msg["shiftKey"].(bool); ok && shift {
		mod |= ModShift
	}
	if alt, ok := msg["altKey"].(bool); ok && alt {
		mod |= ModAlt
	}

	// ── 键位映射引擎:F9 开关与命中的映射键在此消费
	// (与 Win 路径同语义:带修饰键的按下由引擎内部直通)
	if a.Keymap != nil && code != "" {
		if a.Keymap.HandleCode(code, isPressed, mod) {
			return
		}
	}

	if sym == 0 && scancode == 0 {
		return
	}

	if isPressed && mod&ModCtrl != 0 && (key == "c" || key == "C") {
		go host.ClipboardCopy()
		return
	}
	if isPressed && mod&ModCtrl != 0 && (key == "x" || key == "X") {
		go host.ClipboardCut()
		return
	}
	if isPressed && mod&ModCtrl != 0 && (key == "v" || key == "V") {
		go func() {
			if text := host.ReadClipboardText(); text != "" {
				// 与 Win 原生路径同语义：群控有扇出目标时走广播，避免只贴主控。
				a.PasteClipboard(text)
			}
		}()
		return
	}
	if isPressed && mod&(ModCtrl|ModAlt) == 0 {
		cmdID := funcKeyCmdID(key)
		if cmdID > 0 {
			go a.SendCmd(CmdFuncKey, uint32(cmdID))
			return
		}
	}
	if isPressed && key == "Escape" {
		go a.SendCmd(CmdAndroidKey, KeyBack)
		return
	}
	go func(pressed bool, sc uint16, s int, m int, k string) {
		a.SendKeyboard(pressed, sc, int32(s), 0, uint16(m))
		if pressed && len(k) == 1 && m&(ModCtrl|ModAlt) == 0 {
			a.SendText(k)
		}
	}(isPressed, scancode, sym, mod, key)
}

// ─── State sync ───────────────────────────────────────────────────────────────

type appState struct {
	Clarity     int        `json:"clarity"`
	Topmost     bool       `json:"topmost"`
	Rotated     bool       `json:"rotated"`
	CtrlList    []ctrlItem `json:"ctrlList"`
	AllCtrlList []ctrlItem `json:"allCtrlList"`
	DeviceAddr  string     `json:"deviceAddr"`
	ConnState   string     `json:"connState"`
	SmsEnabled  bool       `json:"smsEnabled"`
	Controlled  bool       `json:"controlled"`
	PrimaryAddr string     `json:"primaryAddr"`
	SelfAddr     string     `json:"selfAddr"`
	CamAvailable bool       `json:"camAvailable"`
	CamRunning   bool       `json:"camRunning"`
	CamSource    string     `json:"camSource"` // 当前推流源："cam"/"screenDevice"/"screenDesktop"
	CamOtherInfo string     `json:"camOtherInfo"`
	SpeakerOff  bool       `json:"speakerOff"`
	KmEnabled   bool       `json:"kmEnabled"`
	KmEntries   int        `json:"kmEntries"`
}

func (a *App) stateJSON() string {
	clarity := atomic.LoadInt32(&a.Clarity)
	connState := atomic.LoadInt32(&a.ConnState)
	connRetry := atomic.LoadInt32(&a.ConnRetry)
	cs := ""
	if connState != ConnConnected {
		cs = connStateText(int(connState), int(connRetry))
	}
	devAddr := ""
	if h := a.rtspHost.Load(); h != nil && *h != "" {
		devAddr = *h
		if p := a.rtspPort.Load(); p != nil && *p != "" {
			devAddr += ":" + *p
		}
	}
	a.ctrlMu.Lock()
	ctrlList := make([]ctrlItem, len(a.CtrlItems))
	copy(ctrlList, a.CtrlItems)
	allCtrlList := make([]ctrlItem, len(a.AllCtrlItems))
	copy(allCtrlList, a.AllCtrlItems)
	primaryAddr := a.PrimaryAddr
	selfAddr := a.SelfAddr
	a.ctrlMu.Unlock()
	camAvail := a.CamPusher != nil && a.CamPusher.Available()
	camRun := false
	camOther := ""
	camSrc := ""
	if a.CamPusher != nil {
		camRun = a.CamPusher.IsRunning()
		camSrc = a.CamPusher.Source()
		isMine, isOther, info := a.CamPusher.SharedState()
		if !camRun && isOther {
			camOther = info
		}
		_ = isMine
	}
	kmEnabled := false
	kmEntries := 0
	if a.Keymap != nil {
		a.Keymap.SnapshotBinding(&kmEnabled, &kmEntries)
	}
	b, _ := json.Marshal(appState{
		Clarity:      int(clarity),
		Topmost:      a.Topmost.Load(),
		Rotated:      a.Rotated.Load(),
		CtrlList:     ctrlList,
		AllCtrlList:  allCtrlList,
		DeviceAddr:   devAddr,
		ConnState:    cs,
		SmsEnabled:   a.ApiPort > 0,
		Controlled:   a.Controlled,
		PrimaryAddr:  primaryAddr,
		SelfAddr:     selfAddr,
		CamAvailable: camAvail,
		CamRunning:   camRun,
		CamSource:    srcLabel(camSrc),
		CamOtherInfo: camOther,
		SpeakerOff:  a.SpeakerOff.Load(),
		KmEnabled:   kmEnabled,
		KmEntries:   kmEntries,
	})
	return string(b)
}
