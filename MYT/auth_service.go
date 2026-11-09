package main

import (
	"github.com/moyunteng/myt/internal/auth"
)

// AuthServiceWrapper 授权同步服务包装（薄适配层：状态与逻辑都在 internal/auth，此处只转发）
type AuthServiceWrapper struct {
	service *auth.AuthService
}

func NewAuthServiceWrapper(s *auth.AuthService) *AuthServiceWrapper {
	return &AuthServiceWrapper{service: s}
}

// SetDeviceIPsGetter 注入在线设备 IP 来源（setup.go 注入 cm.GetOnlineDeviceIPs）。
func (s *AuthServiceWrapper) SetDeviceIPsGetter(getter func() []string) {
	s.service.SetDeviceIPsGetter(getter)
}

// SetTokenGetter 注入「当前登录账号」的 token 来源（setup.go 注入 paymentSvc.GetToken）。
//
// auth 包不 import payment（payment 反向已依赖 auth，会成环），故用注入，
// 同 screencast_service.go / internal/cloudapi 的 SetTokenFn 模式。
// 注入后自动同步与手动同步**同源**取 token——两条路径若推不同账号，
// 设备端「保存 token 到本地」会让先推的被后推的覆盖。
func (s *AuthServiceWrapper) SetTokenGetter(getter func() (string, error)) {
	s.service.SetTokenGetter(getter)
}

// SyncNow 手动授权同步：立即推一次，返回推送到的设备数（0 = 暂无在线设备）。
//
// token 来源与自动同步完全一致（都是当前登录账号，逻辑统一在 auth.SyncOnce）。
// 未登录返回「未登录魔云腾账号」——不推送、不兜底。
// UDP 无应答：台数只代表**已发出**，不代表设备已生效——调用方文案须按此措辞。
func (s *AuthServiceWrapper) SyncNow() (int, error) {
	return s.service.SyncOnce()
}

func (s *AuthServiceWrapper) StartAutoSync() {
	s.service.StartAutoSync()
}

func (s *AuthServiceWrapper) StopAutoSync() {
	s.service.StopAutoSync()
}
