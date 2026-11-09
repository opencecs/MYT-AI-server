package main

import (
	"fmt"
	"strings"
	"sync"

	"github.com/moyunteng/myt/internal/config"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// StreamServiceWrapper 流媒体服务（Wails v3 Service）
type StreamServiceWrapper struct {
	app          *application.App
	cm           *config.Manager
	server       *rtmpServer
	p2pManager   *P2PManager
	localIP      string
	savedMu      sync.Mutex
	savedStreams []string
}

// NewStreamServiceWrapper 创建流媒体服务
func NewStreamServiceWrapper() *StreamServiceWrapper {
	return &StreamServiceWrapper{
		server:     &rtmpServer{},
		p2pManager: NewP2PManager(),
	}
}

func (s *StreamServiceWrapper) SetApp(app *application.App)         { s.app = app }
func (s *StreamServiceWrapper) SetConfigManager(cm *config.Manager) { s.cm = cm }

// StartServer 启动流媒体服务器
func (s *StreamServiceWrapper) StartServer() {
	s.localIP = getLocalIP()
	s.server.start()
}

// StopServer 停止流媒体服务器
func (s *StreamServiceWrapper) StopServer() {
	s.server.stop()
	if s.p2pManager != nil {
		s.p2pManager.StopAll()
	}
	stopAllWindowsPushers()
}

// GetLocalIp 获取本机 IP
func (s *StreamServiceWrapper) GetLocalIp() string {
	return s.localIP
}

// GetLocalIpForDevice 获取面向指定设备的本机源 IP（推流分发用：
// 设备必须能路由回该地址拉流；TUN 代理机器上 GetLocalIp 可能返回
// 198.18.x 代理网段或 WSL 虚拟网卡地址，设备不可达 → 云机黑屏）
func (s *StreamServiceWrapper) GetLocalIpForDevice(deviceIP string) string {
	return localIPForDevice(deviceIP)
}

// GetActiveStreams 获取活跃推流列表
func (s *StreamServiceWrapper) GetActiveStreams() []StreamInfo {
	active := s.server.getActiveStreams()
	activeMap := make(map[string]StreamInfo)
	for _, a := range active {
		activeMap[a.StreamName] = a
	}
	s.savedMu.Lock()
	defer s.savedMu.Unlock()
	var result []StreamInfo
	for _, name := range s.savedStreams {
		if info, ok := activeMap[name]; ok {
			result = append(result, info)
		} else {
			result = append(result, StreamInfo{StreamName: name, AppName: "live"})
		}
	}
	for _, a := range active {
		found := false
		for _, name := range s.savedStreams {
			if name == a.StreamName {
				found = true
				break
			}
		}
		if !found {
			result = append(result, a)
		}
	}
	return result
}

// AddStreamName 添加房间号
func (s *StreamServiceWrapper) AddStreamName(name string) {
	s.savedMu.Lock()
	defer s.savedMu.Unlock()
	for _, n := range s.savedStreams {
		if n == name {
			return
		}
	}
	s.savedStreams = append(s.savedStreams, name)
}

// DeleteStreamName 删除房间号
func (s *StreamServiceWrapper) DeleteStreamName(name string) {
	s.savedMu.Lock()
	defer s.savedMu.Unlock()
	var filtered []string
	for _, n := range s.savedStreams {
		if n != name {
			filtered = append(filtered, n)
		}
	}
	s.savedStreams = filtered
}

// GetStreamPullers 获取拉流客户端
func (s *StreamServiceWrapper) GetStreamPullers(streamName string) []PullerInfo {
	return s.server.getStreamPullers(streamName)
}

// StopPushSession 踢掉推流
func (s *StreamServiceWrapper) StopPushSession(streamName string) map[string]interface{} {
	if s.server.useSrs {
		return map[string]interface{}{"success": false, "message": "SRS 暂不支持踢流"}
	}
	url := fmt.Sprintf("http://127.0.0.1:8084/api/ctrl/kick_session?stream_name=%s", streamName)
	resp, err := httpGet(url)
	if err != nil {
		return map[string]interface{}{"success": false, "message": err.Error()}
	}
	defer resp.Body.Close()
	return map[string]interface{}{"success": true, "message": "已断开推流"}
}

// PushStreamToDevices 推送流到设备
func (s *StreamServiceWrapper) PushStreamToDevices(streamName string, deviceIPs []string) map[string]interface{} {
	if s.cm != nil && s.cm.GetDeployMode() == "public" {
		return map[string]interface{}{"success": false, "message": "公网模式不支持转发流分发"}
	}
	localIP := getLocalIP()
	if localIP == "" {
		return map[string]interface{}{"success": false, "message": "无法获取本机IP"}
	}
	httpflvURL := fmt.Sprintf("http://%s:8083/live/%s.flv", localIP, streamName)
	for _, ip := range deviceIPs {
		go func(deviceIP string) {
			setURL := fmt.Sprintf("http://%s/modifydev?cmd=4&type=webrtc&path=%s&resolution=1", streamDeviceAddr(deviceIP), httpflvURL)
			httpGetToDevice(setURL)
			startURL := fmt.Sprintf("http://%s/camera?cmd=start", streamDeviceAddr(deviceIP))
			httpGetToDevice(startURL)
		}(ip)
	}
	return map[string]interface{}{"success": true, "message": fmt.Sprintf("已向 %d 台设备发送推流指令", len(deviceIPs))}
}

// StopDevicePlay 停止设备播放
func (s *StreamServiceWrapper) StopDevicePlay(deviceIPs []string) map[string]interface{} {
	for _, ip := range deviceIPs {
		go func(deviceIP string) {
			stopURL := fmt.Sprintf("http://%s/camera?cmd=stop", streamDeviceAddr(deviceIP))
			httpGetToDevice(stopURL)
		}(ip)
	}
	return map[string]interface{}{"success": true, "message": "已发送停止指令"}
}

// GetWebRTCPlayURL 生成 WebRTC 播放地址
func (s *StreamServiceWrapper) GetWebRTCPlayURL(streamName string, useSDP bool) string {
	localIP := getLocalIP()
	if localIP == "" {
		localIP = "localhost"
	}
	if useSDP {
		return fmt.Sprintf("http://%s:1985/rtc/v1/whep/?app=live&stream=%s", localIP, streamName)
	}
	return fmt.Sprintf("webrtc://%s:1985/live/%s", localIP, streamName)
}

// GetWebRTCPushURL 生成 WebRTC 推流地址
func (s *StreamServiceWrapper) GetWebRTCPushURL(streamName string) string {
	localIP := getLocalIP()
	if localIP == "" {
		localIP = "localhost"
	}
	return fmt.Sprintf("http://%s:1985/rtc/v1/whip/?app=live&stream=%s", localIP, streamName)
}

// ParseWebRTCURL 解析 webrtc:// URL 为 HTTP 信令地址
func (s *StreamServiceWrapper) ParseWebRTCURL(webrtcURL string) (string, error) {
	if !strings.HasPrefix(webrtcURL, "webrtc://") {
		return "", fmt.Errorf("invalid webrtc URL: must start with webrtc://")
	}
	remainder := strings.TrimPrefix(webrtcURL, "webrtc://")
	parts := strings.SplitN(remainder, "/", 2)
	if len(parts) < 2 {
		return "", fmt.Errorf("invalid webrtc URL format: missing path")
	}
	hostPort := parts[0]
	path := parts[1]
	if !strings.Contains(hostPort, ":") {
		hostPort += ":1985"
	}
	pathParts := strings.Split(strings.Trim(path, "/"), "/")
	if len(pathParts) < 2 {
		return "", fmt.Errorf("invalid webrtc URL path: must be app/stream")
	}
	app := pathParts[0]
	stream := strings.Join(pathParts[1:], "/")
	return fmt.Sprintf("http://%s/rtc/v1/whep/?app=%s&stream=%s", hostPort, app, stream), nil
}

// StartP2P 启动 P2P 推流
func (s *StreamServiceWrapper) StartP2P(deviceIP string, port int, streamName string, listenPort int) map[string]interface{} {
	if s.cm != nil && s.cm.GetDeployMode() == "public" {
		return map[string]interface{}{"success": false, "message": "公网模式不支持 P2P 推流"}
	}
	if listenPort <= 0 {
		return map[string]interface{}{"success": false, "message": "listen port required"}
	}
	if streamName == "" {
		streamName = fmt.Sprintf("p2p-%d", listenPort)
	}
	// srt 地址必须面向目标设备可达（TUN 代理机器上 getLocalIP 可能返回 198.18.x）
	streamURL := fmt.Sprintf("srt://%s:%d", localIPForDevice(deviceIP), listenPort)
	result := s.p2pManager.Start(deviceIP, port, streamName, streamURL, listenPort)
	if result["success"] == true {
		result["listenUrl"] = streamURL
	}
	return result
}

// StopP2P 停止 P2P 推流
func (s *StreamServiceWrapper) StopP2P(deviceIP string, port int, streamName string) map[string]interface{} {
	return s.p2pManager.Stop(deviceIP, port, streamName)
}

// GetP2PStatus 查询 P2P 状态
func (s *StreamServiceWrapper) GetP2PStatus(deviceIP string, port int, streamName string) map[string]interface{} {
	return s.p2pManager.Status(deviceIP, port, streamName)
}

// StartWindowsPusher 启动摄像头推流
func (s *StreamServiceWrapper) StartWindowsPusher(args []string) map[string]interface{} {
	if s.cm != nil && s.cm.GetDeployMode() == "public" {
		return map[string]interface{}{"success": false, "message": "公网模式不支持摄像头推流"}
	}
	return startWindowsPusher(args)
}

// StopWindowsPusher 停止摄像头推流
func (s *StreamServiceWrapper) StopWindowsPusher(id int) map[string]interface{} {
	return stopWindowsPusher(id)
}

// CapturePreviewFrame 抓取摄像头预览帧
func (s *StreamServiceWrapper) CapturePreviewFrame(camName string, width, height int) map[string]interface{} {
	return capturePreviewFrame(camName, width, height)
}

// ListCameraDevices 列出可用摄像头
func (s *StreamServiceWrapper) ListCameraDevices() map[string]interface{} {
	return listCameraDevices()
}

// ProxyHttpGet 代理 HTTP GET 请求（解决跨域问题）
func (s *StreamServiceWrapper) ProxyHttpGet(url string) (string, error) {
	resp, err := httpGet(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := readBody(resp)
	if err != nil {
		return "", err
	}
	return body, nil
}
