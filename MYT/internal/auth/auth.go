package auth

import (
	"errors"
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	"github.com/moyunteng/myt/internal/device/protocol"
)

// ErrNotLoggedIn 未登录魔云腾账号：没有可推送的授权 token。
//
// 授权必须来自**用户自己的账号**——本包不再内置固定账号兜底（旧版移植时曾硬编码
// syncUsername/syncPassword，与旧客户端行为不一致：旧客户端始终由用户登录后才有 token）。
var ErrNotLoggedIn = errors.New("未登录魔云腾账号")

const syncInterval = 60 * time.Second

// AuthService 授权同步：把「当前登录账号」的 token 经 UDP 推给在线设备。
// 本包不持有账号与凭证——token 由上层注入（paymentSvc.GetToken）。
type AuthService struct {
	mu sync.Mutex

	// deviceIPsGetter 取在线设备 IP（上层注入 cm.GetOnlineDeviceIPs）
	deviceIPsGetter func() []string
	// tokenGetter 取当前登录账号的 token（上层注入 paymentSvc.GetToken）。
	// 自动同步与手动同步**共用**它——两条路径若推不同账号，
	// 设备端「保存 token 到本地」会让先推的被后推的覆盖。
	tokenGetter func() (string, error)

	stopCh  chan struct{}
	running bool
	wg      sync.WaitGroup
}

func NewAuthService() *AuthService {
	return &AuthService{}
}

func (a *AuthService) SyncTokenToDevices(token string, deviceIPs []string) error {
	conn, err := net.ListenPacket("udp4", "0.0.0.0:0")
	if err != nil {
		return fmt.Errorf("创建UDP连接失败: %w", err)
	}
	defer conn.Close()

	msg := []byte("lgtoken:" + token)
	var wg sync.WaitGroup

	for _, ip := range deviceIPs {
		wg.Add(1)
		go func(targetIP string) {
			defer wg.Done()
			addr, err := net.ResolveUDPAddr("udp4", fmt.Sprintf("%s:%d", targetIP, protocol.Port()))
			if err != nil {
				return
			}
			_, _ = conn.WriteTo(msg, addr)
		}(ip)
	}
	wg.Wait()

	return nil
}

func (a *AuthService) SetDeviceIPsGetter(getter func() []string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.deviceIPsGetter = getter
}

// SetTokenGetter 注入「当前登录账号」的 token 来源（上层注入 paymentSvc.GetToken）。
// 未登录时同步整体跳过（不推送、不做任何兜底）。
func (a *AuthService) SetTokenGetter(getter func() (string, error)) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.tokenGetter = getter
}

func (a *AuthService) StartAutoSync() {
	a.mu.Lock()
	if a.running {
		a.mu.Unlock()
		return
	}
	a.stopCh = make(chan struct{})
	a.running = true
	a.wg.Add(1)
	a.mu.Unlock()

	go a.syncAuth()
}

func (a *AuthService) StopAutoSync() {
	a.mu.Lock()
	if !a.running {
		a.mu.Unlock()
		return
	}
	close(a.stopCh)
	a.running = false
	a.mu.Unlock()

	a.wg.Wait()
}

func (a *AuthService) syncAuth() {
	defer a.wg.Done()

	a.doSyncWithRetry()

	ticker := time.NewTicker(syncInterval)
	defer ticker.Stop()

	for {
		select {
		case <-a.stopCh:
			return
		case <-ticker.C:
			a.doSyncWithRetry()
		}
	}
}

func (a *AuthService) doSyncWithRetry() {
	backoff := time.Second
	maxBackoff := 30 * time.Second
	for {
		if a.doSync() {
			return
		}
		select {
		case <-a.stopCh:
			return
		case <-time.After(backoff):
		}
		log.Printf("[auth] 同步失败，%v 后重试...", backoff)
		if backoff < maxBackoff {
			backoff *= 2
		}
	}
}

// SyncOnce 立即执行一次同步：自动同步的每一轮、以及前端「授权同步」按钮都走这里，
// 返回推送到的设备数（0 = 暂无在线设备）。
//
// 未登录返回 ErrNotLoggedIn：不推送、不兜底（授权只能来自用户自己的账号）。
// UDP 无应答：台数只代表**已发出**，不代表设备已生效——调用方文案须按此措辞。
func (a *AuthService) SyncOnce() (int, error) {
	a.mu.Lock()
	tokenGetter := a.tokenGetter
	ipsGetter := a.deviceIPsGetter
	a.mu.Unlock()

	if tokenGetter == nil {
		return 0, ErrNotLoggedIn
	}
	token, err := tokenGetter()
	if err != nil {
		return 0, fmt.Errorf("%w: %v", ErrNotLoggedIn, err)
	}
	if token == "" {
		return 0, ErrNotLoggedIn
	}

	var ips []string
	if ipsGetter != nil {
		ips = ipsGetter()
	}
	if len(ips) == 0 {
		return 0, nil
	}

	if err := a.SyncTokenToDevices(token, ips); err != nil {
		return 0, err
	}
	return len(ips), nil
}

// doSync 自动同步的单轮：返回「本轮是否处理完毕」（供 syncAuth 决定要不要退避重试）。
// 未登录不算失败——重试没有意义，等用户登录后下一轮自然恢复。
func (a *AuthService) doSync() bool {
	n, err := a.SyncOnce()
	if errors.Is(err, ErrNotLoggedIn) {
		log.Printf("[auth] %v，跳过本轮授权同步", ErrNotLoggedIn)
		return true
	}
	if err != nil {
		log.Printf("[auth] 同步失败: %v", err)
		return false
	}
	if n == 0 {
		log.Println("[auth] 同步就绪，暂无在线设备")
		return true
	}
	log.Printf("[auth] token已推送到 %d 台设备", n)
	return true
}
