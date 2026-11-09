package discover

import (
	"context"
	"fmt"
	"log"
	"net"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/moyunteng/myt/internal/device/protocol"
	"github.com/moyunteng/myt/internal/util"
)

type Device struct {
	IP         string `json:"ip"`
	DeviceType string `json:"device_type"`
	DeviceID   string `json:"device_id"`
	DeviceName string `json:"device_name"`
	Version    string `json:"version"`
	Port       int    `json:"port"`
}

const (
	listenTimeout = 2 * time.Second  // 硬上限：无论是否收到回复，最长等这么久
	idleTimeout   = 600 * time.Millisecond // 静默窗口：连续无新报文即提前收工
	resultBufSize = 512
)

// Discoverer 无内部状态：每轮 Discover/DiscoverManually 独立收集并返回本轮
// 结果，不做跨轮缓存（历史上曾有 devices map 做跨轮去重，从未被读取，已删）。
type Discoverer struct {
	// dialPort 覆盖发现目标端口（0 = protocol.Port()）。仅测试注入用，
	// 生产恒为 0——避免测试真机网段的 7678 端口。
	dialPort int
}

func NewDiscoverer() *Discoverer {
	return &Discoverer{}
}

func (d *Discoverer) port() int {
	if d.dialPort != 0 {
		return d.dialPort
	}
	return protocol.Port()
}

// listen 建一个开启 SO_BROADCAST 的 UDP socket
func (d *Discoverer) listen() (net.PacketConn, error) {
	listenConfig := net.ListenConfig{
		Control: func(network, address string, c syscall.RawConn) error {
			return c.Control(func(fd uintptr) {
				setBroadcast(fd)
			})
		},
	}
	return listenConfig.ListenPacket(context.Background(), "udp4", "0.0.0.0:0")
}

func (d *Discoverer) Discover(ctx context.Context) ([]Device, error) {
	addrs := d.collectBroadcastAddresses()
	if len(addrs) == 0 {
		return nil, fmt.Errorf("无可用的广播地址")
	}
	uniqueAddrs := dedupAddresses(addrs)
	util.Debugf("[discover] 广播地址数: raw=%d unique=%d", len(addrs), len(uniqueAddrs))

	conn, err := d.listen()
	if err != nil {
		return nil, fmt.Errorf("创建UDP监听失败: %w", err)
	}
	defer conn.Close()
	util.Debugf("[discover] UDP监听地址: %s", conn.LocalAddr().String())

	found := d.scan(ctx, conn, uniqueAddrs)
	util.Debugf("[discover] 发现设备数: %d", len(found))
	return found, nil
}

// DiscoverManually 对指定 IP 列表定向发送 UDP 魔术字节，返回成功发现的设备和无响应 IP
func (d *Discoverer) DiscoverManually(ctx context.Context, ips []string) ([]Device, []string) {
	if len(ips) == 0 {
		return nil, nil
	}

	conn, err := d.listen()
	if err != nil {
		log.Printf("[discover:manual] 创建UDP监听失败: %v", err)
		return nil, ips
	}
	defer conn.Close()

	found := d.scan(ctx, conn, ips)

	foundMap := make(map[string]bool, len(found))
	for _, dev := range found {
		foundMap[dev.IP] = true
	}

	var failedIPs []string
	for _, ip := range ips {
		if !foundMap[ip] {
			failedIPs = append(failedIPs, ip)
		}
	}

	log.Printf("[discover:manual] 完成: 成功=%d 失败=%d", len(found), len(failedIPs))
	return found, failedIPs
}

// scan 一次发现会话（广播/单播共用）：向 targets 发魔术字节并收回复。
//
// 死锁修复（2026-09-05）：旧实现先 recvWg.Wait() 再 drain resultCh——回复数超过
// 通道容量(512)时 worker 阻塞在发送、Wait 永远等不到 worker 退出，扫描挂死。
// 实测 330 台设备 × 每台 2 包（两条广播各回一包）= 660 合法回复即触发。
// 现在 drain 放后台 goroutine 与收包并发消费，worker 发送永不阻塞。
//
// 静默提前退出：设备回复实测 ~550ms 内到齐，之后到 2s 全是空等。连续
// idleTimeout 无新报文即提前收工（发送完成时记基线，无回复也适用），
// 常规扫描 ~0.7s 完成；listenTimeout 仍是硬上限兜底。
func (d *Discoverer) scan(ctx context.Context, conn net.PacketConn, targets []string) []Device {
	listenCtx, listenCancel := context.WithTimeout(ctx, listenTimeout)
	defer listenCancel()

	var lastActivity atomic.Int64 // 最近一次收到报文的 UnixNano；0 = 发送尚未完成
	resultCh := make(chan Device, resultBufSize)
	var seen sync.Map // DeviceID 去重：同一设备多包回复只入队一次

	// 静默监控：100ms 粒度检查，超时即取消 listenCtx 提前收工
	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for range ticker.C {
			select {
			case <-listenCtx.Done():
				return
			default:
			}
			last := lastActivity.Load()
			if last == 0 {
				continue
			}
			if time.Since(time.Unix(0, last)) >= idleTimeout {
				listenCancel()
				return
			}
		}
	}()

	// 后台 drain：与收包并发消费 resultCh（死锁修复核心）
	var found []Device
	drainDone := make(chan struct{})
	go func() {
		defer close(drainDone)
		for dev := range resultCh {
			found = append(found, dev)
		}
	}()

	recvWorkers := util.ParallelWorkers("udp")
	var recvWg sync.WaitGroup
	for i := 0; i < recvWorkers; i++ {
		recvWg.Add(1)
		go func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("[PANIC] goroutine recovered: %v", r)
				}
			}()
			defer recvWg.Done()
			d.recvReplies(listenCtx, conn, resultCh, &seen, &lastActivity)
		}()
	}

	addrCh := make(chan string, len(targets))
	for _, t := range targets {
		addrCh <- t
	}
	close(addrCh)

	magic := protocol.MagicBytes()
	port := d.port()
	util.Debugf("[discover] 魔术字节: %q, 目标端口: %d", string(magic), port)

	sendStart := time.Now()
	sendWorkers := util.ParallelWorkers("udp")
	var sendWg sync.WaitGroup
	for i := 0; i < sendWorkers; i++ {
		sendWg.Add(1)
		go func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("[PANIC] goroutine recovered: %v", r)
				}
			}()
			defer sendWg.Done()
			for target := range addrCh {
				addr, err := net.ResolveUDPAddr("udp4", fmt.Sprintf("%s:%d", target, port))
				if err != nil {
					log.Printf("[discover] 解析地址失败: %s:%d, err=%v", target, port, err)
					continue
				}
				if _, err := conn.WriteTo(magic, addr); err != nil {
					continue
				}
			}
		}()
	}
	sendWg.Wait()
	util.Debugf("[discover] 广播发送完成, 耗时: %v", time.Since(sendStart))

	// 发送完成：写入静默计时基线（若期间已有设备回复则保留更早的活动时间）
	lastActivity.CompareAndSwap(0, time.Now().UnixNano())

	<-listenCtx.Done()
	recvWg.Wait()
	close(resultCh)
	<-drainDone

	return found
}

// recvReplies 收包循环：解析回复、按 DeviceID 去重后入队，并记录活动时间
func (d *Discoverer) recvReplies(ctx context.Context, conn net.PacketConn, resultCh chan<- Device, seen *sync.Map, lastActivity *atomic.Int64) {
	buf := make([]byte, 1024)
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		_ = conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
		n, remoteAddr, err := conn.ReadFrom(buf)
		if err != nil {
			continue
		}
		lastActivity.Store(time.Now().UnixNano())

		raw := string(buf[:n])
		reply := protocol.ParseReply(buf[:n], remoteAddr)
		if reply == nil {
			continue
		}
		if util.DebugEnabled {
			log.Printf("[discover] 收到设备回复: ip=%s type=%s id=%s name=%s raw=%q",
				reply.IP, reply.DeviceType, reply.DeviceID, reply.DeviceName, raw)
		}
		if _, dup := seen.LoadOrStore(reply.DeviceID, struct{}{}); dup {
			continue
		}
		resultCh <- Device{
			IP:         reply.IP,
			DeviceType: reply.DeviceType,
			DeviceID:   reply.DeviceID,
			DeviceName: reply.DeviceName,
			Version:    protocol.ParseVersion(reply.DeviceName),
			Port:       protocol.ScreenshotPort,
		}
	}
}

func (d *Discoverer) collectBroadcastAddresses() []string {
	var addrs []string

	addrs = append(addrs, "255.255.255.255")
	addrs = append(addrs, getInterfaceBroadcastAddresses()...)

	return addrs
}

func getInterfaceBroadcastAddresses() []string {
	var result []string
	interfaces, err := net.Interfaces()
	if err != nil {
		return result
	}
	for _, iface := range interfaces {
		if iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagUp == 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ipNet, ok := addr.(*net.IPNet)
			if !ok || ipNet.IP.To4() == nil {
				continue
			}
			mask := ipNet.Mask
			broadcast := make(net.IP, 4)
			for i := range ipNet.IP.To4() {
				broadcast[i] = ipNet.IP.To4()[i] | ^mask[i]
			}
			result = append(result, broadcast.String())
		}
	}
	return result
}

func dedupAddresses(addrs []string) []string {
	seen := make(map[string]bool)
	var unique []string
	for _, a := range addrs {
		if !seen[a] {
			seen[a] = true
			unique = append(unique, a)
		}
	}
	return unique
}

// LocalDevices 只保留 v3 设备（local 模式数据源）。
// UDP 广播是全量回复：各代设备共用 7678 端口和魔术字节，
// 按模式各取所需——local 只要 v3；v2 模式不做后缀过滤，以 2375 能力验证为准。
func LocalDevices(devs []Device) []Device {
	return filterVersion(devs, "v3")
}

func filterVersion(devs []Device, version string) []Device {
	result := make([]Device, 0, len(devs))
	for _, d := range devs {
		if d.Version == version {
			result = append(result, d)
		}
	}
	return result
}
