package main

import (
	"fmt"
	"sync"
	"time"

	"github.com/moyunteng/myt/internal/config"
	"github.com/moyunteng/myt/internal/sdkapi"
	"github.com/moyunteng/myt/internal/util"
)

// --- 批量操作 ---

func (s *SdkApiServiceWrapper) BatchRestart(deviceIPs []string, names []string) []sdkapi.BatchOpResult {
	results := s.batchOp(deviceIPs, names, func(c *sdkapi.Client, name string) error {
		return c.RestartAndroid(name)
	})
	s.notifyAndroidsChanged(deviceIPs)
	return results
}

func (s *SdkApiServiceWrapper) BatchStart(deviceIPs []string, names []string) []sdkapi.BatchOpResult {
	results := s.batchOp(deviceIPs, names, func(c *sdkapi.Client, name string) error {
		return c.StartAndroid(name)
	})
	s.notifyAndroidsChanged(deviceIPs)
	return results
}

func (s *SdkApiServiceWrapper) BatchStop(deviceIPs []string, names []string) []sdkapi.BatchOpResult {
	results := s.batchOp(deviceIPs, names, func(c *sdkapi.Client, name string) error {
		return c.StopAndroid(name)
	})
	s.notifyAndroidsChanged(deviceIPs)
	return results
}

func (s *SdkApiServiceWrapper) BatchDelete(deviceIPs []string, names []string) []sdkapi.BatchOpResult {
	results := s.batchOp(deviceIPs, names, func(c *sdkapi.Client, name string) error {
		return c.DeleteAndroid(name)
	})
	s.notifyAndroidsChanged(deviceIPs)
	return results
}

func (s *SdkApiServiceWrapper) notifyAndroidsChanged(deviceIPs []string) {
	if s.onAndroidsChanged != nil {
		util.GoSafe(func() { s.onAndroidsChanged(deviceIPs) })
	}
}

// notifyStructureChanged 结构变更通知（move/rename）：坑位号/容器名变更没有
// docker lifecycle 事件（WS 通道无从感知），回调方必须在 local 模式下也查询。
func (s *SdkApiServiceWrapper) notifyStructureChanged(deviceIPs []string) {
	if s.onStructureChanged != nil {
		util.GoSafe(func() { s.onStructureChanged(deviceIPs) })
	}
}

func (s *SdkApiServiceWrapper) batchOp(deviceIPs []string, names []string, op func(*sdkapi.Client, string) error) []sdkapi.BatchOpResult {
	results := make([]sdkapi.BatchOpResult, len(deviceIPs))
	var wg sync.WaitGroup
	for i, ip := range deviceIPs {
		if i >= len(names) {
			break
		}
		wg.Add(1)
		go func(idx int, deviceIP string, name string) {
			defer wg.Done()
			// 批量操作用 30 秒超时，公网网关转发 + 设备操作可能较慢
			client := s.newClientWithTimeout(deviceIP, 30*time.Second)
			err := op(client, name)
			results[idx] = sdkapi.BatchOpResult{
				DeviceID: deviceIP,
				Name:     name,
				Success:  err == nil,
				Error:    errToString(err),
			}
		}(i, ip, names[i])
	}
	wg.Wait()
	return results
}

func errToString(err error) string {
	if err == nil {
		return ""
	}
	return fmt.Sprintf("错误: %v", err)
}

// convertServicePortBindings 将 sdkapi.PortBindings 转换为 config.PortBindings
func convertServicePortBindings(src map[string][]sdkapi.PortBinding) map[string][]config.PortBinding {
	if len(src) == 0 {
		return nil
	}
	dst := make(map[string][]config.PortBinding, len(src))
	for k, v := range src {
		bindings := make([]config.PortBinding, len(v))
		for i, b := range v {
			bindings[i] = config.PortBinding{HostIP: b.HostIP, HostPort: b.HostPort}
		}
		dst[k] = bindings
	}
	return dst
}
