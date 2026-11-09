package auth

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/moyunteng/myt/internal/util"
)

// CancelUserResult 注销账号 API 返回的结果
type CancelUserResult struct {
	Code    string
	Msg     string
	Uid     string
	HostNum int
}

// CancelUserAPI 注销账号（user_cancel）：需登录 token + 短信验证码（vcode/vkey）。
// 成功返回注销账号的 uid 与关联主机数。
//
// 签名算法（依据壳子服务端源码 lib/Core/Util.php::getSign，api.php 体系通用）：
// 取参与签名字段（跳过空值、剔除 _sign）按 key 升序排列，把「值」用 '#' 连接，
// 末尾再拼 '#' + 盐，整体 MD5。user_cancel 的 data 字段为 token/vcode/vkey/_ts，
// 故 sign = MD5(_ts#token#vcode#vkey#apiGeneralSalt)。
// 盐取壳子权威配置 my_config.inc.php 的 API_SIGN_KEY（'@#...'，以 @ 开头）；
// 例：sign = MD5("1792739608#token#vcode#vkey#@#1234A98413G=--..234")——
// vkey 与盐之间的 '#' 是 getSign 的分隔符，盐本身以 '@#' 开头。
// _ts 为 Unix 秒（同 slot.go 先例；sp_api.php 登录体系才用毫秒）。
func CancelUserAPI(client *http.Client, token, vcode, vkey string) (*CancelUserResult, error) {
	ts := time.Now().Unix()

	// 模拟 getSign：跳过空值 + ksort。user_cancel 可能不带 vkey（按 act 流程），
	// 与服务端同规则跳过空值后拼值，保证两侧一致。
	values := make([]string, 0, 4)
	if ts != 0 {
		values = append(values, fmt.Sprintf("%d", ts))
	}
	if token != "" {
		values = append(values, token)
	}
	if vcode != "" {
		values = append(values, vcode)
	}
	if vkey != "" {
		values = append(values, vkey)
	}
	sign := util.MD5Hash(strings.Join(values, "#") + "#" + apiGeneralSalt)

	data := map[string]string{
		"token": token,
		"vcode": vcode,
		"vkey":  vkey,
		"_ts":   fmt.Sprintf("%d", ts),
		"_sign": sign,
	}
	jsonData, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("序列化注销数据失败: %w", err)
	}

	params := url.Values{}
	params.Set("type", "user_cancel")
	params.Set("data", string(jsonData))

	log.Printf("[auth] CancelUserAPI 请求: data=%s", string(jsonData))
	body, err := util.MoyuntengPost(client, apiGeneralURL, params)
	if err != nil {
		return nil, fmt.Errorf("注销请求失败: %w", err)
	}
	log.Printf("[auth] CancelUserAPI 响应原文: %s", truncateBody(body, 800))

	return parseCancelUserResponse(body)
}

func truncateBody(body []byte, max int) string {
	if len(body) > max {
		return string(body[:max]) + "..."
	}
	return string(body)
}

func parseCancelUserResponse(body []byte) (*CancelUserResult, error) {
	var raw struct {
		Code any             `json:"code"`
		Msg  string          `json:"msg"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("解析注销响应失败: %w（原文: %s）", err, truncateBody(body, 300))
	}
	codeStr := fmt.Sprintf("%v", raw.Code)

	// 成功判定只看 code；data 形态不稳定（实测可能是数组而非对象，也可能为空数组/
	// null/false），所以解析 data 的失败不作为整体失败，只按能取到的信息填充结果。
	result := &CancelUserResult{Code: codeStr, Msg: raw.Msg}
	if util.IsSuccessCode(raw.Code) && len(raw.Data) > 0 {
		var item struct {
			Uid     string `json:"uid"`
			HostNum int    `json:"host_num"`
		}
		if err := tryUnmarshalCancelData(raw.Data, &item); err == nil &&
			(item.Uid != "" || item.HostNum != 0) {
			result.Uid = item.Uid
			result.HostNum = item.HostNum
		} else if err != nil {
			log.Printf("[auth] CancelUserAPI data 解析失败（忽略，以 code 为准）: %v（原文: %s）",
				err, truncateBody(raw.Data, 300))
		}
	}
	return result, nil
}

// tryUnmarshalCancelData 兼容 data 的多种形态：对象 {uid,host_num}、数组（取首个元素）、
// 空数组/null/false/字符串等其他形态返回哨兵错误（调用方按 code 判定成功即可）。
func tryUnmarshalCancelData(raw json.RawMessage, out *struct {
	Uid     string `json:"uid"`
	HostNum int    `json:"host_num"`
}) error {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) > 0 && trimmed[0] == '[' {
		var arr []json.RawMessage
		if err := json.Unmarshal(trimmed, &arr); err != nil {
			return err
		}
		if len(arr) == 0 {
			return errCancelDataEmpty
		}
		trimmed = bytes.TrimSpace(arr[0])
	}
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return errCancelDataEmpty
	}
	return json.Unmarshal(trimmed, out)
}

var errCancelDataEmpty = errors.New("data 无对象内容")
