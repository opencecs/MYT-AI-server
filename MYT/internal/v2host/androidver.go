package v2host

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"
)

// androidVerTimeout 主机安卓版本探测超时：创建云机弹窗打开时调用，
// 失败不阻塞 UI（壳子 error 回调同语义，用默认值继续）。
const androidVerTimeout = 3 * time.Second

// GetHostAndroidVersion 探测主机安卓版本：
// GET http://{ip}:81/get_key_value?key=ANDROID_VERSION，
// 响应形如 {"code":200,"msg":"and14"}，成功取 msg 字段。
// 任何失败（请求错误/超时/非 2xx/code 异常/msg 为空）返回空串不报错——
// 调用方用默认 'and12' 继续过滤镜像。
func (c *Client) GetHostAndroidVersion(ip string) string {
	ctx, cancel := context.WithTimeout(context.Background(), androidVerTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		c.sdkBaseURL(ip)+"/get_key_value?key=ANDROID_VERSION", nil)
	if err != nil {
		return ""
	}
	resp, err := c.sdkHC.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return ""
	}
	var r struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return ""
	}
	// code 校验与 81 端口其它接口一致（200/0 视为成功）；msg 为空视为取不到
	if (r.Code != 200 && r.Code != 0) || r.Msg == "" {
		return ""
	}
	return r.Msg
}
