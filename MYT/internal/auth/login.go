package auth

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/moyunteng/myt/internal/util"
)

const (
	apiBaseURL     = "https://moyunteng.com/api/sp_api.php"
	apiSalt        = "454&*&*fsdff"
	apiGeneralURL  = "https://www.moyunteng.com/api/api.php"
	// apiGeneralSalt api.php 体系签名盐。以壳子权威配置 lib/inc/my_config.inc.php 的
	// API_SIGN_KEY = '@#1234A98413G=--..234' 为准（以 @# 开头，不含前导 '#'）。
	// 注意：internal/account/slot/slot.go 的盐为 "#@#..."（多一个前导 '#'），该接口实测可用，勿在此统一。
	apiGeneralSalt = "@#1234A98413G=--..234"
)

// LoginResult 登录 API 返回的完整结果
type LoginResult struct {
	Code  string
	Msg   string
	Token string
	Uname string
	Uid   string
}

// VCodeResult 获取验证码 API 返回的结果
type VCodeResult struct {
	Code string
	Msg  string
	VKey string
}

// RegisterResult 注册 API 返回的结果
type RegisterResult struct {
	Code string
	Msg  string
}

// ResetPasswordResult 重置密码 API 返回的结果
type ResetPasswordResult struct {
	Code string
	Msg  string
}

// LoginAPI 调用统一的登录 API，返回解析后的结果
func LoginAPI(client *http.Client, username, password string) (*LoginResult, error) {
	ts := time.Now().UnixMilli()
	pwdMD5 := util.MD5Hash(password)

	signStr := fmt.Sprintf("%d#%s#%s#%s", ts, pwdMD5, username, apiSalt)
	sign := util.MD5Hash(signStr)

	data := map[string]string{
		"_ts":   fmt.Sprintf("%d", ts),
		"pwd":   pwdMD5,
		"uname": username,
		"_sign": sign,
	}
	jsonData, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("序列化登录数据失败: %w", err)
	}

	params := url.Values{}
	params.Set("type", "login")
	params.Set("data", string(jsonData))

	resp, err := client.PostForm(apiBaseURL, params)
	if err != nil {
		return nil, fmt.Errorf("登录请求失败: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}

	return parseLoginResponse(body)
}

// GetPhoneVCodeAPI 获取手机验证码
func GetPhoneVCodeAPI(client *http.Client, phone string) (*VCodeResult, error) {
	data := map[string]string{
		"act":   "com",
		"phone": phone,
		"plat":  "",
		"token": "",
	}
	jsonData, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("序列化验证码数据失败: %w", err)
	}

	params := url.Values{}
	params.Set("type", "get_phone_vcode")
	params.Set("data", string(jsonData))

	body, err := util.MoyuntengPost(client, apiGeneralURL, params)
	if err != nil {
		return nil, fmt.Errorf("获取验证码请求失败: %w", err)
	}

	return parseVCodeResponse(body)
}

// RegisterAPI 用户注册
func RegisterAPI(client *http.Client, phone, password, vcode, vkey string) (*RegisterResult, error) {
	data := map[string]string{
		"uname": phone,
		"pwd":   util.MD5Hash(password),
		"vcode": vcode,
		"vkey":  vkey,
		"token": "",
	}
	jsonData, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("序列化注册数据失败: %w", err)
	}

	params := url.Values{}
	params.Set("type", "register")
	params.Set("data", string(jsonData))

	body, err := util.MoyuntengPost(client, apiGeneralURL, params)
	if err != nil {
		return nil, fmt.Errorf("注册请求失败: %w", err)
	}

	return parseRegisterResponse(body)
}

// ResetPasswordAPI 重置密码（忘记密码）
func ResetPasswordAPI(client *http.Client, phone, password, vcode, vkey string) (*ResetPasswordResult, error) {
	data := map[string]string{
		"uname": phone,
		"pwd":   util.MD5Hash(password),
		"vcode": vcode,
		"vkey":  vkey,
	}
	jsonData, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("序列化重置密码数据失败: %w", err)
	}

	params := url.Values{}
	params.Set("type", "find_pwd")
	params.Set("data", string(jsonData))

	body, err := util.MoyuntengPost(client, apiGeneralURL, params)
	if err != nil {
		return nil, fmt.Errorf("重置密码请求失败: %w", err)
	}

	return parseResetPasswordResponse(body)
}

// --- 响应解析 ---

func parseLoginResponse(body []byte) (*LoginResult, error) {
	var raw struct {
		Code any             `json:"code"`
		Msg  string          `json:"msg"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("解析登录响应失败: %w", err)
	}
	codeStr := fmt.Sprintf("%v", raw.Code)

	if codeStr != "200" {
		return &LoginResult{Code: codeStr, Msg: raw.Msg}, nil
	}

	var data struct {
		Token string `json:"token"`
		Uname string `json:"uname"`
		Uid   string `json:"uid"`
	}
	if err := json.Unmarshal(raw.Data, &data); err != nil {
		return nil, fmt.Errorf("解析登录data失败: %w", err)
	}

	return &LoginResult{Code: codeStr, Msg: raw.Msg, Token: data.Token, Uname: data.Uname, Uid: data.Uid}, nil
}

func parseVCodeResponse(body []byte) (*VCodeResult, error) {
	var raw struct {
		Code any    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			VKey string `json:"vkey"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("解析验证码响应失败: %w", err)
	}
	codeStr := fmt.Sprintf("%v", raw.Code)
	return &VCodeResult{Code: codeStr, Msg: raw.Msg, VKey: raw.Data.VKey}, nil
}

func parseRegisterResponse(body []byte) (*RegisterResult, error) {
	var raw struct {
		Code any    `json:"code"`
		Msg  string `json:"msg"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("解析注册响应失败: %w", err)
	}
	codeStr := fmt.Sprintf("%v", raw.Code)
	return &RegisterResult{Code: codeStr, Msg: raw.Msg}, nil
}

func parseResetPasswordResponse(body []byte) (*ResetPasswordResult, error) {
	var raw struct {
		Code any    `json:"code"`
		Msg  string `json:"msg"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("解析重置密码响应失败: %w", err)
	}
	codeStr := fmt.Sprintf("%v", raw.Code)
	return &ResetPasswordResult{Code: codeStr, Msg: raw.Msg}, nil
}
