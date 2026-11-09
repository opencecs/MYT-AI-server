package main

import (
	"log"

	"github.com/moyunteng/myt/internal/config"
	"github.com/moyunteng/myt/internal/mirror"
	"github.com/moyunteng/myt/internal/util"
)

type MirrorServiceWrapper struct {
	service       *mirror.Service
	configManager *config.Manager
}

func NewMirrorServiceWrapper(s *mirror.Service, cm *config.Manager) *MirrorServiceWrapper {
	return &MirrorServiceWrapper{service: s, configManager: cm}
}

// SetTokenGetter 注入登录 token 提供者（V1 镜像接口分类字段依赖）
func (s *MirrorServiceWrapper) SetTokenGetter(fn func() (string, error)) {
	s.service.SetTokenGetter(fn)
}

// LoadCache 从 mirror_cache.json 加载镜像缓存到内存，启动时调用
func (s *MirrorServiceWrapper) LoadCache() {
	cache := s.configManager.GetMirrorCache()
	if len(cache) > 0 {
		s.service.SetCache(cache)
		log.Printf("加载镜像缓存: %d 条", len(cache))
	}
}

func (s *MirrorServiceWrapper) FetchMirrors() ([]util.MirrorItem, error) {
	result, err := s.service.FetchMirrors()
	if err == nil {
		s.configManager.SetMirrorCache(result)
	}
	return result, err
}

func (s *MirrorServiceWrapper) GetCachedMirrors() []util.MirrorItem {
	return s.service.GetCachedMirrors()
}

// GetCachedContainerMirrors 返回缓存中的容器类型镜像
func (s *MirrorServiceWrapper) GetCachedContainerMirrors() []util.MirrorItem {
	return s.service.GetCachedContainerMirrors()
}

// GetCachedEmulatorMirrors 返回缓存中的模拟器专用镜像（sys_ver=="5"）
func (s *MirrorServiceWrapper) GetCachedEmulatorMirrors() []util.MirrorItem {
	return s.service.GetCachedEmulatorMirrors()
}

func (s *MirrorServiceWrapper) StartAutoRefresh() {
	s.service.SetOnRefresh(func(items []util.MirrorItem) {
		s.configManager.SetMirrorCache(items)
	})
	s.service.StartAutoRefresh()
}

func (s *MirrorServiceWrapper) StopAutoRefresh() {
	s.service.StopAutoRefresh()
}
