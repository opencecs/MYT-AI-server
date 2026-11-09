package sdkapi

import (
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"strconv"
	"strings"
)

// --- V2 路由翻译层 ---
// V2 模式下设备语义由主机 81 端口（超级 SDK / MYT_SDK_WEB）承接，8000 不存在。
// 本层把 8000 设备 SDK 的请求翻译为 81 dc_api 等价请求（壳子同款通道），
// 并把 81 的 {code:200} 响应归一为 8000 风格 {code:0}——调用方（taskqueue
// 批量操作 / 前端 deviceFetch / wrapper 各方法）零改动。
//
// 对照表（8000 → 81）：
//
//	POST /android/start {"name"}      → GET /dc_api/v1/run/{ip}/{name}
//	POST /android/stop  {"name"}      → GET /dc_api/v1/stop/{ip}/{name}
//	POST /android/restart {"name"}    → GET /dc_api/v1/reboot/{ip}/{name}
//	DELETE /android?name=x            → GET /dc_api/v1/remove/{ip}/{x}
//	PUT /android {"name",...}         → GET /dc_api/v1/reset/{ip}/{name}
//	POST /androidV2 {创建参数}        → POST /dc_api/v1/create/{ip}/{index}/{name}?image_addr=...
//
// 未识别路径返回错误（明确「暂不支持 V2 模式」，避免对 81 产生 404 噪音）。

// v2Rewrite 把一条 8000 风格请求翻译为 81 请求。
// 返回 newMethod/newPath（含 query）/newBody；ok=false 表示无对应翻译。
func v2Rewrite(hostIP, method, path string, body []byte) (string, string, []byte, bool, error) {
	// 分离 path 与 query
	base, query, _ := strings.Cut(path, "?")
	q := mustParseQuery(query)

	nameOf := func() string {
		var m struct {
			Name string `json:"name"`
		}
		_ = json.Unmarshal(body, &m)
		if m.Name == "" {
			m.Name = q.Get("name")
		}
		return m.Name
	}

	dcGet := func(action, name string) (string, string, []byte, bool, error) {
		if name == "" {
			return "", "", nil, false, fmt.Errorf("缺少容器名")
		}
		return "GET", fmt.Sprintf("/dc_api/v1/%s/%s/%s", action, hostIP, url.PathEscape(name)), nil, true, nil
	}

	switch {
	case base == "/androidV2" && method == "POST":
		// 创建容器云机：81 create 参数在 query（image_addr/dns/sandbox/sandbox_size/resolution/width/height/dpi/start），
		// image_addr 直传线上 registry 地址（mytsdk 本地没有会自动拉取——V2 Web 同款语义）
		return v2RewriteCreate(hostIP, body)
	case base == "/android/start" && method == "POST":
		return dcGet("run", nameOf())
	case base == "/android/stop" && method == "POST":
		return dcGet("stop", nameOf())
	case base == "/android/restart" && method == "POST":
		return dcGet("reboot", nameOf())
	case base == "/android" && method == "DELETE":
		return dcGet("remove", q.Get("name"))
	case base == "/android" && method == "PUT":
		// 重置容器（恢复出厂）：81 reset 一步完成，start 参数不透传（81 reset 后由调用方按需 run）
		return dcGet("reset", nameOf())
	case base == "/android/exec" && method == "POST":
		// 容器内命令执行：8000 {"name","command":["sd","-c",cmd]} → 81 shell {"cmd":cmd}
		// （81 /and_api/v1/shell 直接执行命令本身，无需 sd -c 包装；取数组末位为实际命令）
		var req struct {
			Name    string   `json:"name"`
			Command []string `json:"command"`
		}
		if err := json.Unmarshal(body, &req); err != nil || req.Name == "" || len(req.Command) == 0 {
			return "", "", nil, false, fmt.Errorf("exec 参数缺失（name/command）")
		}
		nb, _ := json.Marshal(map[string]string{"cmd": req.Command[len(req.Command)-1]})
		return "POST", fmt.Sprintf("/and_api/v1/shell/%s/%s", hostIP, url.PathEscape(req.Name)), nb, true, nil
	}
	return "", "", nil, false, nil
}

// v2RewriteCreate 创建容器：8000 POST /androidV2 JSON body → 81 create path+query。
func v2RewriteCreate(hostIP string, body []byte) (string, string, []byte, bool, error) {
	var req struct {
		Name        string `json:"name"`
		IndexNum    any    `json:"indexNum"`
		ImageURL    string `json:"imageUrl"`
		DNS         string `json:"dns"`
		SandboxSize string `json:"sandboxSize"`
		DoboxWidth  any    `json:"doboxWidth"`
		DoboxHeight any    `json:"doboxHeight"`
		DoboxDpi    any    `json:"doboxDpi"`
		Start       bool   `json:"start"`
		Enforce     bool   `json:"enforce"` // 严格模式（V2 Web 表单"安全模式"的反向开关）
	}
	if err := json.Unmarshal(body, &req); err != nil {
		log.Printf("[v2route] 创建参数解析失败: bodyLen=%d body=%.200s err=%v", len(body), string(body), err)
		return "", "", nil, false, fmt.Errorf("创建参数解析失败: %w", err)
	}
	if req.Name == "" {
		log.Printf("[v2route] 创建参数缺失 name: bodyLen=%d body=%.200s", len(body), string(body))
		return "", "", nil, false, fmt.Errorf("创建参数缺失（name 必填）")
	}
	indexNum := anyToInt(req.IndexNum)
	if indexNum <= 0 {
		return "", "", nil, false, fmt.Errorf("创建参数缺失（indexNum 必填）")
	}
	width := anyToInt(req.DoboxWidth)
	height := anyToInt(req.DoboxHeight)
	dpi := anyToInt(req.DoboxDpi)
	qs := url.Values{}
	if req.ImageURL != "" {
		qs.Set("image_addr", req.ImageURL)
	}
	if req.DNS != "" {
		qs.Set("dns", req.DNS)
	}
	if req.SandboxSize != "" {
		// "16GB" → sandbox=1 + sandbox_size=16（81 单位 GB）
		qs.Set("sandbox", "1")
		qs.Set("sandbox_size", strings.TrimSuffix(req.SandboxSize, "GB"))
	}
	if req.Enforce {
		qs.Set("enforce", "1")
	}
	// 81 分辨率：0=720P 1=1080P 2=自定义（width/height/dpi）——
	// 720x1280/1080x1920 映射官方预设（与 V2 Web 表单对齐），其余走自定义
	if width > 0 && height > 0 {
		switch {
		case width == 720 && height == 1280:
			qs.Set("resolution", "0")
		case width == 1080 && height == 1920:
			qs.Set("resolution", "1")
		default:
			qs.Set("resolution", "2")
			qs.Set("width", fmt.Sprintf("%d", width))
			qs.Set("height", fmt.Sprintf("%d", height))
			qs.Set("dpi", fmt.Sprintf("%d", dpi))
		}
	}
	// 注：81 dc create 无 start 参数（实测被 mytsdk 忽略，openapi 参数表可证）——
	// 「创建后开机」由前端在 create 成功后显式调 /android/start（壳子 V2 Web 同款：
	// createContainer → that.start），这里不再传无效参数
	newPath := fmt.Sprintf("/dc_api/v1/create/%s/%d/%s?%s", hostIP, indexNum, url.PathEscape(req.Name), qs.Encode())
	return "POST", newPath, nil, true, nil
}

// anyToInt 宽松取整：前端传值可能是 number（indexNum）也可能是 string
// （doboxWidth="720"——derived.resW 是字符串），统一转 int，解析失败返回 0。
func anyToInt(v any) int {
	switch x := v.(type) {
	case float64:
		return int(x)
	case json.Number:
		n, _ := x.Int64()
		return int(n)
	case string:
		n, _ := strconv.Atoi(strings.TrimSpace(x))
		return n
	case bool:
		return 0
	default:
		return 0
	}
}

// v2NormalizeResponse 把 81 的 {code:200} 归一为 8000 风格 {code:0}。
// 非 JSON 或已兼容的响应原样返回。
func v2NormalizeResponse(body []byte) []byte {
	var m map[string]any
	if json.Unmarshal(body, &m) != nil {
		return body
	}
	code, ok := m["code"].(float64)
	if !ok || code != 200 {
		return body
	}
	m["code"] = 0
	out, err := json.Marshal(m)
	if err != nil {
		return body
	}
	return out
}

// v2BusinessError 解析 81 响应的业务错误：dc_api 系列 HTTP 恒为 200，
// 失败以 {code:非200, message/msg:"..."} 表达（实测 remove 对不存在的容器
// 返回 HTTP 200 + {"code":2,"message":"xx容器不存在!"}）。getRaw/postRaw
// 只查 HTTP 状态码，若在这里不拦截，业务失败会被当成功返回（假成功——
// 删除/重置等操作失败时前端毫无提示）。code 缺失（非 dc_api 响应，如
// 2375 代理的裸 JSON）视为成功。
func v2BusinessError(body []byte) error {
	var m struct {
		Code    any    `json:"code"`
		Message string `json:"message"`
		Msg     string `json:"msg"`
	}
	if json.Unmarshal(body, &m) != nil {
		return nil // 非 JSON：无业务码语义，交由调用方按需解析
	}
	code, ok := m.Code.(float64)
	if !ok {
		return nil // 无数字 code：非 dc_api 风格响应（2375 /containers/prune 等）
	}
	if code == 200 || code == 0 {
		return nil
	}
	msg := m.Message
	if msg == "" {
		msg = m.Msg
	}
	return fmt.Errorf("V2 主机返回错误（code=%d）: %s", int(code), msg)
}

func mustParseQuery(raw string) url.Values {
	v, err := url.ParseQuery(raw)
	if err != nil {
		return url.Values{}
	}
	return v
}

// v2Dispatch v2 模式统一入口：翻译 8000 请求 → 81 dc_api 后按新方法派发到
// 底层 raw 执行（翻译产物是 v2 原生路径，raw 方法不再触发翻译，无递归）。
// 未识别的 8000 路径返回明确错误（该操作暂不支持 V2 模式，避免 404 噪音）。
func (c *Client) v2Dispatch(method, path string, body []byte) ([]byte, error) {
	newMethod, newPath, newBody, ok, err := v2Rewrite(c.v2HostIP, method, path, body)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("该操作暂不支持 V2 模式（%s %s）", method, path)
	}
	var resp []byte
	switch newMethod {
	case "GET":
		resp, err = c.getRaw(newPath)
	case "POST":
		if len(newBody) > 0 {
			resp, err = c.postRaw(newPath, json.RawMessage(newBody))
		} else {
			resp, err = c.postRaw(newPath, nil)
		}
	default:
		return nil, fmt.Errorf("v2 翻译产生不支持的方法: %s", newMethod)
	}
	if err != nil {
		return nil, err
	}
	// 业务码拦截先于归一化：失败响应（code≠200）直接报错，不做 code→0 的假归一
	if bizErr := v2BusinessError(resp); bizErr != nil {
		return nil, bizErr
	}
	return v2NormalizeResponse(resp), nil
}
