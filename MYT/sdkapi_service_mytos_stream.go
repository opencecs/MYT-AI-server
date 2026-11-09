package main

import (
	"encoding/base64"
	"fmt"
	"os"
	"strings"

	"github.com/moyunteng/myt/internal/config"
	"github.com/moyunteng/myt/internal/mytos"
	"github.com/moyunteng/myt/internal/ports"
)

// --- MYTOS 相关操作（推流 / S5代理 / GPS / 摇一摇 / Google证书）---
// 这些操作走容器内 9082 端口的 MYTOS HTTP 接口，统一经 getMytosClientForAndroid 转发。

// StreamConnectionInfo APP扫码连接参数（供前端生成二维码）
type StreamConnectionInfo struct {
	Name        string `json:"name"`        // 云机名称（处理后）
	IP          string `json:"ip"`          // 设备或容器 IP
	PortVideo   int    `json:"portVideo"`   // 10000 视频端口
	PortControl int    `json:"portControl"` // 10001 控制端口
	PortMYTOS   int    `json:"portMYTOS"`   // 9082 API 端口
	PortCamTCP  int    `json:"portCamTCP"`  // 10006 摄像头 TCP 端口
	PortCamUDP  int    `json:"portCamUDP"`  // 10007 摄像头 UDP 端口
}

// GetStreamConnectionInfo 获取云机的推流连接参数（供前端生成 APP 扫码二维码）
func (s *SdkApiServiceWrapper) GetStreamConnectionInfo(deviceIP, androidName string) (*StreamConnectionInfo, error) {
	if s.cm == nil {
		return nil, fmt.Errorf("配置管理器未初始化")
	}
	var android *config.AndroidInfo
	for _, a := range s.cm.GetAndroidList() {
		if a.Name == androidName && (a.IP == deviceIP || a.DeviceID == deviceIP) {
			android = &a
			break
		}
	}
	if android == nil {
		return nil, fmt.Errorf("未找到云机 %s@%s", androidName, deviceIP)
	}

	// 处理容器名称：如果前缀是长 UUID，替换为随机4位数
	name := android.Name
	if idx := strings.Index(name, "_"); idx > 0 && idx > 10 {
		parts := strings.SplitN(name, "_", 2)
		if len(parts) >= 2 {
			name = parts[1]
		}
	}

	// 确定连接 IP
	ip := deviceIP
	if ports.IsMacvlanDirect(android.NetworkName, android.PortBindings) && android.ContainerIP != "" {
		ip = android.ContainerIP
	}
	if strings.Contains(ip, ":") {
		ip = strings.SplitN(ip, ":", 2)[0]
	}

	// 解析各端口
	var addrMYTOS, addrVideo, addrControl, addrCamTCP, addrCamUDP ports.Addr
	if ports.IsMacvlanDirect(android.NetworkName, android.PortBindings) && android.ContainerIP != "" {
		addrMYTOS = ports.MYTOSDirect(*android)
		addrVideo = ports.VideoDirect(*android)
		addrControl = ports.ControlDirect(*android)
		addrCamTCP = ports.CameraTCPDirect(*android)
		addrCamUDP = ports.CameraUDPDirect(*android)
	} else {
		// 主机地址用 android.IP：v2/public 下传入的 deviceIP 可能是序列号，不能当主机名连
		addrMYTOS = ports.MYTOS(*android, android.IP)
		addrVideo = ports.Video(*android, android.IP)
		addrControl = ports.Control(*android, android.IP)
		addrCamTCP = ports.CameraTCP(*android, android.IP)
		addrCamUDP = ports.CameraUDP(*android, android.IP)
	}

	return &StreamConnectionInfo{
		Name:        name,
		IP:          ip,
		PortVideo:   addrVideo.Port,
		PortControl: addrControl.Port,
		PortMYTOS:   addrMYTOS.Port,
		PortCamTCP:  addrCamTCP.Port,
		PortCamUDP:  addrCamUDP.Port,
	}, nil
}

// SetStreamSource 设置虚拟摄像头视频源（图片/视频推流）
func (s *SdkApiServiceWrapper) SetStreamSource(deviceIP, androidName, camType, filePath string) error {
	client, err := s.getMytosClientForAndroid(deviceIP, androidName)
	if err != nil {
		return err
	}
	return client.SetCameraSource(camType, filePath, 0)
}

// StartCamera 开启虚拟摄像头
func (s *SdkApiServiceWrapper) StartCamera(deviceIP, androidName string) error {
	client, err := s.getMytosClientForAndroid(deviceIP, androidName)
	if err != nil {
		return err
	}
	return client.SetCamera("start", "", "")
}

// StopCamera 停止虚拟摄像头
func (s *SdkApiServiceWrapper) StopCamera(deviceIP, androidName string) error {
	client, err := s.getMytosClientForAndroid(deviceIP, androidName)
	if err != nil {
		return err
	}
	return client.SetCamera("stop", "", "")
}

// UploadAndSetStream 上传文件到云机并设置为摄像头视频源
func (s *SdkApiServiceWrapper) UploadAndSetStream(deviceIP, androidName, localFilePath, camType string) error {
	client, err := s.getMytosClientForAndroid(deviceIP, androidName)
	if err != nil {
		return fmt.Errorf("创建客户端失败: %w", err)
	}
	remotePath, err := client.UploadFile(localFilePath)
	if err != nil {
		return fmt.Errorf("上传文件失败: %w", err)
	}
	if err := client.SetCameraSource(camType, remotePath, 0); err != nil {
		return fmt.Errorf("设置摄像头源失败: %w", err)
	}
	if err := client.SetCamera("start", "", ""); err != nil {
		return fmt.Errorf("开启摄像头失败: %w", err)
	}
	return nil
}

// QueryProxy 查询云机 S5 代理状态
func (s *SdkApiServiceWrapper) QueryProxy(deviceIP, androidName string) (map[string]interface{}, error) {
	client, err := s.getMytosClientForAndroid(deviceIP, androidName)
	if err != nil {
		return nil, err
	}
	return client.QueryProxy()
}

// StartProxy 设置云机 S5 代理（proxyType: 1=本地DNS解析, 2=服务端DNS解析）
func (s *SdkApiServiceWrapper) StartProxy(deviceIP, androidName, ip string, port int, usr, pwd string, proxyType int) error {
	client, err := s.getMytosClientForAndroid(deviceIP, androidName)
	if err != nil {
		return err
	}
	return client.StartProxy(ip, port, usr, pwd, proxyType)
}

// StopProxy 关闭云机 S5 代理
func (s *SdkApiServiceWrapper) StopProxy(deviceIP, androidName string) error {
	client, err := s.getMytosClientForAndroid(deviceIP, androidName)
	if err != nil {
		return err
	}
	return client.StopProxy()
}

// SetIPLocate 设置云机 GPS 定位（ip 为定位 IP，language 为语言）
func (s *SdkApiServiceWrapper) SetIPLocate(deviceIP, androidName, ip, language string) error {
	client, err := s.getMytosClientForAndroid(deviceIP, androidName)
	if err != nil {
		return err
	}
	return client.SetIPLocate(ip, language)
}

// Shake 触发云机摇一摇
func (s *SdkApiServiceWrapper) Shake(deviceIP, androidName string) error {
	client, err := s.getMytosClientForAndroid(deviceIP, androidName)
	if err != nil {
		return err
	}
	return client.SetShake(true)
}

// UploadGoogleCert 上传 Google 证书（base64 编码内容，解码到临时文件后上传）
func (s *SdkApiServiceWrapper) UploadGoogleCert(deviceIP, androidName, base64Content string) error {
	data, err := base64.StdEncoding.DecodeString(base64Content)
	if err != nil {
		return fmt.Errorf("证书 base64 解码失败: %w", err)
	}
	tmpFile, err := os.CreateTemp("", "google-cert-*.bin")
	if err != nil {
		return fmt.Errorf("创建临时文件失败: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)
	if _, err := tmpFile.Write(data); err != nil {
		tmpFile.Close()
		return fmt.Errorf("写入临时文件失败: %w", err)
	}
	tmpFile.Close()

	client, err := s.getMytosClientForAndroid(deviceIP, androidName)
	if err != nil {
		return err
	}
	return client.UploadKeybox(tmpPath)
}

// getMytosClientForAndroid 根据设备IP和实例名创建 MYTOS 客户端
func (s *SdkApiServiceWrapper) getMytosClientForAndroid(deviceIP, androidName string) (mytos.Client, error) {
	if s.cm == nil {
		return nil, fmt.Errorf("配置管理器未初始化")
	}
	// v4 模式：MYTOS 通道（容器内 9082/9083 服务）是 V3 容器体系的产物，V4 实例
	// （tobox/QEMU）没有这些端口。提前返回明确错误，避免调用方拿着推算出的
	// 端口去拨号、超时后才报「连接失败」——用户看到的是笼统的网络错误。
	if s.cm.GetDeployMode() == "v4" {
		return nil, v4NotSupported("云机 MYTOS 通道操作")
	}
	var android *config.AndroidInfo
	for _, a := range s.cm.GetAndroidList() {
		if a.Name == androidName && (a.IP == deviceIP || a.DeviceID == deviceIP) {
			android = &a
			break
		}
	}
	if android == nil {
		return nil, fmt.Errorf("未找到云机 %s@%s", androidName, deviceIP)
	}

	// 公网模式：走网关透传
	if s.cm.GetDeployMode() == "public" {
		token, err := s.getPublicToken()
		if err != nil {
			return nil, fmt.Errorf("获取公网 token 失败: %w", err)
		}
		return newPublicMytosClientForAndroid(s.mirrorSvc, android, deviceIP, token), nil
	}

	var addr ports.Addr
	if ports.IsMacvlanDirect(android.NetworkName, android.PortBindings) && android.ContainerIP != "" {
		addr = ports.MYTOSDirect(*android)
	} else {
		// 主机地址用 android.IP：v2/public 下传入的 deviceIP 可能是序列号，不能当主机名连
		addr = ports.MYTOS(*android, android.IP)
	}

	return newMytosClientForAndroid(s.mirrorSvc, android, addr.Host, addr.Port), nil
}
