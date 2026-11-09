//go:build windows

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"syscall"
	"time"
	"unsafe"
)

func (ui *WindowsUI) ClipboardCopy() { ui.copyOrCutSelection(true) }
func (ui *WindowsUI) ClipboardCut()  { ui.copyOrCutSelection(false) }

// copyOrCutSelection 执行 Ctrl+C/Ctrl+X：调设备 API 复制/剪切屏幕选中的文字，
// 再把返回的文本写入本地 Windows 剪贴板。copy=true 复制(cmd=3)，false 剪切(cmd=4)。
func (ui *WindowsUI) copyOrCutSelection(copy bool) {
	apiAddr := ui.app.DeviceApiAddr()
	if apiAddr == "" {
		logf("[clip] copyOrCutSelection: apiAddr 为空，DeviceHost=%q ApiPort=%d\n", ui.app.DeviceHost(), ui.app.ApiPort)
		return
	}
	var text string
	if copy {
		text = ui.app.copySelection(apiAddr)
	} else {
		text = ui.app.cutSelection(apiAddr)
	}
	if text == "" {
		logf("[clip] copyOrCutSelection: %s 选中文字为空（屏幕上可能没有选中文本）apiAddr=%s\n", clipOpName(copy), apiAddr)
		return
	}
	logf("[clip] copyOrCutSelection: %s 拿到选中文字 len=%d %q\n", clipOpName(copy), len(text), truncForLog(text, 80))
	ui.writeClipboardText(text)
}

// clipOpName 仅用于日志可读性
func clipOpName(copy bool) string {
	if copy {
		return "复制"
	}
	return "剪切"
}

func (ui *WindowsUI) ReadClipboardText() string {
	ret, _, _ := procOpenClipboard.Call(0)
	if ret == 0 {
		return ""
	}
	defer procCloseClipboard.Call()
	h, _, _ := procGetClipboardData.Call(CF_UNICODETEXT)
	if h == 0 {
		return ""
	}
	ptr, _, _ := procGlobalLock.Call(h)
	if ptr == 0 {
		return ""
	}
	defer procGlobalUnlock.Call(h)
	maxCharsSz, _, _ := procGlobalSize.Call(h)
	maxChars := int(maxCharsSz) / 2
	u16slice := unsafe.Slice((*uint16)(unsafe.Pointer(ptr)), maxChars)
	n := 0
	for n < maxChars && u16slice[n] != 0 {
		n++
	}
	return syscall.UTF16ToString(u16slice[:n])
}

func (ui *WindowsUI) writeClipboardText(text string) {
	u16, _ := syscall.UTF16FromString(text)
	size := len(u16) * 2
	ret, _, _ := procOpenClipboard.Call(0)
	if ret == 0 {
		logf("[clip] writeClipboardText: OpenClipboard 失败（可能被其他程序占用）len=%d\n", len(text))
		return
	}
	defer procCloseClipboard.Call()
	procEmptyClipboard.Call()
	h, _, _ := procGlobalAlloc.Call(GMEM_MOVEABLE, uintptr(size))
	if h == 0 {
		logf("[clip] writeClipboardText: GlobalAlloc 失败 size=%d\n", size)
		return
	}
	ptr, _, _ := procGlobalLock.Call(h)
	if ptr == 0 {
		logf("[clip] writeClipboardText: GlobalLock 失败\n")
		procGlobalFree.Call(h)
		return
	}
	dst := unsafe.Slice((*uint16)(unsafe.Pointer(ptr)), len(u16))
	copy(dst, u16)
	procGlobalUnlock.Call(h)
	r2, _, _ := procSetClipboardData.Call(CF_UNICODETEXT, h)
	if r2 == 0 {
		logf("[clip] writeClipboardText: SetClipboardData 失败\n")
		return
	}
	logf("[clip] writeClipboardText: 写入本地剪贴板成功 len=%d\n", len(text))
}

func (ui *WindowsUI) SetIPLocation(ip, lang string) {
	host := ui.app.DeviceHost()
	if host == "" {
		ui.sendIPLocResult(false, "未连接设备")
		return
	}
	port := ui.app.DeviceCtrlPort()
	apiURL := fmt.Sprintf("http://%s:%s/modifydev?cmd=11", host, port)
	if lang != "" {
		apiURL += "&launage=" + url.QueryEscape(lang)
	}
	if ip != "" {
		apiURL += "&ip=" + url.QueryEscape(ip)
	}
	resp, err := ui.app.httpClient.Get(apiURL)
	if err != nil {
		ui.sendIPLocResult(false, "网络错误")
		return
	}
	defer resp.Body.Close()
	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		ui.sendIPLocResult(false, "响应解析失败")
		return
	}
	code, _ := result["code"].(float64)
	if code == 200 {
		ui.sendIPLocResult(true, "")
	} else {
		errMsg, _ := result["error"].(string)
		if errMsg == "" {
			errMsg = "设置失败"
		}
		ui.sendIPLocResult(false, errMsg)
	}
}

func (ui *WindowsUI) sendIPLocResult(ok bool, errMsg string) {
	var js string
	if ok {
		js = `if(window.updateState)updateState({ipLocOK:true})`
	} else {
		if errMsg == "" {
			errMsg = "设置失败"
		}
		js = fmt.Sprintf(`if(window.updateState)updateState({ipLocOK:false,ipLocErr:%q})`, errMsg)
	}
	ui.mu.Lock()
	ui.pendingJS = js
	ui.mu.Unlock()
	procPostMessageW.Call(uintptr(ui.hParent), WM_APP_IPLOC, 0, 0)
}

func (ui *WindowsUI) SendSMS(address, body string) {
	apiPort := ui.app.ApiPort
	if apiPort <= 0 {
		ui.sendSMSResult(false, "未配置API端口")
		return
	}
	host := ui.app.DeviceHost()
	if host == "" {
		ui.sendSMSResult(false, "未连接设备")
		return
	}
	smsURL := fmt.Sprintf("http://%s:%d/sms?cmd=4", host, apiPort)
	payload, _ := json.Marshal(map[string]string{
		"address":   address,
		"body":      body,
		"scaddress": "+8613800000000",
	})
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "POST", smsURL, bytes.NewReader(payload))
	if err != nil {
		ui.sendSMSResult(false, "创建请求失败")
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := ui.app.httpClient.Do(req)
	if err != nil {
		ui.sendSMSResult(false, "网络错误")
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		ui.sendSMSResult(true, "")
		return
	}
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
	logf("[sms] status=%d body=%q\n", resp.StatusCode, respBody)
	var result map[string]any
	if json.Unmarshal(respBody, &result) == nil {
		errMsg, _ := result["error"].(string)
		if errMsg == "" {
			errMsg = "发送失败"
		}
		ui.sendSMSResult(false, errMsg)
	} else {
		ui.sendSMSResult(false, fmt.Sprintf("HTTP %d", resp.StatusCode))
	}
}

func (ui *WindowsUI) sendSMSResult(ok bool, errMsg string) {
	var js string
	if ok {
		js = `if(window.updateState)updateState({smsOK:true})`
	} else {
		if errMsg == "" {
			errMsg = "发送失败"
		}
		js = fmt.Sprintf(`if(window.updateState)updateState({smsOK:false,smsErr:%q})`, errMsg)
	}
	ui.mu.Lock()
	ui.smsResultJS = js
	ui.mu.Unlock()
	procPostMessageW.Call(uintptr(ui.hParent), WM_APP_SMS_RESULT, 0, 0)
}

func (ui *WindowsUI) AddContact(name, phone string) {
	host := ui.app.DeviceHost()
	if host == "" {
		ui.sendContactResult(false, "未连接设备")
		return
	}
	port := ui.app.DeviceCtrlPort()
	contactJSON, _ := json.Marshal([]map[string]string{{"user": name, "tel": phone}})
	apiURL := fmt.Sprintf("http://%s:%s/addcontact?data=%s", host, port, url.QueryEscape(string(contactJSON)))
	resp, err := ui.app.httpClient.Get(apiURL)
	if err != nil {
		ui.sendContactResult(false, "网络错误")
		return
	}
	defer resp.Body.Close()
	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		ui.sendContactResult(false, "响应解析失败")
		return
	}
	code, _ := result["code"].(float64)
	if code == 200 {
		ui.sendContactResult(true, "")
	} else {
		errMsg, _ := result["error"].(string)
		if errMsg == "" {
			errMsg = "添加失败"
		}
		ui.sendContactResult(false, errMsg)
	}
}

func (ui *WindowsUI) sendContactResult(ok bool, errMsg string) {
	var js string
	if ok {
		js = `if(window.updateState)updateState({contactOK:true})`
	} else {
		if errMsg == "" {
			errMsg = "添加失败"
		}
		js = fmt.Sprintf(`if(window.updateState)updateState({contactOK:false,contactErr:%q})`, errMsg)
	}
	ui.mu.Lock()
	ui.contactResultJS = js
	ui.mu.Unlock()
	procPostMessageW.Call(uintptr(ui.hParent), WM_APP_CONTACT_RESULT, 0, 0)
}
