package util

import (
	"crypto/md5"
	"encoding/hex"
	"net/http"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// FlexInt 宽松整数：云 API（PHP 系）常把数字以字符串返回（如 spid:"0"、type:"1"），
// 数字字面量与带引号字符串都接受；解析失败按 0（宁缺勿错，不阻断整个镜像列表）。
type FlexInt int

// UnmarshalJSON 兼容 number / "number" / null / "" 四种形态
func (f *FlexInt) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "" || s == "null" {
		*f = 0
		return nil
	}
	if n, err := strconv.Atoi(s); err == nil {
		*f = FlexInt(n)
		return nil
	}
	// 浮点形态（1.0）取整
	if fl, err := strconv.ParseFloat(s, 64); err == nil {
		*f = FlexInt(fl)
		return nil
	}
	*f = 0
	return nil
}

type MirrorItem struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	URL       string   `json:"url"`
	TType     string   `json:"ttype"`  // 适配机型（单值，V1 接口）
	TType2    []string `json:"ttype2"` // 适配机型（数组）
	OsVer     string   `json:"os_ver"`
	SysVer    string   `json:"sys_ver"`
	SysVerDes string   `json:"sys_ver_des"`
	UDesc     string   `json:"udesc"`
	Type      FlexInt  `json:"type"` // 0=免费镜像, 1=付费(专业版)——V1 接口字段
	Sort      FlexInt  `json:"sort"` // 显示排序（降序）——V1 接口字段
	State     FlexInt  `json:"state"`
	Spid      FlexInt  `json:"spid"`
}

func MD5Hash(s string) string {
	h := md5.Sum([]byte(s))
	return hex.EncodeToString(h[:])
}

const (
	LgCloudMagic = "lgcloud"
	LgCloudPort  = 7678
)

func NewHTTPClient() *http.Client {
	return &http.Client{Timeout: 15 * time.Second}
}

// NewLongTimeoutHTTPClient 用于大文件下载/上传等耗时操作
func NewLongTimeoutHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout}
}

// IsSuccessCode 检查 API 返回的 code 是否表示成功
// code 可能是数值(0/200)或字符串("0"/"200")
func IsSuccessCode(code any) bool {
	switch v := code.(type) {
	case float64:
		return v == 0 || v == 200
	case string:
		return v == "0" || v == "200"
	case int:
		return v == 0 || v == 200
	default:
		return false
	}
}

// IsAuthCode 检查 API 返回的 code 是否表示认证失败
func IsAuthCode(code any) bool {
	switch v := code.(type) {
	case float64:
		return v == 61
	case string:
		return v == "61"
	case int:
		return v == 61
	default:
		return false
	}
}

// FriendlyNetError 将网络错误翻译为用户友好的中文提示
func FriendlyNetError(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "context deadline exceeded"),
		strings.Contains(msg, "Client.Timeout exceeded"),
		strings.Contains(msg, "i/o timeout"):
		return "网络请求超时"
	case strings.Contains(msg, "can't assign requested address"),
		strings.Contains(msg, "connection refused"),
		strings.Contains(msg, "no such host"):
		return "网络连接异常"
	case strings.Contains(msg, "connection reset by peer"),
		strings.Contains(msg, "broken pipe"):
		return "网络连接中断"
	default:
		return msg
	}
}

// OpenDirectory 使用系统命令打开目录
func OpenDirectory(path string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", path).Start()
	case "windows":
		return exec.Command("explorer", path).Start()
	default:
		return exec.Command("xdg-open", path).Start()
	}
}

// TruncateBody 截取响应体前 500 字节用于错误日志，避免大响应刷屏
func TruncateBody(body []byte) string {
	const max = 500
	if len(body) <= max {
		return string(body)
	}
	return string(body[:max]) + "..."
}

// CheckUnsupported 检测设备返回的纯文本是否表示接口不支持，
// 返回友好提示；若不是不支持错误则返回空字符串。
func CheckUnsupported(body []byte) string {
	s := strings.TrimSpace(string(body))
	if strings.Contains(s, "does not exist") || strings.Contains(s, "not found") {
		return "该设备固件版本不支持此功能，请升级到最新 V3 镜像"
	}
	return ""
}
