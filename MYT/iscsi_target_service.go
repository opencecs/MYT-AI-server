package main

import (
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"

	"github.com/moyunteng/myt/internal/iscsitarget"
)

type IscsiTargetStatus struct {
	Running bool   `json:"running"`
	IQN     string `json:"iqn"`
	Portal  string `json:"portal"`
	APIAddr string `json:"apiAddr"`
}

type IscsiTargetServiceWrapper struct {
	manager  *iscsitarget.Manager
	target   *iscsitarget.Target
	api      *iscsitarget.APIServer
	portal   string
	apiAddr  string
	started  bool
	iscsiSvc *IscsiServiceWrapper
}

func NewIscsiTargetServiceWrapper(rootDir string) *IscsiTargetServiceWrapper {
	storageDir := filepath.Join(rootDir, "iscsi-storage")
	if err := os.MkdirAll(storageDir, 0755); err != nil {
		log.Printf("[iSCSI Target] 创建存储目录失败: %v", err)
	}

	mgr := iscsitarget.NewManager(storageDir)
	if err := mgr.LoadExisting(); err != nil {
		log.Printf("[iSCSI Target] 加载已有磁盘映像失败: %v", err)
	}

	portal := "0.0.0.0:3260"
	apiAddr := "127.0.0.1:9000"

	return &IscsiTargetServiceWrapper{
		manager: mgr,
		portal:  portal,
		apiAddr: apiAddr,
	}
}

// buildEntries 为已有磁盘构建 TargetEntry 列表
func (s *IscsiTargetServiceWrapper) buildEntries() []*iscsitarget.TargetEntry {
	luns := s.manager.List()
	entries := make([]*iscsitarget.TargetEntry, 0, len(luns))
	for _, l := range luns {
		iqn := iqnForDisk(l.Name)
		entries = append(entries, &iscsitarget.TargetEntry{
			IQN:  iqn,
			LUNs: &iscsitarget.SingleLUNProvider{Mgr: s.manager, LunID: l.ID},
		})
	}
	return entries
}

// iqnForDisk 根据 disk name 生成 per-disk IQN
func iqnForDisk(name string) string {
	return "iqn.2024.com.myt:disk." + name
}

func (s *IscsiTargetServiceWrapper) StartTarget() error {
	if s.started {
		return nil
	}

	entries := s.buildEntries()
	s.target = iscsitarget.NewTarget(s.portal, entries)
	if err := s.target.Start(); err != nil {
		return fmt.Errorf("启动 iSCSI Target 失败: %w", err)
	}

	s.api = iscsitarget.NewAPIServer(s.manager, s.target)
	go func() {
		if err := s.api.Start(s.apiAddr); err != nil {
			log.Printf("[iSCSI Target] Management API 停止: %v", err)
		}
	}()

	s.started = true
	iqnCount := len(entries)
	log.Printf("[iSCSI Target] 已启动 - Portal: %s, IQNs: %d", s.portal, iqnCount)
	return nil
}

func (s *IscsiTargetServiceWrapper) StopTarget() {
	if !s.started {
		return
	}
	if s.target != nil {
		s.target.Stop()
	}
	if s.api != nil {
		s.api.Stop()
	}
	s.started = false
	log.Printf("[iSCSI Target] 已停止")
}

func (s *IscsiTargetServiceWrapper) GetTargetStatus() IscsiTargetStatus {
	running := s.started && s.target != nil && s.target.IsRunning()
	// 返回第一个 IQN 作为主 IQN（向后兼容）
	iqn := ""
	if s.target != nil {
		entries := s.target.GetEntries()
		if len(entries) > 0 {
			iqn = entries[0].IQN
		}
	}
	return IscsiTargetStatus{
		Running: running,
		IQN:     iqn,
		Portal:  s.portal,
		APIAddr: s.apiAddr,
	}
}

func (s *IscsiTargetServiceWrapper) CreateDisk(name string, sizeGB int64, diskType string, storageDir string) (*iscsitarget.DiskResponse, error) {
	if name == "" || sizeGB <= 0 {
		return nil, fmt.Errorf("名称和大小不能为空")
	}

	lun, err := s.manager.CreateAutoID(name, sizeGB, diskType == "thin", storageDir)
	if err != nil {
		return nil, fmt.Errorf("创建磁盘失败: %w", err)
	}

	iqn := iqnForDisk(name)
	// 动态添加 IQN entry 到运行中的 Target
	if s.target != nil {
		s.target.AddEntry(&iscsitarget.TargetEntry{
			IQN:  iqn,
			LUNs: &iscsitarget.SingleLUNProvider{Mgr: s.manager, LunID: lun.ID},
		})
	}

	return &iscsitarget.DiskResponse{
		IQN:     iqn,
		LUN:     lun.ID,
		Success: true,
		Message: "created",
	}, nil
}

func (s *IscsiTargetServiceWrapper) DeleteDisk(name string, force bool) error {
	if name == "" {
		return fmt.Errorf("名称不能为空")
	}
	iqn := iqnForDisk(name)
	// force=true 时先关闭活跃 session（同步等待 session 完全退出）
	if force && s.target != nil {
		s.target.CloseSessionsByIQN(iqn)
	} else if !force && s.target != nil {
		// 非强制：磁盘仍有活跃 session 时拒绝删除，避免与 in-flight I/O 竞态导致映像文件删除失败
		for _, active := range s.target.GetActiveIQNs() {
			if active == iqn {
				return fmt.Errorf("磁盘 %s 正在被使用，请先卸载或使用强制删除", name)
			}
		}
	}
	// 先从 Manager 删除（含映像文件），成功后再从 Target 移除 IQN entry，保证状态一致
	if err := s.manager.Delete(name); err != nil {
		return err
	}
	if s.target != nil {
		s.target.RemoveEntry(iqn)
	}
	return nil
}

// ResizeDisk 扩容磁盘映像并联动设备端（rescan + growpart + resize2fs）
func (s *IscsiTargetServiceWrapper) ResizeDisk(deviceIP, name string, newSizeGB int64) (*IscsiResizeResponse, error) {
	if name == "" || newSizeGB <= 0 {
		return nil, fmt.Errorf("名称和新大小不能为空")
	}
	// 委托给 IscsiServiceWrapper，它包含完整的 Target + 设备端联动逻辑
	if s.iscsiSvc != nil && deviceIP != "" {
		return s.iscsiSvc.ResizeDiskImage(deviceIP, "", name, newSizeGB)
	}
	// 无 iscsiSvc 或无设备 IP 时，只做 Target 端扩容
	if err := s.manager.Resize(name, newSizeGB); err != nil {
		return nil, fmt.Errorf("Target 端扩容失败: %w", err)
	}
	return &IscsiResizeResponse{Success: true}, nil
}

func (s *IscsiTargetServiceWrapper) ListDisks() []iscsitarget.DiskInfo {
	luns := s.manager.List()

	// 构建 LUN ID -> IQN 映射
	lunIQN := map[int]string{}
	if s.target != nil {
		for _, e := range s.target.GetEntries() {
			if sp, ok := e.LUNs.(*iscsitarget.SingleLUNProvider); ok {
				lunIQN[sp.LunID] = e.IQN
			}
		}
	}

	disks := make([]iscsitarget.DiskInfo, 0, len(luns))
	for _, l := range luns {
		iqn := lunIQN[l.ID]
		if iqn == "" {
			iqn = iqnForDisk(l.Name)
		}
		disks = append(disks, iscsitarget.DiskInfo{
			Name:        l.Name,
			LUN:         l.ID,
			SizeGB:      math.Round(float64(l.Backend.Size())/float64(1024*1024*1024)*10) / 10,
			Type:        l.Type(),
			IQN:         iqn,
			StoragePath: filepath.Dir(l.FilePath),
		})
	}
	return disks
}

func (s *IscsiTargetServiceWrapper) SetIscsiService(svc *IscsiServiceWrapper) {
	s.iscsiSvc = svc
}

func (s *IscsiTargetServiceWrapper) GetIscsiTargetManager() *iscsitarget.Manager {
	return s.manager
}

func (s *IscsiTargetServiceWrapper) GetIscsiTargetIQN() string {
	// 返回第一个 IQN（向后兼容）
	if s.target != nil {
		entries := s.target.GetEntries()
		if len(entries) > 0 {
			return entries[0].IQN
		}
	}
	return ""
}

func (s *IscsiTargetServiceWrapper) GetStorageDir() string {
	return s.manager.Dir()
}
