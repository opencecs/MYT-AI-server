package main

import (
	"fmt"

	"github.com/moyunteng/myt/internal/config"
	"github.com/moyunteng/myt/internal/mytos/container"
	"github.com/moyunteng/myt/internal/ports"
)

// --- 容器机型切换（异步随机设备信息）---
//
// 容器与模拟器完全隔离：容器的"切换机型"走 MYTOS /modifydev?cmd=2 原地修改设备信息（无需重启），
// 模拟器的"切换机型"走 SDK V3 /android/switchModel（需要重启）。两者参数语义完全不同，不复用。
// 这些方法仅对容器镜像生效；若对模拟器调用会返回错误。

// ContainerRandomizeDeviceInfoOptions 对外暴露的容器随机设备信息参数（JSON 友好）
type ContainerRandomizeDeviceInfoOptions struct {
	IsAsync          bool   `json:"isAsync"`
	IsSpecifiedModel bool   `json:"isSpecifiedModel"`
	ModelName        string `json:"modelName"`
	Language         string `json:"language"`
	RandomAbroad     bool   `json:"randomAbroad"`
	ModifyMAC        bool   `json:"modifyMAC"`
	UserIP           string `json:"userIP"`
}

// ContainerRandomizeDeviceInfoResult 容器随机设备信息接口返回
type ContainerRandomizeDeviceInfoResult struct {
	TaskID string                 `json:"taskId"`
	Raw    map[string]interface{} `json:"raw"`
}

// ContainerRandomizeDeviceInfo 容器异步随机设备信息（容器机型切换入口）
func (s *SdkApiServiceWrapper) ContainerRandomizeDeviceInfo(deviceIP, androidName string, opts ContainerRandomizeDeviceInfoOptions) (*ContainerRandomizeDeviceInfoResult, error) {
	cc, err := s.getContainerMytosClient(deviceIP, androidName)
	if err != nil {
		return nil, err
	}
	res, err := cc.RandomizeDeviceInfo(container.RandomizeDeviceInfoOptions{
		IsAsync:          opts.IsAsync,
		IsSpecifiedModel: opts.IsSpecifiedModel,
		ModelName:        opts.ModelName,
		Language:         opts.Language,
		RandomAbroad:     opts.RandomAbroad,
		ModifyMAC:        opts.ModifyMAC,
		UserIP:           opts.UserIP,
	})
	if err != nil {
		return nil, err
	}
	return &ContainerRandomizeDeviceInfoResult{
		TaskID: res.TaskID,
		Raw:    res.Raw,
	}, nil
}

// ContainerQueryRandomizeTask 查询容器异步随机设备信息任务状态
func (s *SdkApiServiceWrapper) ContainerQueryRandomizeTask(deviceIP, androidName, taskID string) (map[string]interface{}, error) {
	cc, err := s.getContainerMytosClient(deviceIP, androidName)
	if err != nil {
		return nil, err
	}
	return cc.QueryRandomizeTask(taskID)
}

// ContainerModelDictionaryEntry 机型字典条目（按厂商分组）
type ContainerModelDictionaryEntry struct {
	Brand  string         `json:"brand"`
	Models map[string]int `json:"models"`
}

// GetContainerModelDictionary 返回 MYTOS 机型字典（容器切换机型使用）
func (s *SdkApiServiceWrapper) GetContainerModelDictionary() []ContainerModelDictionaryEntry {
	dict := container.ListMytosModelDictionary()
	out := make([]ContainerModelDictionaryEntry, 0, len(dict))
	for brand, models := range dict {
		out = append(out, ContainerModelDictionaryEntry{Brand: brand, Models: models})
	}
	return out
}

// IsContainerAndroid 判断指定云机是否为容器镜像（sys_ver != "5"）
func (s *SdkApiServiceWrapper) IsContainerAndroid(deviceIP, androidName string) (bool, error) {
	if s.cm == nil {
		return false, fmt.Errorf("配置管理器未初始化")
	}
	var android *config.AndroidInfo
	for _, a := range s.cm.GetAndroidList() {
		if a.Name == androidName && (a.IP == deviceIP || a.DeviceID == deviceIP) {
			android = &a
			break
		}
	}
	if android == nil {
		return false, fmt.Errorf("未找到云机 %s@%s", androidName, deviceIP)
	}
	return isContainerImageURL(s.mirrorSvc, android.Image), nil
}

// getContainerMytosClient 仅返回容器客户端；若云机是模拟器则报错
func (s *SdkApiServiceWrapper) getContainerMytosClient(deviceIP, androidName string) (*container.Client, error) {
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
	if !isContainerImageURL(s.mirrorSvc, android.Image) {
		return nil, fmt.Errorf("云机 %s@%s 不是容器，无法使用容器机型切换接口（模拟器请使用 /android/switchModel）", androidName, deviceIP)
	}
	var addr ports.Addr
	if ports.IsMacvlanDirect(android.NetworkName, android.PortBindings) && android.ContainerIP != "" {
		addr = ports.MYTOSDirect(*android)
	} else {
		// 主机地址用 android.IP：v2/public 下传入的 deviceIP 可能是序列号，不能当主机名连
		addr = ports.MYTOS(*android, android.IP)
	}
	return container.NewClient(addr.Host, addr.Port), nil
}
