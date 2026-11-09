//go:build linux

package main

// ui_linux_api.go — MessageHost 设备 API 类方法（W5）：IP 定位/短信/联系人/上传。
// 语义对照 ui_darwin_api.go:59-238（同 HTTP 端点与回推 JS）与 ui_windows_upload.go。
// 上传协议逻辑在平台无关的 app_upload.go（App.UploadAndInstall/Path）。

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

// SetIPLocation IP 定位（对照 ui_darwin_api.go:59-94 同语义移植）。
func (ui *LinuxUI) SetIPLocation(ip, lang string) {
	app := ui.app
	host := app.DeviceHost()
	if host == "" {
		ui.evalPanelJS(`if(window.updateState)updateState({ipLocOK:false,ipLocErr:"未连接设备"})`)
		return
	}
	apiURL := fmt.Sprintf("http://%s:%s/modifydev?cmd=11", host, app.DeviceCtrlPort())
	if lang != "" {
		apiURL += "&launage=" + url.QueryEscape(lang)
	}
	if ip != "" {
		apiURL += "&ip=" + url.QueryEscape(ip)
	}
	resp, err := app.httpClient.Get(apiURL)
	if err != nil {
		ui.evalPanelJS(`if(window.updateState)updateState({ipLocOK:false,ipLocErr:"网络错误"})`)
		return
	}
	defer resp.Body.Close()
	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		ui.evalPanelJS(`if(window.updateState)updateState({ipLocOK:false,ipLocErr:"响应解析失败"})`)
		return
	}
	code, _ := result["code"].(float64)
	if code == 200 {
		ui.evalPanelJS(`if(window.updateState)updateState({ipLocOK:true})`)
	} else {
		errMsg, _ := result["error"].(string)
		if errMsg == "" {
			errMsg = "设置失败"
		}
		ui.evalPanelJS(fmt.Sprintf(`if(window.updateState)updateState({ipLocOK:false,ipLocErr:%q})`, errMsg))
	}
}

// SendSMS 发送短信（对照 ui_darwin_api.go:98-138 同语义移植）。
func (ui *LinuxUI) SendSMS(address, body string) {
	app := ui.app
	if app.ApiPort <= 0 {
		ui.evalPanelJS(`if(window.updateState)updateState({smsOK:false,smsErr:"未配置API端口"})`)
		return
	}
	host := app.DeviceHost()
	if host == "" {
		ui.evalPanelJS(`if(window.updateState)updateState({smsOK:false,smsErr:"未连接设备"})`)
		return
	}
	smsURL := fmt.Sprintf("http://%s:%d/sms?cmd=4", host, app.ApiPort)
	payload, _ := json.Marshal(map[string]string{
		"address":   address,
		"body":      body,
		"scaddress": "+8613800000000",
	})
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "POST", smsURL, bytes.NewReader(payload))
	if err != nil {
		ui.evalPanelJS(`if(window.updateState)updateState({smsOK:false,smsErr:"创建请求失败"})`)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.httpClient.Do(req)
	if err != nil {
		ui.evalPanelJS(`if(window.updateState)updateState({smsOK:false,smsErr:"网络错误"})`)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		ui.evalPanelJS(`if(window.updateState)updateState({smsOK:true})`)
	} else {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		ui.evalPanelJS(fmt.Sprintf(`if(window.updateState)updateState({smsOK:false,smsErr:"HTTP %d"})`, resp.StatusCode))
		logf("[linux] sms: status=%d body=%q\n", resp.StatusCode, respBody)
	}
}

// AddContact 添加联系人（对照 ui_darwin_api.go:140-176 同语义移植）。
func (ui *LinuxUI) AddContact(name, phone string) {
	app := ui.app
	host := app.DeviceHost()
	if host == "" {
		ui.evalPanelJS(`if(window.updateState)updateState({contactOK:false,contactErr:"未连接设备"})`)
		return
	}
	contactJSON, _ := json.Marshal([]map[string]string{{"user": name, "tel": phone}})
	apiURL := fmt.Sprintf("http://%s:%s/addcontact?data=%s", host, app.DeviceCtrlPort(), url.QueryEscape(string(contactJSON)))
	resp, err := app.httpClient.Get(apiURL)
	if err != nil {
		ui.evalPanelJS(`if(window.updateState)updateState({contactOK:false,contactErr:"网络错误"})`)
		return
	}
	defer resp.Body.Close()
	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		ui.evalPanelJS(`if(window.updateState)updateState({contactOK:false,contactErr:"响应解析失败"})`)
		return
	}
	code, _ := result["code"].(float64)
	if code == 200 {
		ui.evalPanelJS(`if(window.updateState)updateState({contactOK:true})`)
	} else {
		errMsg, _ := result["error"].(string)
		if errMsg == "" {
			errMsg = "添加失败"
		}
		ui.evalPanelJS(fmt.Sprintf(`if(window.updateState)updateState({contactOK:false,contactErr:%q})`, errMsg))
	}
}

// UploadFile 文件选择 + 上传安装（对照 ui_windows_upload.go:83 + darwinStartFileUpload：
// 协议逻辑在平台无关的 app_upload.go；xclip 式文件选择用 zenity，未装则提示）。
func (ui *LinuxUI) UploadFile() {
	app := ui.app
	if app.ApiPort <= 0 {
		ui.evalPanelJS(`if(window.updateState)updateState({uploadFile:"提示",uploadOK:false,uploadErr:"需要 -apiport 参数"})`)
		return
	}
	if app.RTSPURL == "" {
		ui.evalPanelJS(`if(window.updateState)updateState({uploadFile:"提示",uploadOK:false,uploadErr:"未连接设备"})`)
		return
	}
	// zenity 文件选择（GTK 环境标准配套；未安装静默失败）
	out, err := exec.Command("zenity", "--file-selection", "--title=选择要上传的文件").Output()
	if err != nil {
		return // 用户取消或未安装 zenity
	}
	filePath := strings.TrimSpace(string(out))
	if filePath == "" {
		return
	}
	go app.UploadAndInstallPath(filePath, ui.uploadStatusToPanel)
}

// UploadFileBase64 base64 数据上传（对照 ui_windows_upload.go:122-139）。
func (ui *LinuxUI) UploadFileBase64(name, b64 string) {
	app := ui.app
	if app.ApiPort <= 0 {
		ui.evalPanelJS(fmt.Sprintf(`if(window.updateState)updateState({uploadFile:%q,uploadOK:false,uploadErr:"需要 -apiport 参数"})`, name))
		return
	}
	if app.RTSPURL == "" {
		ui.evalPanelJS(fmt.Sprintf(`if(window.updateState)updateState({uploadFile:%q,uploadOK:false,uploadErr:"未连接设备"})`, name))
		return
	}
	go func() {
		data, err := base64.StdEncoding.DecodeString(b64)
		if err != nil {
			ui.evalPanelJS(fmt.Sprintf(`if(window.updateState)updateState({uploadFile:%q,uploadOK:false,uploadErr:"文件数据解码失败"})`, name))
			return
		}
		app.UploadAndInstall(name, data, ui.uploadStatusToPanel)
	}()
}

// uploadStatusToPanel 上传进度/终态回推（对照 darwinUploadStatus，ui_darwin_api.go:217-229）。
func (ui *LinuxUI) uploadStatusToPanel(text, detail string, pct int, done bool, err error) {
	if done {
		if err == nil {
			ui.evalPanelJS(fmt.Sprintf(`if(window.updateState)updateState({uploadFile:%q,uploadOK:true})`, text))
		} else {
			ui.evalPanelJS(fmt.Sprintf(`if(window.updateState)updateState({uploadFile:%q,uploadOK:false,uploadErr:%q})`, text, detail))
		}
		return
	}
	ui.evalPanelJS(fmt.Sprintf(`if(window.updateState)updateState({uploadFile:%q,uploadPct:%d,uploadDetail:%q})`, text, pct, detail))
}
