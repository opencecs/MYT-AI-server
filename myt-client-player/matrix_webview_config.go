package main

import (
	"fmt"
	"net/url"
)

// MatrixInjectConfig 公网群控被控注入配置：被控收到 UDP 语义事件后，
// HTTP POST 到 myt-agent /api/screencast/inject，由 myt-agent 转发到被控安卓。
// 主控不需要此配置（主控本地操控走 player 页面原生 handler）。
type MatrixInjectConfig struct {
	BaseURL   string // http://<agent>/api/screencast/inject
	Token     string // Bearer token（被控登录态）
	DeviceID  string // 被控设备 id
	Container string // 被控容器名
	Index     int    // 被控坑位号
	SW        int    // 设备宽（竖屏 1080 / 横屏 1920）
	SH        int    // 设备高（竖屏 1920 / 横屏 1080）
}

// parseMatrixInjectConfig 从被控 player URL 解析注入配置。
// weburl 格式: http://<agent>/api/screencast/player?device_id=xxx&container=yyy&index=N&token=zzz
// agentBaseURL 取 URL scheme://host（注入接口同源同端口）。
func parseMatrixInjectConfig(weburl string, landscape bool) *MatrixInjectConfig {
	u, err := url.Parse(weburl)
	if err != nil {
		logf("[matrix-inject] parse weburl failed: %v", err)
		return nil
	}
	q := u.Query()
	devID := q.Get("device_id")
	container := q.Get("container")
	token := q.Get("token")
	if devID == "" || container == "" || token == "" {
		logf("[matrix-inject] weburl missing device_id/container/token")
		return nil
	}
	index := 0
	if idx := q.Get("index"); idx != "" {
		fmt.Sscanf(idx, "%d", &index)
	}
	sw, sh := 1080, 1920
	if landscape {
		sw, sh = 1920, 1080
	}
	baseURL := fmt.Sprintf("%s://%s/api/screencast/inject", u.Scheme, u.Host)
	logf("[matrix-inject] config: dev=%s container=%s index=%d sw=%d sh=%d base=%s",
		devID, container, index, sw, sh, baseURL)
	return &MatrixInjectConfig{
		BaseURL:   baseURL,
		Token:     token,
		DeviceID:  devID,
		Container: container,
		Index:     index,
		SW:        sw,
		SH:        sh,
	}
}
