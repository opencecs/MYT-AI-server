package util

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// 魔云腾 API 常量
const (
	MoyuntengAPI      = "https://www.moyunteng.com/api/api.php"
	MoyuntengLoginAPI = "https://moyunteng.com/api/sp_api.php"
	MoyuntengMirrorAPI = "http://api.moyunteng.com/api.php"
)

// MoyuntengPost 发送魔云腾 API 请求（POST, application/x-www-form-urlencoded）
// params 为表单参数（type/data/act 等），会自动 URL 编码
func MoyuntengPost(client *http.Client, apiURL string, params url.Values) ([]byte, error) {
	apiType := params.Get("type")

	req, err := http.NewRequest("POST", apiURL, strings.NewReader(params.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Referer", "https://www.moyunteng.com/")
	req.Header.Set("Origin", "https://www.moyunteng.com")
	req.Header.Set("User-Agent", "Mozilla/5.0")
	req.Header.Set("Accept", "application/json, text/javascript, */*; q=0.01")

	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		Debugf("[http] POST api.php?type=%s → 失败: %v (%s)", apiType, FriendlyNetError(err), time.Since(start).Round(time.Millisecond))
		return nil, fmt.Errorf("API请求失败(%s): %s", apiType, FriendlyNetError(err))
	}
	Debugf("[http] POST %s?type=%s → %d (%s)", apiURL, apiType, resp.StatusCode, time.Since(start).Round(time.Millisecond))
	defer resp.Body.Close()

	return io.ReadAll(resp.Body)
}
