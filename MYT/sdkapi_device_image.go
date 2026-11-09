package main

import (
	"sync"
	"time"

	"github.com/moyunteng/myt/internal/config"
	"github.com/moyunteng/myt/internal/sdkapi"
)

type DiskInfo struct {
	DeviceIP string `json:"deviceIP"`
	MMCUse   int64  `json:"mmcUse"`
	MMCTotal int64  `json:"mmcTotal"`
}

// SetV2DeviceInfoGetter 注入 v2 模式设备信息回调（setup：转发 myt_sdk :81/systeminfo）
func (s *SdkApiServiceWrapper) SetV2DeviceInfoGetter(fn func(ip string) (*sdkapi.DeviceInfo, error)) {
	s.v2DeviceInfoGetter = fn
}

// SetV2AutoInstallTrigger 注入 v2 SDK 自动安装触发回调（setup：转发到
// V2ServiceWrapper.maybeAutoInstallV2Sdk——版本查询失败且判定 /info 不可用时
// 按状态机决策是否触发后台安装；注入而非直接引用，避免 main 包内 wrapper 互相依赖）
func (s *SdkApiServiceWrapper) SetV2AutoInstallTrigger(fn func(ip, trigger string)) {
	s.v2AutoInstallTrigger = fn
}

// SetV2AutoInstallReset 注入 v2 SDK 自动安装状态机重置回调（setup：转发
// V2ServiceWrapper.ResetV2SdkAutoState——手动安装成功后清失败计数/放弃标记，
// 自动链路重新可用）
func (s *SdkApiServiceWrapper) SetV2AutoInstallReset(fn func(ip string)) {
	s.v2AutoInstallReset = fn
}

func (s *SdkApiServiceWrapper) GetDeviceInfo(deviceIP string) (*sdkapi.DeviceInfo, error) {
	// v2 模式：V2 主机无 8000 SDK API，设备信息走注入的 systeminfo 回调
	if s.cm != nil && s.cm.GetDeployMode() == "v2" && s.v2DeviceInfoGetter != nil {
		return s.v2DeviceInfoGetter(deviceIP)
	}
	// v4 模式：设备信息接口是 V3 SDK 的 /info/device（8000），V4 主机只监听 8088。
	// 明确报错而不是让 newClient 去拨 8088 的 /info/device 拿到 404 文本。
	if s.isV4Mode() {
		return nil, v4NotSupported("设备信息")
	}
	client := s.newClient(deviceIP)
	return client.GetDeviceInfo()
}

// GetNetworkInfo 已迁移至 VpcServiceWrapper，避免 Wails methodID 冲突

func (s *SdkApiServiceWrapper) GetSSHInfo(deviceIP string) (*sdkapi.SSHInfo, error) {
	client := s.newClient(deviceIP)
	return client.GetSSHInfo()
}

func (s *SdkApiServiceWrapper) ToggleSSH(deviceIP string, enable bool) error {
	client := s.newClient(deviceIP)
	return client.ToggleSSH(enable)
}

func (s *SdkApiServiceWrapper) RestartDevice(deviceIP string) error {
	client := s.newClient(deviceIP)
	return client.RestartDevice()
}

func (s *SdkApiServiceWrapper) GetAPIVersion(deviceIP string) (string, error) {
	client := s.newClient(deviceIP)
	return client.GetAPIVersion()
}

func (s *SdkApiServiceWrapper) GetAPIVersionInfo(deviceIP string) (*sdkapi.APIVersionInfo, error) {
	// v2 模式：设备 8000 /info 版本号是 string，走 v2 专用查询（跳过 int 版本
	// 缓存/过期判定——那套逻辑会把 string 版本折成 0）
	if s.cm != nil && s.cm.GetDeployMode() == "v2" {
		info, err := s.sdkCheckUpgradeV2(deviceIP)
		if err != nil && s.v2AutoInstallTrigger != nil {
			// /info 访问不了（连接拒绝/404/非 JSON/超时）→ 异步决策自动安装，
			// 不阻塞本次查询返回；状态机保证不重复触发/重试上限/彻底放弃
			hostIP := s.resolveDeviceIP(deviceIP)
			s.v2AutoInstallTrigger(hostIP, "version-query")
		}
		return info, err
	}
	client := s.newClient(deviceIP)
	info, err := client.GetAPIVersionInfo()
	if err != nil {
		return nil, err
	}
	s.notifySdkExpired(deviceIP, info.CurrentVersion) // 查到过期版本（<208）= 老 SDK 设备 HTTP 可达：即时恢复（setup 钩子）
	s.cacheSdkVersion(deviceIP, info.CurrentVersion)  // HTTP 查询结果反哺后端缓存：升级轮询下轮免探测
	return info, nil
}

// cacheSdkVersion HTTP 查到的 SDK 版本落入 DeviceDetail 缓存（hello 之外的第二来源，
// 老 SDK 设备无 WS，其版本只有 HTTP 查询能拿到）。与 hello 写入同字段，幂等。
func (s *SdkApiServiceWrapper) cacheSdkVersion(deviceIP string, ver int) {
	if s.cm == nil || deviceIP == "" || ver <= 0 {
		return
	}
	deviceID := s.GetDeviceIDByIP(deviceIP)
	if deviceID == "" {
		return
	}
	existing := s.cm.GetDeviceDetail(deviceID)
	if existing == nil {
		existing = &config.DeviceDetail{DeviceID: deviceID}
	}
	if existing.SDKVersion == ver {
		return // 值未变不写（避免每台每查都标脏落档）
	}
	existing.SDKVersion = ver
	s.cm.SetDeviceDetail(deviceID, existing)
}

func (s *SdkApiServiceWrapper) ChangeSSHPassword(deviceIP, password string) error {
	client := s.newClient(deviceIP)
	return client.ChangeSSHPassword(password)
}

func (s *SdkApiServiceWrapper) ToggleSSHRoot(deviceIP string, enable bool) error {
	client := s.newClient(deviceIP)
	return client.ToggleSSHRoot(enable)
}

// GetDevicesDiskInfo 批量获取磁盘信息
func (s *SdkApiServiceWrapper) GetDevicesDiskInfo(deviceIPs []string) []DiskInfo {
	results := make([]DiskInfo, len(deviceIPs))
	var wg sync.WaitGroup
	for i, ip := range deviceIPs {
		wg.Add(1)
		go func(deviceIP string, pos int) {
			defer wg.Done()
			client := s.newClient(deviceIP)
			info, err := client.GetDeviceInfo()
			if err == nil && info != nil {
				results[pos] = DiskInfo{DeviceIP: deviceIP, MMCUse: info.MMCUse * 1024 * 1024, MMCTotal: info.MMCTotal * 1024 * 1024}
			} else {
				results[pos] = DiskInfo{DeviceIP: deviceIP}
			}
		}(ip, i)
	}
	wg.Wait()
	return results
}

// --- 镜像管理 ---

func (s *SdkApiServiceWrapper) ListImages(deviceIP string) ([]sdkapi.ImageInfo, error) {
	client := s.newClient(deviceIP)
	return client.ListImages()
}

func (s *SdkApiServiceWrapper) PullImage(deviceIP, imageURL string) error {
	client := s.newClient(deviceIP)
	return client.PullImage(imageURL)
}

// PullImageCancelable 可取消的在线镜像拉取（create 等前端编排长任务用）。
// 与 PullImage 行为一致，额外通过 taskID 注册可取消 context；
// 前端 CancelTaskOp(taskID) 可中断在途 HTTP（public 模式经网关的 pullImage 亦可中断）。
func (s *SdkApiServiceWrapper) PullImageCancelable(deviceIP, imageURL, taskID string) error {
	ctx := s.registerCancel(taskID)
	defer s.unregisterCancel(taskID)
	// 在线镜像拉取耗时可达分钟级（百 MB 镜像），8s 默认超时会在拉取途中断开连接、
	// 取消设备端拉取任务。public 模式经 Go 代理必须用长超时（local 模式浏览器直连 SSE 不受影响）。
	client := s.newClientWithTimeout(deviceIP, 10*time.Minute)
	client.SetContext(ctx)
	return client.PullImage(imageURL)
}

func (s *SdkApiServiceWrapper) DeleteImage(deviceIP, image string) error {
	client := s.newClient(deviceIP)
	return client.DeleteImage(image)
}

func (s *SdkApiServiceWrapper) SwitchImage(deviceIP, name, imageURL, modelID, modelName, localModel, dns, sandboxSize, width, height, dpi string, enforce bool, mgenable, gmsenable, countryCode, mytBridgeName, macVlanIp string) error {
	client := s.newClient(deviceIP)
	return client.SwitchImage(sdkapi.SwitchImageRequest{
		Name: name, ImageURL: imageURL,
		ModelID: modelID, ModelName: modelName, LocalModel: localModel,
		DNS: dns, SandboxSize: sandboxSize,
		DoboxWidth: width, DoboxHeight: height, DoboxDpi: dpi,
		Enforce: enforce, Mgenable: mgenable, Gmsenable: gmsenable,
		CountryCode: countryCode, MytBridgeName: mytBridgeName, MacVlanIp: macVlanIp,
	})
}

// --- 备份管理 ---

func (s *SdkApiServiceWrapper) ListBackups(deviceIP string) ([]sdkapi.BackupInfo, error) {
	client := s.newClient(deviceIP)
	return client.ListBackups()
}

func (s *SdkApiServiceWrapper) DeleteBackup(deviceIP, filename string) error {
	client := s.newClient(deviceIP)
	return client.DeleteBackup(filename)
}
