package main

import (
	"log"
	"sync"

	"github.com/moyunteng/myt/internal/config"
	"github.com/moyunteng/myt/internal/util"
)

// --- V4 剪贴板双向链路 ---
//
// 两条路径：
//  1. 本机→云机（主动）：前端右键「粘贴本机剪贴板」→ navigator.clipboard.readText()
//     → SetAndroidClipboard（POST /android/clipboard，paste=true 直贴焦点输入框）
//  2. 云机→本机（回灌）：WS clipboard/push 事件 → 防回声比对 → 写系统剪贴板
//     （Wails Clipboard.SetText，vendor 跨平台实现）→ 推 v4:clipboard 事件 toast
//
// 范围约定（与 V3 对齐——V3 客户端没有自动剪贴板同步，只有设备端 mytos API 的
// 手动设置）：不做自动双向同步，仅上述两条显式路径。

// v4ClipboardEcho 防回声记录：本机主动 set 过的最近文本（按 主机/实例键控）。
// WS 回灌推来的文本与记录一致时忽略——那是我们自己刚写进去的，不是云机侧新复制。
type v4ClipboardEcho struct {
	mu   sync.Mutex
	last map[string]string
}

func newV4ClipboardEcho() *v4ClipboardEcho {
	return &v4ClipboardEcho{last: make(map[string]string)}
}

// remember 记录主动设置的文本
func (e *v4ClipboardEcho) remember(hostIP, instanceKey, text string) {
	e.mu.Lock()
	e.last[hostIP+"/"+instanceKey] = text
	e.mu.Unlock()
}

// isEcho 判断推送文本是否为本机刚设置的（命中则顺带清除记录——同一文本只挡一次，
// 云机侧再次复制同样内容仍应回灌）
func (e *v4ClipboardEcho) isEcho(hostIP, instanceKey, text string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	key := hostIP + "/" + instanceKey
	if prev, ok := e.last[key]; ok && prev == text {
		delete(e.last, key)
		return true
	}
	return false
}

// onV4ClipboardPush WS clipboard/push 回灌入口（v4EventAdapter.OnClipboard 调用）：
// 防回声比对 → 异步写系统剪贴板 → 推 v4:clipboard 事件（前端 toast 提示）。
func onV4ClipboardPush(svc *SdkApiServiceWrapper, hostIP, instanceKey, text string) {
	if svc == nil || text == "" {
		return
	}
	if svc.v4Clip != nil && svc.v4Clip.isEcho(hostIP, instanceKey, text) {
		log.Printf("[v4clip] 主机 %s 实例 %s 剪贴板推送为本机回声，忽略", hostIP, instanceKey)
		return
	}
	if svc.app == nil || svc.app.Clipboard == nil {
		return
	}
	// 写剪贴板是 Win32 调用（内部有锁等待），不阻塞 WS 消费 goroutine
	textCopy := text
	util.GoSafe(func() {
		if ok := svc.app.Clipboard.SetText(textCopy); !ok {
			log.Printf("[v4clip] 主机 %s 实例 %s 写系统剪贴板失败", hostIP, instanceKey)
			return
		}
		log.Printf("[v4clip] 主机 %s 实例 %s 剪贴板已回灌到本机（%d 字符）", hostIP, instanceKey, len([]rune(textCopy)))
		svc.emitEvent("v4:clipboard", map[string]string{
			"host": hostIP, "instance": instanceKey, "text": textCopy,
		})
	})
}

// SetAndroidClipboard 设置云机剪贴板（前端右键「粘贴本机剪贴板」调用）。
// V4 分支见 sdkapi_service_droid_v4.go；V3/local/public/v2 走设备端 mytos API
// （历史行为保持不变）。
func (s *SdkApiServiceWrapper) SetAndroidClipboard(deviceIP, androidName, text string, paste bool) error {
	if s.isV4Mode() {
		return s.setAndroidClipboardV4(deviceIP, androidName, text, paste)
	}
	// V3 等模式：设备端 SetClipboard（/clipboard?cmd=2，无 paste 语义，仅落剪贴板）
	client, err := s.getMytosClientForAndroid(deviceIP, androidName)
	if err != nil {
		return err
	}
	if err := client.SetClipboard(text); err != nil {
		return err
	}
	log.Printf("[sdkapi] 设置云机剪贴板成功: %s@%s %d 字符", androidName, deviceIP, len([]rune(text)))
	return nil
}

// GetAndroidProfile 读云机设备档案（弹窗预填）。V4：WS snapshot 落缓存的
// config.AndroidInfo.Profile；其他模式无此概念（nil）。
func (s *SdkApiServiceWrapper) GetAndroidProfile(deviceIP, androidName string) (*config.AndroidProfile, error) {
	if s.isV4Mode() {
		return s.getAndroidProfileV4(deviceIP, androidName)
	}
	return nil, nil
}

// ConfigureAndroidProfile 设置云机设备档案（V4 POST /android/configure）。
func (s *SdkApiServiceWrapper) ConfigureAndroidProfile(deviceIP, androidName string, profile *config.AndroidProfile) error {
	if s.isV4Mode() {
		return s.configureAndroidProfileV4(deviceIP, androidName, profile)
	}
	return v4NotSupported("设备档案")
}
