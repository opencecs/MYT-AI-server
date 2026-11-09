package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/moyunteng/myt/internal/cloudapi"
	"github.com/moyunteng/myt/internal/sdkapi"
)

// DeviceFetch 通用设备请求代理（供前端 deviceFetch 调用，替代浏览器直连设备）。
// local 模式：deviceIP 为局域网 IP，直连设备。public 模式：deviceIP 为 device_id，走网关透传。
// method: GET/POST/PUT/DELETE；path: 设备 SDK 路径；body: POST/PUT 请求体 JSON（可为空）。
// 返回设备原始响应（{code,data,msg} 格式），前端按原 deviceFetch 逻辑解析。
func (s *SdkApiServiceWrapper) DeviceFetch(deviceIP, method, path, body string) (string, error) {
	client := s.newClient(deviceIP)
	resp, err := client.DoRequest(method, path, []byte(body))
	if err != nil {
		return "", err
	}
	return string(resp), nil
}

// DeviceFetchCancelable 可取消的设备请求代理（create 等前端编排长任务用）。
// 与 DeviceFetch 行为一致，额外通过 taskID 注册可取消 context；
// 前端 CancelTaskOp(taskID) 可中断在途 HTTP（sdkapi Client 全部请求用 requestContext() 透传 ctx）。
func (s *SdkApiServiceWrapper) DeviceFetchCancelable(deviceIP, method, path, body, taskID string) (string, error) {
	ctx := s.registerCancel(taskID)
	defer s.unregisterCancel(taskID)
	// 创建云机（POST /android、/androidV2）设备端同步等待容器建完才返回，
	// 8s 默认超时会在途中断开连接 → 设备端 r.Context() 被取消 → SDK 创建被中断（半截容器）。
	// 长耗时 POST 用 5 分钟超时；v2 模式首建可能触发主机拉镜像（分钟级更长），放宽到 15 分钟。
	// 用户手动取消（CancelTaskOp）仍即时生效。
	var client *sdkapi.Client
	if method == "POST" && (path == "/android" || path == "/androidV2") {
		if s.cm != nil && s.cm.GetDeployMode() == "v2" {
			client = s.newClientWithTimeout(deviceIP, 15*time.Minute)
		} else {
			client = s.newClientWithTimeout(deviceIP, 5*time.Minute)
		}
	} else {
		client = s.newClient(deviceIP)
	}
	client.SetContext(ctx)
	resp, err := client.DoRequest(method, path, []byte(body))
	if err != nil {
		return "", err
	}
	return string(resp), nil
}

// PublicSnap 公网模式截图代理：通过网关透传请求设备 SDK 的 /android/task=snap&level=1。
// deviceID 为设备 ID，indexNum 为容器坑位号。返回图片二进制数据。
func (s *SdkApiServiceWrapper) PublicSnap(deviceID string, indexNum int) ([]byte, error) {
	// B端容器级透传格式：/v1/devices/{id}/android/{path}?index=N&bridge=0
	// 不走 /passthrough 前缀和 buildURL，直接构造完整 URL
	token, err := s.getPublicToken()
	if err != nil {
		return nil, fmt.Errorf("网关鉴权失败: %w", err)
	}
	snapURL := fmt.Sprintf("%s/v1/devices/%s/android/task=snap?level=1&index=%d&bridge=0",
		cloudapi.GatewayURL, deviceID, indexNum)

	req, err := http.NewRequest("GET", snapURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "*/*")
	pwd := s.GetDevicePassword(deviceID)
	if pwd != "" {
		req.Header.Set("X-Device-Password", pwd)
	}

	resp, err := publicSnapHTTP.Do(req)
	if err != nil {
		log.Printf("[screenshot] public snap 失败 deviceID=%s index=%d err=%v", deviceID, indexNum, err)
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 5*1024*1024))
	if err != nil {
		return nil, err
	}

	log.Printf("[screenshot] public snap deviceID=%s index=%d len=%d contentType=%s status=%d",
		deviceID, indexNum, len(body), http.DetectContentType(body), resp.StatusCode)

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("截图失败: HTTP %d", resp.StatusCode)
	}

	return body, nil
}

// GetPublicToken 获取 public 模式网关 access_token（导出版，供投屏等外部模块使用）。
func (s *SdkApiServiceWrapper) GetPublicToken() (string, error) {
	return s.getPublicToken()
}

// getPublicToken 获取 public 模式授权 token（由 payment 提供，网关统一鉴权）。
func (s *SdkApiServiceWrapper) getPublicToken() (string, error) {
	if s.cloudClient == nil {
		s.cloudClient = cloudapi.NewClient()
	}
	token, err := s.cloudClient.EnsureValidToken()
	if err != nil || token == "" {
		return "", fmt.Errorf("未获取到授权 token，请先登录魔云腾账号")
	}
	return token, nil
}
