package main

import "net/http"

// ScreenshotServiceWrapper 的 http.Handler 委托。
//
// 根因：setup.go 注册带 Route="/screenshots" 的是外层 wrapper，而 ServeHTTP
// 定义在内层 *screenshot.Service 上——wrapper 未实现 http.Handler，Wails
// startupService（vendor application.go）检测失败后挂载固定 503 的 fallback
// handler（"Service does not handle HTTP requests"），导致
// GET http://wails.localhost/screenshots/{key}?v=... 全部 503。
//
// Wails bindings 的 internalServiceMethods 显式排除 ServeHTTP，不会被注册为
// 前端可调用方法；asset server 匹配 Route 后剥离前缀再调用本方法，key 解析
// 逻辑见 internal/device/screenshot/screenshot_http.go。
func (s *ScreenshotServiceWrapper) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if s.service == nil {
		http.Error(w, "screenshot service not initialized", http.StatusServiceUnavailable)
		return
	}
	s.service.ServeHTTP(w, r)
}
