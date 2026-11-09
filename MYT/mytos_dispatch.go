package main

import (
	"github.com/moyunteng/myt/internal/config"
	"github.com/moyunteng/myt/internal/mirror"
	"github.com/moyunteng/myt/internal/mytos"
	"github.com/moyunteng/myt/internal/mytos/container"
	"github.com/moyunteng/myt/internal/mytos/emulator"
	"github.com/moyunteng/myt/internal/ports"
)

// newMytosClientForAndroid 根据云机镜像类型创建对应的 mytos 客户端。
//
// 镜像类型由 AndroidInfo.Image URL 反查镜像缓存决定：
//   - sys_ver=="5"  → 模拟器（emulator.Client）
//   - sys_ver!="5"  → 容器（container.Client）
//
// 缓存未命中时默认使用模拟器客户端（保持历史行为）。
//
// 注意：模拟器与容器是两套独立实现，互不共享代码。即使 URL 当前一致，
// 参数与返回字段也可能随版本演进出现差异，不能假设通用。
func newMytosClientForAndroid(mirrorSvc *MirrorServiceWrapper, android *config.AndroidInfo, host string, port int) mytos.Client {
	if isContainerImageURL(mirrorSvc, android.Image) {
		return container.NewClient(host, port)
	}
	return emulator.NewClient(host, port)
}

// newPublicMytosClientForAndroid 公网模式创建 mytos 客户端（走网关透传）。
// deviceID 为目标设备 ID；token 为网关 access_token。
// 根据 android 信息判断桥接/非桥接，网关据此路由到正确的容器端口。
func newPublicMytosClientForAndroid(mirrorSvc *MirrorServiceWrapper, android *config.AndroidInfo, deviceID, token string) mytos.Client {
	isContainer := isContainerImageURL(mirrorSvc, android.Image)
	bridge := 0
	if ports.IsMacvlanDirect(android.NetworkName, android.PortBindings) {
		bridge = 1
	}
	if isContainer {
		return container.NewPublicClient(deviceID, token, android.IndexNum, bridge)
	}
	return emulator.NewPublicClient(deviceID, token, android.IndexNum, bridge)
}

// isContainerImageURL 判断给定镜像 URL 是否为容器镜像（非模拟器类型）。
// 仅查在线镜像缓存；未命中返回 false（按模拟器处理）。
func isContainerImageURL(mirrorSvc *MirrorServiceWrapper, imageURL string) bool {
	if imageURL == "" || mirrorSvc == nil {
		return false
	}
	for _, m := range mirrorSvc.GetCachedMirrors() {
		if m.URL == imageURL {
			return !mirror.IsEmulatorMirror(m)
		}
	}
	return false
}
