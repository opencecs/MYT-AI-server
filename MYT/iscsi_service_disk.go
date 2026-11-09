package main

import (
	"fmt"
	"log"
	"math"
	"path/filepath"

	"github.com/moyunteng/myt/internal/sdkapi"
)

// --- 磁盘映像管理（创建/扩容/删除/列表/关闭会话）---

// CreateDiskImage 通过本地 Target Manager 创建磁盘映像
func (s *IscsiServiceWrapper) CreateDiskImage(deviceIP, portal, name string, size int64, diskType string, storageDir string) (*sdkapi.IscsiDiskImageResult, error) {
	log.Printf("[iSCSI] CreateDiskImage: name=%s size=%d(GB) type=%s storageDir=%s", name, size, diskType, storageDir)
	if s.targetMgr == nil {
		return nil, fmt.Errorf("iSCSI Target 未初始化")
	}
	if name == "" || size <= 0 {
		return nil, fmt.Errorf("名称和大小不能为空")
	}
	if diskType != "fixed" && diskType != "thin" {
		return nil, fmt.Errorf("类型必须为 fixed 或 thin")
	}

	lun, err := s.targetMgr.CreateAutoID(name, size, diskType == "thin", storageDir)
	if err == nil {
		log.Printf("[iSCSI] CreateDiskImage 成功: LUN=%d Backend.Size=%d(bytes) = %d(GB)", lun.ID, lun.Backend.Size(), lun.Backend.Size()/(1024*1024*1024))
	}
	if err != nil {
		return nil, fmt.Errorf("创建磁盘映像失败: %w", err)
	}

	// 动态添加 per-disk IQN entry 到运行中的 Target
	iqn := iqnFromName(name)
	s.addDiskEntry(iqn, lun.ID)

	return &sdkapi.IscsiDiskImageResult{
		IQN:     iqn,
		Lun:     lun.ID,
		Success: true,
		Message: "created",
	}, nil
}

// IscsiResizeResponse 扩容结果返回给前端
type IscsiResizeResponse struct {
	Success    bool   `json:"success"`
	Device     string `json:"device,omitempty"`
	Partition  string `json:"partition,omitempty"`
	MountPoint string `json:"mountPoint,omitempty"`
	TotalMB    int64  `json:"totalMB,omitempty"`
	UsedMB     int64  `json:"usedMB,omitempty"`
	AvailMB    int64  `json:"availMB,omitempty"`
	Warning    string `json:"warning,omitempty"`
}

// ResizeDiskImage 扩容磁盘映像 + 联动设备端（rescan + growpart + resize2fs）
func (s *IscsiServiceWrapper) ResizeDiskImage(deviceIP, portal, name string, newSizeGB int64) (*IscsiResizeResponse, error) {
	if s.targetMgr == nil {
		return nil, fmt.Errorf("iSCSI Target 未初始化")
	}
	if name == "" || newSizeGB <= 0 {
		return nil, fmt.Errorf("名称和新大小不能为空")
	}

	// Step 1: Target 端扩大 .img 文件
	if err := s.targetMgr.Resize(name, newSizeGB); err != nil {
		return nil, fmt.Errorf("Target 端扩容失败: %w", err)
	}
	log.Printf("[iSCSI] Target 端扩容完成: %s → %dGB", name, newSizeGB)

	result := &IscsiResizeResponse{Success: true}

	// Step 2: Initiator 端联动（优先 SDK /mytIscsi/resize，失败 fallback SSH）
	if deviceIP != "" {
		iqn := iqnFromName(name)
		portalAddr := portal

		// 优先使用 SDK 新接口（portal/iqn 为空时 SDK 会自动扩容当前挂载设备）
		resizeResult, sdkErr := s.sdkApiSvc.newClient(deviceIP).ResizeIscsi(portalAddr, iqn)
		if sdkErr != nil {
			log.Printf("[iSCSI] SDK 扩容接口失败: %v，尝试 SSH fallback", sdkErr)
			// fallback: SSH 方式
			if sshErr := s.resizeIscsiDeviceSSH(deviceIP); sshErr != nil {
				log.Printf("[iSCSI] 设备端扩容警告: %v（Target 端已完成，可稍后手动 rescan）", sshErr)
				result.Warning = fmt.Sprintf("Target 端已扩容，设备端扩容失败: %v", sshErr)
			}
		} else if resizeResult != nil {
			log.Printf("[iSCSI] SDK 扩容成功: device=%s partition=%s totalMB=%d usedMB=%d availMB=%d success=%v",
				resizeResult.Device, resizeResult.Partition, resizeResult.TotalMB, resizeResult.UsedMB, resizeResult.AvailMB, resizeResult.Success)
			result.Device = resizeResult.Device
			result.Partition = resizeResult.Partition
			result.MountPoint = resizeResult.MountPoint
			result.TotalMB = resizeResult.TotalMB
			result.UsedMB = resizeResult.UsedMB
			result.AvailMB = resizeResult.AvailMB
			result.Success = resizeResult.Success

			// 用返回的容量更新设备列表磁盘显示
			if s.configMgr != nil && resizeResult.Success {
				deviceID := s.sdkApiSvc.GetDeviceIDByIP(deviceIP)
				if deviceID != "" {
					s.configMgr.UpdateDeviceDisk(deviceID, resizeResult.UsedMB*1024*1024, resizeResult.TotalMB*1024*1024)
				}
			}
		}
	}

	return result, nil
}

// DeleteDiskImage 通过本地 Target Manager 删除磁盘映像
func (s *IscsiServiceWrapper) DeleteDiskImage(deviceIP, portal, name string, force bool) error {
	if s.targetMgr == nil {
		return fmt.Errorf("iSCSI Target 未初始化")
	}
	if name == "" {
		return fmt.Errorf("名称不能为空")
	}
	iqn := iqnFromName(name)
	if !force {
		// 检查该磁盘是否被占用（按 per-disk IQN 判断）
		occupied := s.GetOccupiedDisks()
		for _, d := range occupied {
			if d.IQN == iqn {
				return fmt.Errorf("设备 %s 正在使用该磁盘，请先卸载再删除", d.DeviceIP)
			}
		}
	} else {
		// 强制模式：先关闭该 IQN 的活跃 session
		if s.targetSvc != nil && s.targetSvc.target != nil {
			s.targetSvc.target.CloseSessionsByIQN(iqn)
		}
	}
	// 先从 Manager 删除（含映像文件），成功后再从 Target 移除 IQN entry，保证状态一致
	if err := s.targetMgr.Delete(name); err != nil {
		return err
	}
	s.removeDiskEntry(iqn)
	return nil
}

// CloseIscsiSession 关闭指定 IQN 的 iSCSI session（踢连接）
func (s *IscsiServiceWrapper) CloseIscsiSession(iqn string) error {
	if s.targetSvc == nil || s.targetSvc.target == nil {
		return fmt.Errorf("iSCSI Target 未初始化")
	}
	return s.targetSvc.target.CloseSession(iqn)
}

// ListDiskImages 通过本地 Target Manager 列出磁盘映像
func (s *IscsiServiceWrapper) ListDiskImages(portal string) ([]sdkapi.IscsiDiskImageInfo, error) {
	if s.targetMgr == nil {
		return nil, fmt.Errorf("iSCSI Target 未初始化")
	}
	luns := s.targetMgr.List()
	disks := make([]sdkapi.IscsiDiskImageInfo, 0, len(luns))
	for _, l := range luns {
		disks = append(disks, sdkapi.IscsiDiskImageInfo{
			Name:        l.Name,
			Lun:         l.ID,
			SizeGB:      math.Round(float64(l.Backend.Size())/float64(1024*1024*1024)*10) / 10,
			Type:        l.Type(),
			IQN:         iqnFromName(l.Name),
			StoragePath: filepath.Dir(l.FilePath),
		})
	}
	return disks, nil
}
