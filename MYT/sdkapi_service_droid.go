package main

import (
	"fmt"
	"log"
	"time"

	"github.com/moyunteng/myt/internal/config"
	"github.com/moyunteng/myt/internal/sdkapi"
	"github.com/moyunteng/myt/internal/util"
)

// --- 安卓实例操作 ---

// androidOpTimeout 容器操作超时：设备端 docker rm/stop/restart 同步等待完成，
// 大容器可能耗时几十秒——15s 默认超时会误报「网络请求超时」且设备端仍在执行
// （操作实际成功但显示失败）。
const androidOpTimeout = 60 * time.Second

func (s *SdkApiServiceWrapper) ListAndroids(deviceIP string) ([]config.AndroidInfo, error) {
	// v4 模式：8088 REST /android（主键 backend/id，indexNum 由客户端派生）
	if s.isV4Mode() {
		return s.listAndroidsV4(deviceIP)
	}
	client := s.newClient(deviceIP)
	list, err := client.ListAndroids()
	if err != nil {
		return nil, err
	}
	s.passwordsMu.RLock()
	devID := s.ipToDeviceID[deviceIP]
	s.passwordsMu.RUnlock()
	// public 模式 deviceIP 就是 device_id，ipToDeviceID 查不到，直接用 deviceIP
	if devID == "" {
		devID = deviceIP
	}
	result := make([]config.AndroidInfo, len(list))
	for i, a := range list {
		result[i] = config.AndroidInfo{
			ID: a.ID, Name: a.Name, Status: a.Status, Image: a.Image,
			IndexNum: a.IndexNum, DeviceID: devID, IP: deviceIP,
			NetworkName: a.NetworkName, PortBindings: convertServicePortBindings(a.PortBindings),
			ContainerIP: a.IPAddress,
			Tags:        a.Tags,
		}
		if util.DebugEnabled && deviceIP == "10.10.11.46" {
			// DEBUG: 记录特定设备的原始SDK返回数据
			log.Printf("[sdkapi:DEBUG] ListAndroids原始: ip=%s index=%d id=%s name=%s status=%s networkName=%s containerIP=%s portBindings=%v",
				deviceIP, a.IndexNum, a.ID, a.Name, a.Status, a.NetworkName, a.IPAddress, a.PortBindings)
		}
	}
	return result, nil
}

// ListPhoneModels 获取设备支持的机型列表
func (s *SdkApiServiceWrapper) ListPhoneModels(deviceIP string) ([]sdkapi.PhoneModel, error) {
	// TODO(v4): V4 无机型列表端点（机型由镜像 variant 决定）
	if s.isV4Mode() {
		return nil, v4NotSupported("机型列表")
	}
	client := s.newClient(deviceIP)
	return client.ListPhoneModels()
}

// CreateAndroid 创建安卓实例
func (s *SdkApiServiceWrapper) CreateAndroid(deviceIP, name, imageURL, dns string, indexNum int, modelID, modelName, localModel, sandboxSize, width, height, dpi string, start bool, mytBridgeName, macVlanIp string) error {
	// TODO(v4): V4 有 POST /android/create（参数：name/cpu_cores/memory_mb/disk_mb/
	// backend/variant/三件套路径），但镜像体系（variant 来自 /image/list）与机型、
	// 坑位号语义都不同，需与创建云机弹窗一起设计，先明确报错。
	if s.isV4Mode() {
		return v4NotSupported("创建云机")
	}
	client := s.newClient(deviceIP)
	err := client.CreateAndroid(sdkapi.CreateAndroidRequest{
		Name:          name,
		ImageURL:      imageURL,
		DNS:           dns,
		IndexNum:      indexNum,
		ModelID:       modelID,
		ModelName:     modelName,
		LocalModel:    localModel,
		SandboxSize:   sandboxSize,
		DoboxWidth:    width,
		DoboxHeight:   height,
		DoboxDpi:      dpi,
		Start:         &start,
		MytBridgeName: mytBridgeName,
		MacVlanIp:     macVlanIp,
	})
	if err == nil {
		s.notifyAndroidsChanged([]string{deviceIP})
	}
	return err
}

func (s *SdkApiServiceWrapper) DeleteAndroid(deviceIP, name string) error {
	if s.isV4Mode() {
		return s.deleteAndroidV4(deviceIP, name)
	}
	if util.DebugEnabled {
		log.Printf("[sdkapi:DEBUG] DeleteAndroid: deviceIP=%s name=%s", deviceIP, name)
	}
	client := s.newClientWithTimeout(deviceIP, androidOpTimeout)
	err := client.DeleteAndroid(name)
	if err == nil {
		s.notifyAndroidsChanged([]string{deviceIP})
	}
	return err
}

func (s *SdkApiServiceWrapper) StartAndroid(deviceIP, name string) error {
	if s.isV4Mode() {
		return s.startAndroidV4(deviceIP, name)
	}
	if util.DebugEnabled {
		log.Printf("[sdkapi:DEBUG] StartAndroid: deviceIP=%s name=%s", deviceIP, name)
	}
	client := s.newClientWithTimeout(deviceIP, androidOpTimeout)
	err := client.StartAndroid(name)
	if err == nil {
		s.notifyAndroidsChanged([]string{deviceIP})
	}
	return err
}

func (s *SdkApiServiceWrapper) StopAndroid(deviceIP, name string) error {
	if s.isV4Mode() {
		return s.stopAndroidV4(deviceIP, name)
	}
	if util.DebugEnabled {
		log.Printf("[sdkapi:DEBUG] StopAndroid: deviceIP=%s name=%s", deviceIP, name)
	}
	client := s.newClientWithTimeout(deviceIP, androidOpTimeout)
	err := client.StopAndroid(name)
	if err == nil {
		s.notifyAndroidsChanged([]string{deviceIP})
	}
	return err
}

func (s *SdkApiServiceWrapper) RestartAndroid(deviceIP, name string) error {
	if s.isV4Mode() {
		return s.restartAndroidV4(deviceIP, name)
	}
	client := s.newClientWithTimeout(deviceIP, androidOpTimeout)
	err := client.RestartAndroid(name)
	if err == nil {
		s.notifyAndroidsChanged([]string{deviceIP})
	}
	return err
}

func (s *SdkApiServiceWrapper) RenameAndroid(deviceIP, name, newName string) error {
	// TODO(v4): V4 SDK 无改名端点（InstanceInfo 的 name 由 tebox 侧管理）
	if s.isV4Mode() {
		return v4NotSupported("重命名云机")
	}
	client := s.newClient(deviceIP)
	err := client.RenameAndroid(name, newName)
	if err == nil {
		s.notifyStructureChanged([]string{deviceIP})
	}
	return err
}

func (s *SdkApiServiceWrapper) MoveAndroid(deviceIP, name string, indexNum int) error {
	// TODO(v4): V4 无坑位号概念（实例由 backend 聚合），无对应端点
	if s.isV4Mode() {
		return v4NotSupported("移动坑位")
	}
	client := s.newClientWithTimeout(deviceIP, androidOpTimeout)
	err := client.MoveAndroid(name, indexNum)
	if err == nil {
		s.notifyStructureChanged([]string{deviceIP})
	}
	return err
}

// SetAndroidLabels 设置同设备多个容器的标签（设备端单容器一次一调，逐个写入）。
// 同设备循环写：任一失败立即返回错误（错误信息带容器名，便于定位是哪台写挂了）。
//
// 写入成功后的收敛双保险（2026-10-30 清空标签 UI 不更新修复）：
//  1. 乐观回填：按容器名补丁 configManager 缓存的 Tags（含清空语义：tags=[] 覆盖为空
//     集，nil 不动）——随后推送的 androidlist:updated 携带的列表即含补丁后 tags，
//     前端 setAndroidList 内容比对命中 → androids props 换新 → 卡片/浮层立即收敛。
//     此前仅 ScreenshotView 单点调 PatchAndroidTags 且只 bump 本地版本号（props 不变、
//     ScreenshotCard memo 按 android 内容浅比直接跳过重渲染），清空后 UI 停留旧标签。
//  2. 权威事件：notifyAndroidsChanged 触发容器列表查询，设备端真实 tags 秒级回灌。
//     local 模式无查询（零反馈路径）时也已有第 1 步的补丁值兜底。
func (s *SdkApiServiceWrapper) SetAndroidLabels(deviceIP string, names []string, tags []string) error {
	// TODO(v4): V4 SDK 无容器标签端点（标签是 V3 的坑位概念）
	if s.isV4Mode() {
		return v4NotSupported("设置云机标签")
	}
	if len(names) == 0 {
		return nil
	}
	if tags == nil {
		tags = []string{}
	}
	client := s.newClient(deviceIP)
	for _, name := range names {
		if err := client.SetAndroidLabel(name, tags); err != nil {
			return fmt.Errorf("容器 %s 设置标签失败: %w", name, err)
		}
	}
	// 设备标识解析：deviceIP 在 public 模式实为 device_id，local/v2 模式按缓存
	// 条目的 DeviceID 匹配（GetAndroidsByDevice 按 a.DeviceID == deviceID 过滤）。
	// 标签只影响缓存条目内容，无需触发查询——推送当前缓存即可让前端即时收敛。
	if s.cm != nil {
		deviceID := deviceIP
		if s.cm.GetDevice(deviceIP) != nil {
			deviceID = s.cm.GetDevice(deviceIP).DeviceID
		} else if s.cm.GetDeviceByIP(deviceIP) != nil {
			deviceID = s.cm.GetDeviceByIP(deviceIP).DeviceID
		}
		if hit := s.cm.PatchAndroidTags(deviceID, names, tags); len(hit) > 0 {
			s.emitEvent("androidlist:updated", s.cm.GetAndroidList())
		}
	}
	s.notifyAndroidsChanged([]string{deviceIP})
	return nil
}

// GetAndroidTagDict 拉取本机标签字典（GET /android/tags）。
// 用途：标签写操作成功后的字典兜底刷新（老设备无 WS tagsChanged 事件时的 REST 替代源），
// 前端把返回的扁平全集并入 allTags 聚合源（标签筛选下拉 / 编辑建议）。
func (s *SdkApiServiceWrapper) GetAndroidTagDict(deviceIP string) ([]string, error) {
	if s.isV4Mode() {
		return nil, v4NotSupported("云机标签字典")
	}
	return s.newClient(deviceIP).GetAndroidTagDict()
}
