package main

import (
	"log"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/moyunteng/myt/internal/iscsitarget"

	"github.com/moyunteng/myt/internal/config"
	"github.com/moyunteng/myt/internal/sdkapi"
	"github.com/moyunteng/myt/internal/util"
)

type LocalNicInfo struct {
	Name string `json:"name"`
	IP   string `json:"ip"`
	Mac  string `json:"mac"`
}

type LocalDiskInfo struct {
	Drive      string `json:"drive"`
	Label      string `json:"label"`
	TotalBytes uint64 `json:"totalBytes"`
	FreeBytes  uint64 `json:"freeBytes"`
}

type IscsiServiceWrapper struct {
	sdkApiSvc         *SdkApiServiceWrapper
	targetMgr         *iscsitarget.Manager
	targetSvc         *IscsiTargetServiceWrapper
	iqn               string
	configMgr         *config.Manager
	onAndroidsChanged func(deviceIPs []string) // 挂载/卸载后触发容器列表即时刷新

	// IQN→IP 映射缓存（60s）：无盘页每 5s 轮询 GetOccupiedDisks，无缓存时每轮
	// 对全部在线设备并发 ListIscsi——千台规模 = 洪峰千余 req/s 打设备。映射
	// 实际变化只在挂载/卸载时（invalidateIQNCache 主动失效）。
	iqnCacheMu   sync.Mutex
	iqnCache     map[string]string
	iqnCacheAt   time.Time
	iqnCacheBusy bool // 查询进行中：后来者等待共享结果，不重复发起
}

func NewIscsiServiceWrapper(sdkApiSvc *SdkApiServiceWrapper) *IscsiServiceWrapper {
	return &IscsiServiceWrapper{
		sdkApiSvc: sdkApiSvc,
	}
}

func (s *IscsiServiceWrapper) SetIscsiTargetManager(mgr *iscsitarget.Manager, iqn string) {
	s.targetMgr = mgr
	s.iqn = iqn
}

func (s *IscsiServiceWrapper) SetIscsiTargetService(svc *IscsiTargetServiceWrapper) {
	s.targetSvc = svc
}

// SetOnAndroidsChanged 设置容器列表变更回调，挂载/卸载成功后触发即时刷新
func (s *IscsiServiceWrapper) SetOnAndroidsChanged(fn func(deviceIPs []string)) {
	s.onAndroidsChanged = fn
}

// addDiskEntry 动态添加 per-disk IQN entry 到运行中的 Target
func (s *IscsiServiceWrapper) addDiskEntry(iqn string, lunID int) {
	if s.targetSvc == nil || s.targetSvc.target == nil || s.targetMgr == nil {
		log.Printf("[iSCSI] addDiskEntry 跳过: targetSvc=%v target=%v targetMgr=%v", s.targetSvc != nil, s.targetSvc != nil && s.targetSvc.target != nil, s.targetMgr != nil)
		return
	}
	s.targetSvc.target.AddEntry(&iscsitarget.TargetEntry{
		IQN:  iqn,
		LUNs: &iscsitarget.SingleLUNProvider{Mgr: s.targetMgr, LunID: lunID},
	})
	log.Printf("[iSCSI] addDiskEntry 成功: iqn=%s lunID=%d", iqn, lunID)
}

// removeDiskEntry 从运行中的 Target 移除 per-disk IQN entry
func (s *IscsiServiceWrapper) removeDiskEntry(iqn string) {
	if s.targetSvc == nil || s.targetSvc.target == nil {
		return
	}
	s.targetSvc.target.RemoveEntry(iqn)
}

// iqnFromName 根据 disk name 生成 per-disk IQN
func iqnFromName(name string) string {
	return "iqn.2024.com.myt:disk." + name
}

func (s *IscsiServiceWrapper) SetConfigManager(cm *config.Manager) {
	s.configMgr = cm
}

// OccupiedDisk 已被占用的磁盘信息
type OccupiedDisk struct {
	IQN      string `json:"iqn"`
	DeviceIP string `json:"deviceIP"`
}

// GetOccupiedDisks 返回已被占用的磁盘 IQN 及对应设备 IP
// 优先查 Target 本地活跃连接（即使远程设备离线也能检测），再补充 IQN→IP 映射
func (s *IscsiServiceWrapper) GetOccupiedDisks() []OccupiedDisk {
	var result []OccupiedDisk
	seen := map[string]bool{}

	// Step 1: 从本地 Target 查询哪些 IQN 有活跃连接
	if s.targetSvc != nil && s.targetSvc.target != nil {
		activeSessions := s.targetSvc.target.GetActiveSessions()
		iqnToIP := s.buildIQNToIPMap() // 60s 缓存 + 单飞（无盘页 5s 轮询不再全量打设备）
		for iqn := range activeSessions {
			if seen[iqn] {
				continue
			}
			seen[iqn] = true
			ip := iqnToIP[iqn]
			result = append(result, OccupiedDisk{IQN: iqn, DeviceIP: ip})
		}
	}

	if len(result) > 0 {
		log.Printf("[iSCSI] GetOccupiedDisks: %d 个占用", len(result))
	}
	return result
}

// iqnCacheTTL IQN→IP 映射缓存时长：映射只在设备挂载/卸载时变化（变化点已接
// invalidateIQNCache 主动失效），TTL 只是兜底。
const iqnCacheTTL = 60 * time.Second

// invalidateIQNCache 挂载/卸载后主动失效 IQN 映射缓存（下一轮查询立即重建）。
func (s *IscsiServiceWrapper) invalidateIQNCache() {
	s.iqnCacheMu.Lock()
	s.iqnCache = nil
	s.iqnCacheMu.Unlock()
}

// buildIQNToIPMap 并行查询所有在线设备 iSCSI session（SDK-only，跳过 SSH fallback 加速）。
// 60s 缓存 + 单飞（查询进行中后来者等同一结果）：无盘页 5s 轮询不再每次打全部设备。
func (s *IscsiServiceWrapper) buildIQNToIPMap() map[string]string {
	s.iqnCacheMu.Lock()
	if s.iqnCache != nil && time.Since(s.iqnCacheAt) < iqnCacheTTL {
		cached := s.iqnCache
		s.iqnCacheMu.Unlock()
		return cached
	}
	if s.iqnCacheBusy { // 已有查询在飞：等它完成取共享结果（不重复发起）
		for s.iqnCacheBusy {
			s.iqnCacheMu.Unlock()
			time.Sleep(20 * time.Millisecond)
			s.iqnCacheMu.Lock()
		}
		cached := s.iqnCache
		s.iqnCacheMu.Unlock()
		return cached
	}
	s.iqnCacheBusy = true
	s.iqnCacheMu.Unlock()

	iqnToIP := map[string]string{}
	if s.configMgr != nil && s.sdkApiSvc != nil {
		devices := s.configMgr.GetOnlineDevices()
		var mu sync.Mutex
		var wg sync.WaitGroup
		for _, d := range devices {
			if d.IP == "" {
				continue
			}
			wg.Add(1)
			go func(ip string) {
				defer wg.Done()
				sessions, _ := s.sdkApiSvc.newClient(ip).ListIscsi()
				if len(sessions) == 0 {
					return
				}
				mu.Lock()
				for _, ses := range sessions {
					if ses.IQN != "" {
						iqnToIP[ses.IQN] = ip
					}
				}
				mu.Unlock()
			}(d.IP)
		}
		wg.Wait()
	}

	s.iqnCacheMu.Lock()
	s.iqnCache = iqnToIP
	s.iqnCacheAt = time.Now()
	s.iqnCacheBusy = false
	s.iqnCacheMu.Unlock()
	return iqnToIP
}

// GetMountedDeviceIP 遍历在线设备查询哪台已挂载 iSCSI，没有则返回空
func (s *IscsiServiceWrapper) GetMountedDeviceIP() string {
	if s.configMgr == nil || s.sdkApiSvc == nil {
		return ""
	}
	devices := s.configMgr.GetOnlineDevices()
	for _, d := range devices {
		if d.IP == "" {
			continue
		}
		status, err := s.sdkApiSvc.newClient(d.IP).GetIscsiStatus()
		if err == nil && status.IsMounted {
			return d.IP
		}
	}
	return ""
}

// ListLocalNics 获取本机物理网卡列表（过滤虚拟网卡）
func (s *IscsiServiceWrapper) ListLocalNics() ([]LocalNicInfo, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	var result []LocalNicInfo
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		name := iface.Name
		if isVirtualNic(name, iface.HardwareAddr.String()) {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ipNet, ok := addr.(*net.IPNet)
			if !ok || ipNet.IP.IsLoopback() || ipNet.IP.To4() == nil {
				continue
			}
			if isPrivateVirtualIP(ipNet.IP) {
				continue
			}
			result = append(result, LocalNicInfo{
				Name: name,
				IP:   ipNet.IP.String(),
				Mac:  iface.HardwareAddr.String(),
			})
		}
	}
	return result, nil
}

// isPrivateVirtualIP 判断是否为 VPC 等虚拟网段的 IP
func isPrivateVirtualIP(ip net.IP) bool {
	virtualCIDR := "192.168.224.0/19"
	_, ipNet, _ := net.ParseCIDR(virtualCIDR)
	if ipNet != nil && ipNet.Contains(ip) {
		return true
	}
	return false
}

// ─── 设备端 iSCSI 操作 ───

func (s *IscsiServiceWrapper) DiscoverIscsi(deviceIP, portal string) ([]sdkapi.IscsiTargetInfo, error) {
	targets, err := s.sdkApiSvc.newClientWithTimeout(deviceIP, 30*time.Second).DiscoverIscsi(portal)
	log.Printf("[iSCSI] DiscoverIscsi: deviceIP=%s portal=%s targets=%d err=%v", deviceIP, portal, len(targets), err)
	return targets, err
}

func (s *IscsiServiceWrapper) LoginIscsi(deviceIP, portal, iqn, localIP string) error {
	return s.sdkApiSvc.newClientWithTimeout(deviceIP, 30*time.Second).LoginIscsi(portal, iqn, localIP)
}

func (s *IscsiServiceWrapper) LogoutIscsi(deviceIP, portal, iqn string) error {
	return s.sdkApiSvc.newClientWithTimeout(deviceIP, 15*time.Second).LogoutIscsi(portal, iqn)
}

func (s *IscsiServiceWrapper) ListIscsi(deviceIP string) ([]sdkapi.IscsiSessionInfo, error) {
	sessions, err := s.sdkApiSvc.newClient(deviceIP).ListIscsi()
	if err != nil && strings.Contains(err.Error(), "Not Found") {
		return nil, nil
	}
	for i, ses := range sessions {
		log.Printf("[iSCSI] ListIscsi[%d]: iqn=%q portal=%q device=%q", i, ses.IQN, ses.Portal, ses.Device)
	}
	log.Printf("[iSCSI] ListIscsi: deviceIP=%s count=%d err=%v", deviceIP, len(sessions), err)
	return sessions, err
}

func (s *IscsiServiceWrapper) ListDeviceNics(deviceIP string) ([]sdkapi.IscsiNicInfo, error) {
	return s.sdkApiSvc.newClient(deviceIP).ListIscsiNic()
}

func (s *IscsiServiceWrapper) PartitionIscsi(deviceIP, device, label string) ([]string, error) {
	return s.sdkApiSvc.newClientWithTimeout(deviceIP, 30*time.Second).PartitionIscsi(device, label)
}

func (s *IscsiServiceWrapper) FormatIscsi(deviceIP, partition, fsType string) error {
	return s.sdkApiSvc.newClientWithTimeout(deviceIP, 30*time.Second).FormatIscsi(partition, fsType)
}

// ─── Mount/Umount 一体化接口 ───

// MountIscsi 挂载 iSCSI 磁盘（一体化：登录+分区格式化+挂载+迁移容器）
// 超时需覆盖格式化（磁盘越大越久，SSH fallback 路径 mkfs 给了 600s）+ rsync 数据迁移（300s），故给 600s
func (s *IscsiServiceWrapper) MountIscsi(deviceIP, portal, iqn string, autoInit bool, localIP string) (*sdkapi.IscsiMountResult, error) {
	result, err := s.sdkApiSvc.newClientWithTimeout(deviceIP, 600*time.Second).MountIscsi(portal, iqn, autoInit, localIP)
	if err == nil && result != nil && result.Success {
		s.notifyAndroidsChanged(deviceIP)
		return result, nil
	}
	// SDK 失败分两种：
	// 1) 快速失败（iscsiadm exit status 21 session 已存在、discovery 失败等）→ cleanup + 重试 SDK
	// 2) 超时失败（600s HTTP 超时，docker 重启卡住等）→ 不重试 SDK（再等 600s 无意义），直接 SSH fallback
	errStr := ""
	if err != nil {
		errStr = err.Error()
	}
	isTimeout := strings.Contains(errStr, "timeout") || strings.Contains(errStr, "context deadline exceeded") || strings.Contains(errStr, "Client.Timeout")
	if !isTimeout {
		log.Printf("[iSCSI] SDK mount 快速失败，尝试清理残留会话后重试: %v", err)
		if cleanupErr := s.sdkApiSvc.newClientWithTimeout(deviceIP, 30*time.Second).CleanupIscsi(portal, iqn, true); cleanupErr == nil {
			result2, err2 := s.sdkApiSvc.newClientWithTimeout(deviceIP, 600*time.Second).MountIscsi(portal, iqn, autoInit, localIP)
			if err2 == nil && result2 != nil && result2.Success {
				s.notifyAndroidsChanged(deviceIP)
				return result2, nil
			}
			log.Printf("[iSCSI] SDK cleanup+重试仍失败: %v，fallback 到 SSH", err2)
		}
	} else {
		log.Printf("[iSCSI] SDK mount 超时(600s)，跳过重试直接 SSH fallback: %v", err)
	}
	// SDK 仍失败 → SSH 回退（SSH 路径会先清理残留会话再登录）
	mountResult, mountErr := s.mountIscsiSSH(deviceIP, portal, iqn, autoInit, localIP)
	if mountErr == nil && mountResult != nil && mountResult.Success {
		s.notifyAndroidsChanged(deviceIP)
	}
	return mountResult, mountErr
}

// UmountIscsi 卸载 iSCSI 磁盘（一体化：迁移容器+卸载+登出）
func (s *IscsiServiceWrapper) UmountIscsi(deviceIP, portal, iqn string, force bool) (*sdkapi.IscsiUmountResult, error) {
	// 优先使用 SDK API
	result, err := s.sdkApiSvc.newClientWithTimeout(deviceIP, 60*time.Second).UmountIscsi(portal, iqn, force)
	if err == nil && result != nil && result.Success {
		s.notifyAndroidsChanged(deviceIP)
		return result, nil
	}
	// SDK API 不支持时，通过 SSH 回退
	umountResult, umountErr := s.umountIscsiSSH(deviceIP, portal, iqn)
	if umountErr == nil && umountResult != nil && umountResult.Success {
		s.notifyAndroidsChanged(deviceIP)
	}
	return umountResult, umountErr
}

// notifyAndroidsChanged 异步通知容器列表变更
func (s *IscsiServiceWrapper) notifyAndroidsChanged(deviceIP string) {
	s.invalidateIQNCache() // 挂载/卸载改变 IQN→IP 映射：失效缓存让下轮查询重建
	if s.onAndroidsChanged == nil {
		return
	}
	util.GoSafe(func() { s.onAndroidsChanged([]string{deviceIP}) })
}

// CleanupIscsi 手动清理 iSCSI 残留状态（session、挂载、node 记录）
func (s *IscsiServiceWrapper) CleanupIscsi(deviceIP, portal, iqn string, force bool) error {
	return s.sdkApiSvc.newClientWithTimeout(deviceIP, 30*time.Second).CleanupIscsi(portal, iqn, force)
}

// GetStorageMode 查询设备当前存储模式
func (s *IscsiServiceWrapper) GetStorageMode(deviceIP string) string {
	status, err := s.GetIscsiStatus(deviceIP)
	if err != nil {
		log.Printf("[iSCSI] GetStorageMode fallback: deviceIP=%s err=%v", deviceIP, err)
		return "local"
	}
	return status.StorageMode
}

// GetIscsiStatus 查询设备 iSCSI 存储状态
func (s *IscsiServiceWrapper) GetIscsiStatus(deviceIP string) (*sdkapi.IscsiStorageStatus, error) {
	status, err := s.sdkApiSvc.newClient(deviceIP).GetIscsiStatus()
	if err != nil && strings.Contains(err.Error(), "Not Found") {
		return &sdkapi.IscsiStorageStatus{StorageMode: "local"}, nil
	}
	if err != nil {
		return nil, err
	}

	// 自动修正：isMounted=true 但 storageMode 还是 local（外部挂载未经过 SDK）
	if status.IsMounted && status.StorageMode != "iscsi" {
		log.Printf("[iSCSI] GetIscsiStatus: 自动修正 storageMode %s → iscsi（isMounted=true）", status.StorageMode)
		status.StorageMode = "iscsi"
	}

	log.Printf("[iSCSI] GetIscsiStatus: deviceIP=%s mode=%s mounted=%v device=%s total=%dMB used=%dMB avail=%dMB",
		deviceIP, status.StorageMode, status.IsMounted, status.Device, status.TotalMB, status.UsedMB, status.AvailMB)

	// 挂载时用无盘存储空间更新设备列表的磁盘显示
	if s.configMgr != nil && status.IsMounted {
		deviceID := s.sdkApiSvc.GetDeviceIDByIP(deviceIP)
		if deviceID != "" {
			s.configMgr.UpdateDeviceDisk(deviceID, status.UsedMB*1024*1024, status.TotalMB*1024*1024)
		}
	}

	return status, nil
}

// ─── 本地 Target Manager 磁盘管理 ───

// CreateDiskImage/ResizeDiskImage/DeleteDiskImage/CloseIscsiSession/ListDiskImages
// 已拆分到 iscsi_service_disk.go
