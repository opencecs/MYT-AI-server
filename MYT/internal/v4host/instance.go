package v4host

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/moyunteng/myt/internal/config"
)

// --- V4（tobox_player）8088 REST 客户端 ---
//
// 接口清单见 tobox_player/api.json（RPC 风格、无版本前缀）：
//
//	GET  /android               → {instances:[InstanceInfo], total}
//	POST /android/start|stop|reset|delete   body {id, backend?, wipe_data?}
//	GET  /health                → {"status":"ok"}（唯一免认证端点，无设备标识）
//	GET  /info                  → SDK 版本 / 后端概览
//
// 响应统一包一层：成功 {"code":0,"message":"ok","data":...}；
// 失败 {"code":<httpStatus>,"message":"...","data":null}（HTTP 状态码同样非 200）。
// 鉴权：全局 Basic Auth（/health 豁免），默认 admin/myt。

const (
	// DefaultUser / DefaultPassword V4 默认 Basic Auth 凭据（tebox.auth 默认值）
	DefaultUser     = "admin"
	DefaultPassword = "myt"

	// opTimeout 实例操作超时：开机/关机/删除是同步等待（启动约 70s 才 running，
	// 但接口本身在状态切到 starting 即返回），给 60s 余量
	opTimeout = 60 * time.Second
	// listTimeout 列表超时：聚合多后端，单后端不可达时服务端会跳过（可能慢几秒）
	listTimeout = 20 * time.Second
	// maxRespSize 响应体读取上限（实例多时列表几十 KB，防异常大包）
	maxRespSize = 8 << 20
)

// Instance V4 实例信息（GET /android 的 instances 元素，字段见 api.json
// tobox_player.api.instance.v1.InstanceInfo）。
//
// cast_port / control_port 是宿主机 loopback 内部端口，外部播放器走全局
// 9901/9905 + 握手行，不能直接连——此处仅落库备查。
type Instance struct {
	// Backend 实例所属后端名（列表始终标注；不同后端可有同名 id）
	Backend string `json:"backend"`
	ID      string `json:"id"`
	Name    string `json:"name"`
	Variant string `json:"variant"`
	// State stopped / starting / running / stopping
	State   string `json:"state"`
	Running bool   `json:"running"`
	PID     int32  `json:"pid"`

	ADBPort     int32 `json:"adb_port"`
	CastPort    int32 `json:"cast_port"`
	ControlPort int32 `json:"control_port"`

	CPUCores   int32 `json:"cpu_cores"`
	MemoryMB   int32 `json:"memory_mb"`
	DiskMB     int32 `json:"disk_mb"`
	DiskUsedMB int32 `json:"disk_used_mb"`
}

// ListResult GET /android 的 data 结构
type ListResult struct {
	Instances []Instance `json:"instances"`
	Total     int        `json:"total"`
}

// apiEnvelope 统一响应外壳
type apiEnvelope struct {
	Code    json.RawMessage `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

// InstanceRef 实例操作参数（id 必填，backend 可选=默认后端）
type InstanceRef struct {
	ID      string `json:"id"`
	Backend string `json:"backend,omitempty"`
}

// InstanceClient 单台 V4 主机的 8088 REST 客户端（每次调用现建，轻量）。
type InstanceClient struct {
	baseURL  string
	user     string
	password string
	http     *http.Client
}

// NewInstanceClient 创建客户端。host 形如 "10.10.2.142" 或 "10.10.2.142:8088"；
// user/password 为空时用默认 admin/myt。
func NewInstanceClient(host, user, password string) *InstanceClient {
	if user == "" {
		user = DefaultUser
	}
	if password == "" {
		password = DefaultPassword
	}
	base := host
	if !strings.HasPrefix(base, "http://") && !strings.HasPrefix(base, "https://") {
		if !HasPort(base) {
			base = fmt.Sprintf("%s:%d", base, DefaultPort)
		}
		base = "http://" + base
	}
	return &InstanceClient{
		baseURL:  strings.TrimSuffix(base, "/"),
		user:     user,
		password: password,
		http:     &http.Client{Transport: probeTransport},
	}
}

// List 拉取实例列表（GET /android，聚合全部后端）。
func (c *InstanceClient) List(ctx context.Context) (*ListResult, error) {
	body, err := c.do(ctx, http.MethodGet, "/android", nil, listTimeout)
	if err != nil {
		return nil, err
	}
	data, err := unwrapEnvelope(body)
	if err != nil {
		return nil, err
	}
	var res ListResult
	if len(data) > 0 {
		if err := json.Unmarshal(data, &res); err != nil {
			return nil, fmt.Errorf("/android 响应解析失败: %w", err)
		}
	}
	if res.Instances == nil {
		res.Instances = []Instance{}
	}
	return &res, nil
}

// Start 启动实例（POST /android/start）。
func (c *InstanceClient) Start(ctx context.Context, ref InstanceRef) error {
	return c.postRef(ctx, "/android/start", ref, nil)
}

// Stop 停止实例（POST /android/stop）。
func (c *InstanceClient) Stop(ctx context.Context, ref InstanceRef) error {
	return c.postRef(ctx, "/android/stop", ref, nil)
}

// Delete 删除实例及其全部磁盘数据（POST /android/delete，不可恢复）。
func (c *InstanceClient) Delete(ctx context.Context, ref InstanceRef) error {
	return c.postRef(ctx, "/android/delete", ref, nil)
}

// Reset 重置实例底层环境（POST /android/reset）。wipeData=true 时清空数据分区
// （恢复出厂，已安装应用与数据丢失）。
func (c *InstanceClient) Reset(ctx context.Context, ref InstanceRef, wipeData bool) error {
	extra := map[string]any{"wipe_data": wipeData}
	return c.postRef(ctx, "/android/reset", ref, extra)
}

// DeviceProfile 实例设备档案（POST /android/configure 的 profile 字段，
// Swagger: tobox_player.api.device_profile.v1.DeviceProfile）。
//
// 单一事实源在 config.AndroidProfile（依赖叶子包；本包与 v4ws 的 REST/WS 两条
// 路径共用同一 JSON 契约，snake_case 字段名由其 json tag 保证），此处仅为别名。
type DeviceProfile = config.AndroidProfile

// SetClipboard 设置实例剪贴板（POST /android/clipboard）。
// paste=true 时云机把文本直接粘贴到当前焦点输入框（不只落到剪贴板）。
// backend 为空走服务端默认后端。
func (c *InstanceClient) SetClipboard(ctx context.Context, ref InstanceRef, text string, paste bool) error {
	if ref.ID == "" {
		return fmt.Errorf("实例 id 为空")
	}
	if text == "" {
		return fmt.Errorf("剪贴板文本为空")
	}
	payload := map[string]any{"id": ref.ID, "text": text, "paste": paste}
	if ref.Backend != "" {
		payload["backend"] = ref.Backend
	}
	body, err := c.do(ctx, http.MethodPost, "/android/clipboard", payload, opTimeout)
	if err != nil {
		return err
	}
	_, err = unwrapEnvelope(body)
	return err
}

// Configure 设置实例设备档案（POST /android/configure，覆盖式写入 profile）。
func (c *InstanceClient) Configure(ctx context.Context, ref InstanceRef, profile DeviceProfile) error {
	if ref.ID == "" {
		return fmt.Errorf("实例 id 为空")
	}
	payload := map[string]any{"id": ref.ID, "profile": profile}
	if ref.Backend != "" {
		payload["backend"] = ref.Backend
	}
	body, err := c.do(ctx, http.MethodPost, "/android/configure", payload, opTimeout)
	if err != nil {
		return err
	}
	_, err = unwrapEnvelope(body)
	return err
}

// postRef 实例操作类 POST：body = {id, backend?} + 可选额外字段。
func (c *InstanceClient) postRef(ctx context.Context, path string, ref InstanceRef, extra map[string]any) error {
	if ref.ID == "" {
		return fmt.Errorf("实例 id 为空")
	}
	payload := map[string]any{"id": ref.ID}
	if ref.Backend != "" {
		payload["backend"] = ref.Backend
	}
	for k, v := range extra {
		payload[k] = v
	}
	body, err := c.do(ctx, http.MethodPost, path, payload, opTimeout)
	if err != nil {
		return err
	}
	_, err = unwrapEnvelope(body)
	return err
}

// do 发请求：带 Basic Auth、读体、按 HTTP 状态码分类错误。
func (c *InstanceClient) do(ctx context.Context, method, path string, payload any, timeout time.Duration) ([]byte, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var reader io.Reader
	if payload != nil {
		buf, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("序列化请求失败: %w", err)
		}
		reader = bytes.NewReader(buf)
	}
	req, err := http.NewRequestWithContext(reqCtx, method, c.baseURL+path, reader)
	if err != nil {
		return nil, fmt.Errorf("构造请求失败: %w", err)
	}
	req.SetBasicAuth(c.user, c.password)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s %s 失败: %w", method, path, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxRespSize))
	if err != nil {
		return nil, fmt.Errorf("%s %s 读取响应失败: %w", method, path, err)
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("%s %s 认证失败（HTTP 401，检查 Basic Auth 凭据）", method, path)
	}
	if resp.StatusCode >= 400 {
		// 服务端错误体带 message（如未知后端 400），优先透出
		if msg := envelopeMessage(body); msg != "" {
			return nil, fmt.Errorf("%s %s 失败（HTTP %d）: %s", method, path, resp.StatusCode, msg)
		}
		return nil, fmt.Errorf("%s %s 失败（HTTP %d）", method, path, resp.StatusCode)
	}
	return body, nil
}

// unwrapEnvelope 解开 {code,message,data} 外壳：code!=0 视为业务失败。
func unwrapEnvelope(body []byte) (json.RawMessage, error) {
	if len(bytes.TrimSpace(body)) == 0 {
		return nil, nil // 空体视为成功（部分操作无 data）
	}
	var env apiEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("响应不是合法 JSON: %w", err)
	}
	if len(env.Code) == 0 {
		// 无 code 字段：直接当数据体返回（兼容非标准响应）
		return body, nil
	}
	var code any
	_ = json.Unmarshal(env.Code, &code)
	if !isZeroCode(code) {
		return nil, fmt.Errorf("接口返回错误: code=%v message=%s", code, env.Message)
	}
	return env.Data, nil
}

// envelopeMessage 从错误响应体提取 message（解析失败返回空）
func envelopeMessage(body []byte) string {
	var env apiEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		return ""
	}
	return env.Message
}

// isZeroCode code 是否表示成功（0 或 "0"）
func isZeroCode(code any) bool {
	switch v := code.(type) {
	case float64:
		return v == 0
	case string:
		return v == "0" || v == ""
	case nil:
		return true
	}
	return false
}
