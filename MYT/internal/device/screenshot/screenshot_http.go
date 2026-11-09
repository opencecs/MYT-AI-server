package screenshot

import (
	"fmt"
	"net/http"
	"strings"
)

// 截图二进制 HTTP 通道（asset server 服务路由）。
//
// ScreenshotServiceWrapper 注册为 Wails Service 时带 Route（/screenshots），
// Wails 把本 Handler 挂到内部 asset server；WebView2 端经虚拟 host
// http://wails.localhost/screenshots/... 访问（processRequest 按 host 前缀
// wails.localhost 拦截，非真实 TCP 监听，无端口暴露面）。
// 实现 http.Handler 接口（服务路由的准入要求，见 Wails startupService）。
//
// 前端 img src 直连该 URL（带 ?v=版本号 失效控制），彻底绕开 base64 dataURL
// 过 IPC：省 1.33x 膨胀与字符串拷贝，浏览器按 URL 缓存/解码管理。
// 版本号由 GetScreenshotVersions 轮询下发，与数据通道（GetScreenshots dataURL，
// 保留作为回落）保持同一 versionRef 口径。

// ServeHTTP 响应 GET /{key}?v=版本号：key 形如 "192.168.1.10_1"。
// 命中缓存写 image/* 字节流（含 ETag/immutable 缓存头：URL 带 v，内容不可变）；
// 未命中 404（前端按缺图回落处理）。仅允许 GET。
func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	// 路由前缀（/screenshots）已由 asset server 剥离，余下即 key。
	key := strings.TrimPrefix(r.URL.Path, "/")
	// URL 里含 IP/device_id，不含敏感凭据；v 参数仅用于缓存失效
	if key == "" {
		http.NotFound(w, r)
		return
	}
	s.mu.RLock()
	item := s.cache[key]
	if item == nil || item.rawData == nil {
		s.mu.RUnlock()
		http.NotFound(w, r)
		return
	}
	img := item.rawData
	typ := item.contentType
	s.mu.RUnlock()

	w.Header().Set("Content-Type", typ)
	w.Header().Set("Cache-Control", "private, immutable, max-age=31536000")
	w.Header().Set("ETag", fmt.Sprintf(`"%d"`, item.Version))
	_, _ = w.Write(img)
}
