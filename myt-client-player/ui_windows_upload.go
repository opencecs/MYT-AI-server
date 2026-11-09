//go:build windows

package main

import (
	"encoding/base64"
	"fmt"
	"unsafe"
	"unicode/utf16"

	"golang.org/x/sys/windows"
)

// ─── File upload（Windows UI 层） ─────────────────────────────────────────────
//
// 上传/安装的协议逻辑在平台无关的 app_upload.go（App.UploadAndInstall），
// 本文件只负责：Win32 文件选择对话框、拖拽、base64 消息解码。
// 状态反馈全部走侧边栏 WebView（upload-status 文本 + upload-progress 进度条，
// 经 uploadJS 队列转 UI 线程 Eval），不再使用视频区 overlay 浮窗。

var (
	comdlg32             = windows.NewLazyDLL("comdlg32.dll")
	procGetOpenFileNameW = comdlg32.NewProc("GetOpenFileNameW")
)

type openFileName struct {
	lStructSize       uint32
	hwndOwner         uintptr
	hInstance         uintptr
	lpstrFilter       *uint16
	lpstrCustomFilter *uint16
	nMaxCustFilter    uint32
	nFilterIndex      uint32
	lpstrFile         *uint16
	nMaxFile          uint32
	lpstrFileTitle    *uint16
	nMaxFileTitle     uint32
	lpstrInitialDir   *uint16
	lpstrTitle        *uint16
	flags             uint32
	nFileOffset       uint16
	nFileExtension    uint16
	lpstrDefExt       *uint16
	lCustData         uintptr
	lpfnHook          uintptr
	lpTemplateName    *uint16
	pvReserved        uintptr
	dwReserved        uint32
	flagsEx           uint32
}

const (
	ofnFileMustExist = 0x00001000
	ofnNoChangeDir   = 0x00000008
)

// uploadStatusToSidebar 协议层状态回调 → 侧边栏 WebView。
// 进度消息 uploadPct/uploadDetail；终态 uploadOK/uploadErr。
// 每次覆盖 uploadJS 再 PostMessage——高频进度下消息合并，UI 线程取最新值。
func (ui *WindowsUI) uploadStatusToSidebar(text, detail string, pct int, done bool, err error) {
	ui.pushUploadJS(uploadStateJS(text, detail, pct, done, err))
}

// pushUploadJS 写入待执行 JS 并投递到 UI 线程（WM_APP_UPLOAD_SIDEBAR 消费）。
func (ui *WindowsUI) pushUploadJS(js string) {
	ui.mu.Lock()
	ui.uploadJS = js
	ui.mu.Unlock()
	procPostMessageW.Call(uintptr(ui.hParent), WM_APP_UPLOAD_SIDEBAR, 0, 0)
}

// uploadStateJS 组装侧边栏 updateState 消息（upload_file cmd 路径的文件名也带上）。
func uploadStateJS(text, detail string, pct int, done bool, err error) string {
	if done {
		if err == nil {
			return fmt.Sprintf(`if(window.updateState)updateState({uploadFile:%q,uploadOK:true})`, text)
		}
		return fmt.Sprintf(`if(window.updateState)updateState({uploadFile:%q,uploadOK:false,uploadErr:%q})`, text, detail)
	}
	return fmt.Sprintf(`if(window.updateState)updateState({uploadFile:%q,uploadPct:%d,uploadDetail:%q})`, text, pct, detail)
}

func (ui *WindowsUI) UploadFile() {
	if ui.app.ApiPort <= 0 {
		ui.pushUploadJS(uploadStateJS("提示", "需要 -apiport 参数", 0, true, fmt.Errorf("未配置API端口")))
		return
	}
	if ui.app.RTSPURL == "" {
		ui.pushUploadJS(uploadStateJS("提示", "未连接设备", 0, true, fmt.Errorf("未连接设备")))
		return
	}
	procPostMessageW.Call(uintptr(ui.hParent), WM_APP_UPLOAD, 0, 0)
}

func (ui *WindowsUI) handleDropFiles(hDrop uintptr) {
	index := uint32(0)
	len, _, _ := procDragQueryFileW.Call(hDrop, uintptr(index), 0, 0)
	if len == 0 {
		procDragFinish.Call(hDrop)
		return
	}
	buf := make([]uint16, len+1)
	procDragQueryFileW.Call(hDrop, uintptr(index), uintptr(unsafe.Pointer(&buf[0])), uintptr(len+1))
	procDragFinish.Call(hDrop)
	filePath := windows.UTF16PtrToString(&buf[0])
	ui.startFileUpload(filePath)
}

func (ui *WindowsUI) startFileUpload(filePath string) {
	if ui.app.ApiPort <= 0 {
		ui.pushUploadJS(uploadStateJS("提示", "需要 -apiport 参数", 0, true, fmt.Errorf("未配置API端口")))
		return
	}
	if ui.app.RTSPURL == "" {
		ui.pushUploadJS(uploadStateJS("提示", "未连接设备", 0, true, fmt.Errorf("未连接设备")))
		return
	}
	go ui.app.UploadAndInstallPath(filePath, ui.uploadStatusToSidebar)
}

// UploadFileBase64 侧边栏 WebView 上传（拖拽 FileReader base64，≤200MB）。
func (ui *WindowsUI) UploadFileBase64(name, b64 string) {
	if ui.app.ApiPort <= 0 {
		ui.pushUploadJS(uploadStateJS(name, "需要 -apiport 参数", 0, true, fmt.Errorf("未配置API端口")))
		return
	}
	if ui.app.RTSPURL == "" {
		ui.pushUploadJS(uploadStateJS(name, "未连接设备", 0, true, fmt.Errorf("未连接设备")))
		return
	}
	go func() {
		data, err := base64.StdEncoding.DecodeString(b64)
		if err != nil {
			ui.pushUploadJS(uploadStateJS(name, "文件数据解码失败", 0, true, err))
			return
		}
		ui.app.UploadAndInstall(name, data, ui.uploadStatusToSidebar)
	}()
}

func (ui *WindowsUI) handleUploadFilePick() {
	var fileBuf [1024]uint16
	filterBuf := utf16.Encode([]rune("All Files"))
	filterBuf = append(filterBuf, 0)
	filterBuf = append(filterBuf, utf16.Encode([]rune("*.*"))...)
	filterBuf = append(filterBuf, 0, 0)
	ofn := openFileName{
		lStructSize: uint32(unsafe.Sizeof(openFileName{})),
		hwndOwner:   uintptr(ui.hParent),
		lpstrFilter: &filterBuf[0],
		lpstrFile:   &fileBuf[0],
		nMaxFile:    uint32(len(fileBuf)),
		lpstrTitle:  windows.StringToUTF16Ptr("选择要上传的文件"),
		flags:       ofnFileMustExist | ofnNoChangeDir,
	}
	ret, _, _ := procGetOpenFileNameW.Call(uintptr(unsafe.Pointer(&ofn)))
	if ret == 0 {
		return
	}
	filePath := windows.UTF16PtrToString(&fileBuf[0])
	logf("[upload] selected: %s\n", filePath)
	ui.startFileUpload(filePath)
}
