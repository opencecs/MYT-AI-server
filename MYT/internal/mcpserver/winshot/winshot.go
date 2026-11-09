// Package winshot 提供 MCP 投屏业务工具：start_screencast / screenshot_window /
// close_screencast。
//
// 链路：容器标识（androidId / 容器名 / 设备IP_坑位 / 设备IP）→ resolveAndroid
// 归一化为运行中容器（meta）→ castKey（"ip_indexNum"）→
//   - start：透传 StartBatchIndividualProjections（UI「单击卡片→投屏」同款入口）
//   - screenshot：HTTP GET 播放器控制接口 /screenshot（409 纹理硬解瞬态自愈重试）
//   - close：HTTP POST 播放器控制接口 /close
package winshot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/moyunteng/myt/internal/config"
	"github.com/moyunteng/myt/internal/mcpserver/reg"
)

// Deps 工具依赖（setup.go 注入）。
type Deps struct {
	// RunningAndroids 运行中容器清单（标识解析数据源，来自 list_androids 同源）。
	RunningAndroids func() []config.AndroidInfo
	// RunningKeys 当前已打开播放器的投屏 key 列表（"ip_indexNum"）。
	RunningKeys func() []string
	// ControlPortOf 投屏 key → 播放器本地控制 HTTP 端口（0 = 不在投屏/未启用）。
	ControlPortOf func(key string) int
	// StartCast 发起投屏（透传 StartBatchIndividualProjections 单台路径）。
	StartCast func(deviceIP string, indexNum int) error
	// HTTPClient 可注入（测试用）；nil = 默认客户端。
	HTTPClient *http.Client
}

// controlTimeout 控制 HTTP 请求超时（截图取帧 + PNG 编码在播放器侧完成）。
const controlTimeout = 10 * time.Second

// shotAttempts/shotRetryInterval 纹理硬解 409 的自愈重试参数：播放器下一帧
// 可能就是 BGRA 软解帧，短暂等待后重试通常可自愈。
const (
	shotAttempts      = 3
	shotRetryInterval = 300 * time.Millisecond
)

// errTextureConflict 播放器 409（纹理硬解帧不可截取）的哨兵错误。
var errTextureConflict = errors.New("纹理硬解帧不可截取")

// client 返回 HTTP 客户端（带超时）。
func (d *Deps) client() *http.Client {
	if d.HTTPClient != nil {
		return d.HTTPClient
	}
	return &http.Client{Timeout: controlTimeout}
}

// notInCastErr 未在投屏的引导错误（列出当前投屏清单，引导 AI 改目标）。
func (d *Deps) notInCastErr(android string) error {
	keys := "（无）"
	if ks := d.RunningKeys(); len(ks) > 0 {
		keys = strings.Join(ks, ", ")
	}
	return fmt.Errorf("容器 %q 未在投屏中；当前投屏列表: %s（可先调 start_screencast 发起）", android, keys)
}

// castKey 容器的投屏 key（"ip_indexNum"，与 screencast.SlotRef.String 一致）。
func castKey(a config.AndroidInfo) string {
	return fmt.Sprintf("%s_%d", a.IP, a.IndexNum)
}

// runningList 运行中容器清单文本（androidId+name+坑位，报错引导用）。
func runningList(running []config.AndroidInfo) string {
	parts := make([]string, 0, len(running))
	for _, a := range running {
		parts = append(parts, fmt.Sprintf("%s(%s,%s_%d)", a.ID, a.Name, a.IP, a.IndexNum))
	}
	return strings.Join(parts, "; ")
}

// resolveAndroid 把业务标识归一化为运行中容器（三工具共用）：
// ① androidId 精确；② 容器名唯一命中（多个 → 报错列候选）；③
// "设备IP_坑位" key；④ 设备 IP（仅一台运行中直接开，多台 → 报错列清单，不猜）。
func (d *Deps) resolveAndroid(android string) (config.AndroidInfo, error) {
	running := d.RunningAndroids()
	if android == "" {
		return config.AndroidInfo{}, fmt.Errorf("android 参数不能为空；当前运行中: %s", runningList(running))
	}
	if len(running) == 0 {
		return config.AndroidInfo{}, errors.New("当前无运行中的云机，无法操作投屏（请先确认设备在线且容器已开机）")
	}
	// ① androidId 精确
	for _, a := range running {
		if a.ID == android {
			return a, nil
		}
	}
	// ② 容器名唯一命中
	var byName []config.AndroidInfo
	for _, a := range running {
		if a.Name == android {
			byName = append(byName, a)
		}
	}
	if len(byName) == 1 {
		return byName[0], nil
	}
	if len(byName) > 1 {
		return config.AndroidInfo{}, fmt.Errorf("容器名 %q 命中 %d 台运行中云机，请用 androidId 指定: %s",
			android, len(byName), runningList(byName))
	}
	// ③ "设备IP_坑位" key（IP 段 + "_" + 坑位号）
	if ip, idx, ok := strings.Cut(android, "_"); ok {
		for _, a := range running {
			if a.IP == ip && fmt.Sprintf("%d", a.IndexNum) == idx {
				return a, nil
			}
		}
	}
	// ④ 设备 IP：仅一台运行中直接开；多台 → 报错列清单，不猜
	var byIP []config.AndroidInfo
	for _, a := range running {
		if a.IP == android {
			byIP = append(byIP, a)
		}
	}
	if len(byIP) == 1 {
		return byIP[0], nil
	}
	if len(byIP) > 1 {
		return config.AndroidInfo{}, fmt.Errorf("设备 %q 有 %d 台运行中云机，请用 androidId / 容器名 / 设备IP_坑位 指定: %s",
			android, len(byIP), runningList(byIP))
	}
	return config.AndroidInfo{}, fmt.Errorf("未找到运行中的云机 %q；当前运行中: %s", android, runningList(running))
}

// controlRequest 调播放器控制接口，返回 body（HTTP 层错误已包装；
// 409 包装为 errTextureConflict）。
func (d *Deps) controlRequest(method, path string, port int) ([]byte, error) {
	url := fmt.Sprintf("http://127.0.0.1:%d%s", port, path)
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := d.client().Do(req)
	if err != nil {
		return nil, fmt.Errorf("连接播放器控制接口失败（播放器可能已退出）: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, fmt.Errorf("读取播放器响应失败: %w", err)
	}
	if resp.StatusCode == http.StatusConflict {
		var e struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(body, &e) == nil && e.Error != "" {
			return nil, fmt.Errorf("%w: %s", errTextureConflict, e.Error)
		}
		return nil, fmt.Errorf("%w（播放器返回 409）", errTextureConflict)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("播放器控制接口返回 %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return body, nil
}

// startCast 发起投屏：解析目标 → 已投屏幂等返回 → 透传 StartCast。
func (d *Deps) startCast(android string) (key string, already bool, err error) {
	meta, err := d.resolveAndroid(android)
	if err != nil {
		return "", false, err
	}
	key = castKey(meta)
	for _, k := range d.RunningKeys() {
		if k == key {
			return key, true, nil // 幂等：已在投屏，不重复拉起
		}
	}
	if d.StartCast == nil {
		return "", false, errors.New("发起投屏能力未注入")
	}
	if err := d.StartCast(meta.IP, meta.IndexNum); err != nil {
		return "", false, fmt.Errorf("发起投屏失败（设备离线/容器未运行/启动出错）: %w", err)
	}
	return key, false, nil
}

// capture 截图主体：解析 → 控制端口 → GET /screenshot（409 自愈重试）→ PNG。
func (d *Deps) capture(android string) (key string, png []byte, err error) {
	meta, err := d.resolveAndroid(android)
	if err != nil {
		return "", nil, err
	}
	key = castKey(meta)
	port := d.ControlPortOf(key)
	if port <= 0 {
		return "", nil, d.notInCastErr(android)
	}
	for attempt := 1; ; attempt++ {
		body, err := d.controlRequest(http.MethodGet, "/screenshot", port)
		if err == nil {
			return key, body, nil
		}
		if !errors.Is(err, errTextureConflict) || attempt >= shotAttempts {
			if errors.Is(err, errTextureConflict) {
				err = fmt.Errorf("%v；可改用 get_screenshot（设备端截图接口）", err)
			}
			return "", nil, err
		}
		time.Sleep(shotRetryInterval) // 播放器下一帧可能就是 BGRA 软解帧
	}
}

// closePlayer 关闭主体：解析 → 控制端口 → POST /close（播放器优雅退出）。
func (d *Deps) closePlayer(android string) (string, error) {
	meta, err := d.resolveAndroid(android)
	if err != nil {
		return "", err
	}
	key := castKey(meta)
	port := d.ControlPortOf(key)
	if port <= 0 {
		return "", d.notInCastErr(android)
	}
	if _, err := d.controlRequest(http.MethodPost, "/close", port); err != nil {
		return "", err
	}
	return key, nil
}

// screenshotArgs screenshot_window 参数。
type screenshotArgs struct {
	Android string `json:"android" jsonschema_desc:"容器标识：androidId / 容器名 / 设备IP_坑位 / 设备IP（任一形态）"`
}

// Register 注册投屏业务工具（server.go 装配；deps nil = 不注册）。
func Register(srv *mcp.Server, sink reg.Sink, deps *Deps) {
	if deps == nil {
		return
	}
	reg.Reg(srv, sink, "start_screencast",
		"对指定云机发起投屏（打开 go_legacy.exe 播放器窗口，与界面「单击卡片→投屏」同链路）。"+
			"已在投屏中则幂等返回 already=true。android 传容器标识（androidId / 容器名 / "+
			"设备IP_坑位 / 设备IP）。有副作用（拉起窗口）但非破坏性。",
		func(ctx context.Context, a screenshotArgs) (any, error) {
			key, already, err := deps.startCast(a.Android)
			if err != nil {
				return nil, err
			}
			return map[string]any{"result": "ok", "castKey": key, "already": already, "android": a.Android}, nil
		}, reg.Effectful())

	reg.RegRaw(srv, sink, "screenshot_window",
		"截取指定云机的投屏播放器画面（go_legacy.exe 独立进程，主界面 CDP 截不到；"+
			"经播放器本地控制接口直接取渲染帧，与窗口最小化/遮挡无关）。"+
			"未在投屏时报错并列出当前投屏清单；纹理硬解帧不可截取时自动重试 3 次，"+
			"仍失败提示改用 get_screenshot（设备端截图接口）。"+
			"android 传容器标识。返回 PNG image content。无破坏性。",
		func(ctx context.Context, a screenshotArgs) (*mcp.CallToolResult, error) {
			_, png, err := deps.capture(a.Android)
			if err != nil {
				return nil, err
			}
			return &mcp.CallToolResult{Content: []mcp.Content{
				&mcp.ImageContent{Data: png, MIMEType: "image/png"},
			}}, nil
		}, reg.ReadOnly())

	type closeArgs struct {
		Android string `json:"android" jsonschema_desc:"容器标识：androidId / 容器名 / 设备IP_坑位 / 设备IP（任一形态）"`
	}
	reg.Reg(srv, sink, "close_screencast",
		"关闭指定云机的投屏播放器窗口（经播放器本地控制接口优雅退出；"+
			"注意 stop_device_play 只停设备流，本工具才真正关掉播放器窗口）。"+
			"未在投屏时报错并列出当前投屏清单。android 传容器标识。",
		func(ctx context.Context, a closeArgs) (any, error) {
			key, err := deps.closePlayer(a.Android)
			if err != nil {
				return nil, err
			}
			return map[string]any{"result": "ok", "key": key}, nil
		}, reg.ReadOnly())
}
