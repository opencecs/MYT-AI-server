//go:build darwin

package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"strings"
	"time"
)

// ─── Clipboard ────────────────────────────────────────────────────────────

func (ui *DarwinUI) ClipboardCopy() { ui.copyOrCutSelection(true) }
func (ui *DarwinUI) ClipboardCut()  { ui.copyOrCutSelection(false) }

// copyOrCutSelection 执行 Ctrl+C/Ctrl+X：调设备 API 复制/剪切屏幕选中的文字，
// 再把返回的文本写入本地 macOS 剪贴板。copy=true 复制(cmd=3)，false 剪切(cmd=4)。
func (ui *DarwinUI) copyOrCutSelection(copy bool) {
	apiAddr := ui.app.DeviceApiAddr()
	if apiAddr == "" {
		return
	}
	var text string
	if copy {
		text = ui.app.copySelection(apiAddr)
	} else {
		text = ui.app.cutSelection(apiAddr)
	}
	if text == "" {
		return
	}
	darwinWriteClipboard(text)
}

func darwinReadClipboard() string {
	out, err := exec.Command("pbpaste").Output()
	if err != nil {
		return ""
	}
	return string(out)
}

func darwinWriteClipboard(text string) {
	cmd := exec.Command("pbcopy")
	cmd.Stdin = strings.NewReader(text)
	cmd.Run()
}

// ─── IP Location ───────────────────────────────────────────────────────────

func darwinSetIPLocation(app *App, ip, lang string) {
	host := app.DeviceHost()
	if host == "" {
		cEvalSidebarJS(`if(window.updateState)updateState({ipLocOK:false,ipLocErr:"未连接设备"})`)
		return
	}
	port := app.DeviceCtrlPort()
	apiURL := fmt.Sprintf("http://%s:%s/modifydev?cmd=11", host, port)
	if lang != "" {
		apiURL += "&launage=" + url.QueryEscape(lang)
	}
	if ip != "" {
		apiURL += "&ip=" + url.QueryEscape(ip)
	}
	resp, err := app.httpClient.Get(apiURL)
	if err != nil {
		cEvalSidebarJS(`if(window.updateState)updateState({ipLocOK:false,ipLocErr:"网络错误"})`)
		return
	}
	defer resp.Body.Close()
	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		cEvalSidebarJS(`if(window.updateState)updateState({ipLocOK:false,ipLocErr:"响应解析失败"})`)
		return
	}
	code, _ := result["code"].(float64)
	if code == 200 {
		cEvalSidebarJS(`if(window.updateState)updateState({ipLocOK:true})`)
	} else {
		errMsg, _ := result["error"].(string)
		if errMsg == "" {
			errMsg = "设置失败"
		}
		cEvalSidebarJS(fmt.Sprintf(`if(window.updateState)updateState({ipLocOK:false,ipLocErr:%q})`, errMsg))
	}
}

// ─── SMS ───────────────────────────────────────────────────────────────────

func darwinSendSMS(app *App, address, body string) {
	apiPort := app.ApiPort
	if apiPort <= 0 {
		cEvalSidebarJS(`if(window.updateState)updateState({smsOK:false,smsErr:"未配置API端口"})`)
		return
	}
	host := app.DeviceHost()
	if host == "" {
		cEvalSidebarJS(`if(window.updateState)updateState({smsOK:false,smsErr:"未连接设备"})`)
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
		cEvalSidebarJS(`if(window.updateState)updateState({smsOK:false,smsErr:"创建请求失败"})`)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.httpClient.Do(req)
	if err != nil {
		cEvalSidebarJS(`if(window.updateState)updateState({smsOK:false,smsErr:"网络错误"})`)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		cEvalSidebarJS(`if(window.updateState)updateState({smsOK:true})`)
	} else {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		cEvalSidebarJS(fmt.Sprintf(`if(window.updateState)updateState({smsOK:false,smsErr:"HTTP %d"})`, resp.StatusCode))
		logf("[mac] sms: status=%d body=%q\n", resp.StatusCode, respBody)
	}
}

// ─── Add Contact ───────────────────────────────────────────────────────────

func darwinAddContact(app *App, name, phone string) {
	host := app.DeviceHost()
	if host == "" {
		cEvalSidebarJS(`if(window.updateState)updateState({contactOK:false,contactErr:"未连接设备"})`)
		return
	}
	port := app.DeviceCtrlPort()
	contactJSON, _ := json.Marshal([]map[string]string{{"user": name, "tel": phone}})
	apiURL := fmt.Sprintf("http://%s:%s/addcontact?data=%s", host, port, url.QueryEscape(string(contactJSON)))
	resp, err := app.httpClient.Get(apiURL)
	if err != nil {
		cEvalSidebarJS(`if(window.updateState)updateState({contactOK:false,contactErr:"网络错误"})`)
		return
	}
	defer resp.Body.Close()
	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		cEvalSidebarJS(`if(window.updateState)updateState({contactOK:false,contactErr:"响应解析失败"})`)
		return
	}
	code, _ := result["code"].(float64)
	if code == 200 {
		cEvalSidebarJS(`if(window.updateState)updateState({contactOK:true})`)
	} else {
		errMsg, _ := result["error"].(string)
		if errMsg == "" {
			errMsg = "添加失败"
		}
		cEvalSidebarJS(fmt.Sprintf(`if(window.updateState)updateState({contactOK:false,contactErr:%q})`, errMsg))
	}
}

// ─── File Upload ───────────────────────────────────────────────────────────
//
// 上传/安装的协议逻辑在平台无关的 app_upload.go（App.UploadAndInstall），
// 本段只负责：osascript 文件选择、base64 解码。
// 状态反馈全部走侧边栏 WebView（upload-status + 进度条），不再使用视频区 overlay。

func darwinUploadFile(app *App) {
	if app.ApiPort <= 0 {
		darwinUploadDone("提示", "需要 -apiport 参数", fmt.Errorf("未配置API端口"))
		return
	}
	if app.RTSPURL == "" {
		darwinUploadDone("提示", "未连接设备", fmt.Errorf("未连接设备"))
		return
	}

	script := `set theFile to (choose file with prompt "选择要上传的文件")
	return POSIX path of theFile`
	out, err := exec.Command("osascript", "-e", script).Output()
	if err != nil {
		return
	}
	filePath := strings.TrimSpace(string(out))
	if filePath == "" {
		return
	}
	darwinStartFileUpload(app, filePath)
}

func darwinStartFileUpload(app *App, filePath string) {
	if app.ApiPort <= 0 {
		darwinUploadDone("提示", "需要 -apiport 参数", fmt.Errorf("未配置API端口"))
		return
	}
	if app.RTSPURL == "" {
		darwinUploadDone("提示", "未连接设备", fmt.Errorf("未连接设备"))
		return
	}
	go app.UploadAndInstallPath(filePath, darwinUploadStatus)
}

// darwinUploadStatus 协议层状态回调 → 侧边栏 WebView（进度 + 终态）。
func darwinUploadStatus(text, detail string, pct int, done bool, err error) {
	if done {
		darwinUploadDone(text, detail, err)
		return
	}
	cEvalSidebarJS(fmt.Sprintf(`if(window.updateState)updateState({uploadFile:%q,uploadPct:%d,uploadDetail:%q})`, text, pct, detail))
}

// darwinUploadDone 终态推侧边栏。
func darwinUploadDone(text, detail string, err error) {
	if err == nil {
		cEvalSidebarJS(fmt.Sprintf(`if(window.updateState)updateState({uploadFile:%q,uploadOK:true})`, text))
	} else {
		cEvalSidebarJS(fmt.Sprintf(`if(window.updateState)updateState({uploadFile:%q,uploadOK:false,uploadErr:%q})`, text, detail))
	}
}

func darwinUploadFileBase64(app *App, name, b64 string) {
	if app.ApiPort <= 0 {
		darwinUploadDone(name, "需要 -apiport 参数", fmt.Errorf("未配置API端口"))
		return
	}
	if app.RTSPURL == "" {
		darwinUploadDone(name, "未连接设备", fmt.Errorf("未连接设备"))
		return
	}
	go func() {
		data, err := base64.StdEncoding.DecodeString(b64)
		if err != nil {
			darwinUploadDone(name, "文件数据解码失败", err)
			return
		}
		app.UploadAndInstall(name, data, darwinUploadStatus)
	}()
}

