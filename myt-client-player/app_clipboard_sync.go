package main

import (
	"encoding/json"
	"io"
	"net/url"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

const clipSyncInterval = 2 * time.Second

// startClipboardSync 启动剪贴板同步轮询
func (a *App) startClipboardSync() {
	a.stopClipboardSync()
	ch := make(chan struct{})
	a.clipStopCh = ch
	go a.clipSyncLoop(ch)
}

func (a *App) stopClipboardSync() {
	if ch := a.clipStopCh; ch != nil {
		close(ch)
		a.clipStopCh = nil
	}
}

func (a *App) clipSyncLoop(stopCh chan struct{}) {
	// 等待首次连接稳定
	select {
	case <-time.After(3 * time.Second):
	case <-stopCh:
		return
	case <-a.shutdownCh:
		return
	}

	var lastText string
	ticker := time.NewTicker(clipSyncInterval)
	defer ticker.Stop()

	for {
		select {
		case <-stopCh:
			return
		case <-a.shutdownCh:
			return
		case <-ticker.C:
		}

		if !a.Connected.Load() || a.ClipboardWriter == nil {
			continue
		}

		apiAddr := a.DeviceApiAddr()
		if apiAddr == "" {
			continue
		}

		text := a.fetchDeviceClipboard(apiAddr)
		if text == "" || text == lastText {
			continue
		}

		lastText = text
		a.ClipboardWriter(text)
	}
}

// clipFetchInFlight 轮询抓取在飞标记（仅后台轮询使用，CAS 抢独占）。
var clipFetchInFlight atomic.Int32

// clipUserMu 用户手动剪贴板操作（Ctrl+C/X/V）串行锁。
// 用锁而非"在飞就跳过"：Ctrl+C 未返回时按 Ctrl+V 必须排队执行，
// 丢弃正是"复制后粘贴无效"的表现之一。
var clipUserMu sync.Mutex

// truncForLog 截断长文本用于日志，避免刷屏
func truncForLog(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "...(truncated)"
}

func (a *App) fetchDeviceClipboard(apiAddr string) string {
	// 后台轮询：用户手动操作在飞时让位（不抢设备 HTTP 接口），
	// 但不反过来阻塞用户操作——单一计数会让 Ctrl+C/V 被轮询静默吞掉。
	if !clipUserMu.TryLock() {
		logf("[clip] fetchDeviceClipboard: 跳过（用户剪贴板操作在飞）\n")
		return ""
	}
	defer clipUserMu.Unlock()

	if !clipFetchInFlight.CompareAndSwap(0, 1) {
		logf("[clip] fetchDeviceClipboard: 跳过（上一轮轮询未完成）\n")
		return ""
	}
	defer clipFetchInFlight.Add(-1)

	apiURL := "http://" + apiAddr + "/clipboard"
	resp, err := a.httpClient.Get(apiURL)
	if err != nil {
		logf("[clip] fetchDeviceClipboard: HTTP 请求失败 url=%s err=%v\n", apiURL, err)
		return ""
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		logf("[clip] fetchDeviceClipboard: 读响应体失败 url=%s err=%v\n", apiURL, err)
		return ""
	}
	logf("[clip] fetchDeviceClipboard: url=%s status=%d bodyLen=%d body=%q\n", apiURL, resp.StatusCode, len(body), truncForLog(string(body), 200))

	if resp.StatusCode != 200 {
		return ""
	}

	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		logf("[clip] fetchDeviceClipboard: JSON 解析失败 err=%v\n", err)
		return ""
	}
	text := ""
	if data, ok := result["data"].(map[string]any); ok {
		text, _ = data["text"].(string)
	}
	if text == "" {
		text, _ = result["text"].(string)
	}
	logf("[clip] fetchDeviceClipboard: 解析出 text len=%d text=%q\n", len(text), truncForLog(text, 80))
	return text
}

// copySelection 调设备 API cmd=3 复制屏幕上选中的文字，返回选中文本。
// 前提：设备屏幕上有处于选中状态的文字（鼠标长按选中），否则返回空。
// 用于 Ctrl+C：让 Android 执行真正的复制并拿到内容写入本地剪贴板。
func (a *App) copySelection(apiAddr string) string {
	return a.clipboardCmd(apiAddr, 3, "")
}

// cutSelection 调设备 API cmd=4 剪切屏幕上选中的文字，返回选中文本。
// 剪切后选中文字从原位置删除。用于 Ctrl+X。
func (a *App) cutSelection(apiAddr string) string {
	return a.clipboardCmd(apiAddr, 4, "")
}

// pasteText 调设备 API cmd=5 把文本粘贴到设备当前聚焦的输入框。
// 前提：设备屏幕上有聚焦的输入框。用于 Ctrl+V。
func (a *App) pasteText(text string) {
	apiAddr := a.DeviceApiAddr()
	if apiAddr == "" {
		logf("[clip] pasteText: apiAddr 为空\n")
		return
	}
	a.clipboardCmd(apiAddr, 5, text) // cmd=5 不返回 text，仅状态码
}

// clipboardCmd 统一执行剪贴板命令（cmd=3 复制选中 / cmd=4 剪切选中 / cmd=5 粘贴）。
// cmd=3/4 返回 data.text；cmd=5 不返回 text。text 非空时用于 cmd=5 粘贴内容。
func (a *App) clipboardCmd(apiAddr string, cmd int, text string) string {
	// 用户显式操作（Ctrl+C/X/V）：与其它用户操作串行，不受后台轮询影响。
	// 用阻塞加锁而非跳过：连按 Ctrl+C 再 Ctrl+V 必须按序执行完，不能丢。
	clipUserMu.Lock()
	defer clipUserMu.Unlock()

	apiURL := "http://" + apiAddr + "/clipboard?cmd=" + strconv.Itoa(cmd)
	if cmd == 5 && text != "" {
		apiURL += "&text=" + url.QueryEscape(text)
	}
	resp, err := a.httpClient.Get(apiURL)
	if err != nil {
		logf("[clip] clipboardCmd: HTTP 请求失败 cmd=%d url=%s err=%v\n", cmd, apiURL, err)
		return ""
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		logf("[clip] clipboardCmd: 读响应体失败 cmd=%d err=%v\n", cmd, err)
		return ""
	}
	logf("[clip] clipboardCmd: cmd=%d url=%s status=%d body=%q\n", cmd, apiURL, resp.StatusCode, truncForLog(string(body), 200))
	if resp.StatusCode != 200 {
		return ""
	}
	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		logf("[clip] clipboardCmd: JSON 解析失败 cmd=%d err=%v\n", cmd, err)
		return ""
	}
	// code=200 才成功（文档：200 成功 / 201/202 失败）
	if code, ok := result["code"].(float64); !ok || int(code) != 200 {
		logf("[clip] clipboardCmd: 设备返回失败 cmd=%d result=%v\n", cmd, result)
		return ""
	}
	out := ""
	if data, ok := result["data"].(map[string]any); ok {
		out, _ = data["text"].(string)
	}
	return out
}
