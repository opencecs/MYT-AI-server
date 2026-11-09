package iscsitarget

import (
	"fmt"
	"path/filepath"
	"strings"
)

// Backend LUN 存储后端接口
type Backend interface {
	Read(at int64, buf []byte) error
	Write(at int64, buf []byte) error
	// Sync 持久化数据与元数据到物理介质（SCSI SYNCHRONIZE CACHE / FUA 路径），
	// 返回即代表已落盘——initiator 的 fsync 语义依赖此承诺
	Sync() error
	Size() int64
	Close() error
}

// validateName 校验磁盘名称，防止路径穿越
func validateName(name string) error {
	if name == "" {
		return fmt.Errorf("名称不能为空")
	}
	if strings.Contains(name, "..") || strings.ContainsAny(name, `/\:`) {
		return fmt.Errorf("名称包含非法字符")
	}
	return nil
}

// validateStorageDir 校验存储目录，防止路径穿越
func validateStorageDir(dir string) error {
	if dir == "" {
		return nil
	}
	if strings.Contains(dir, "..") {
		return fmt.Errorf("存储路径包含非法字符")
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("无效的存储路径: %w", err)
	}
	// 确保是绝对路径：Unix 以 / 开头，Windows 以盘符如 C: 开头
	if abs[0] != '/' && (len(abs) < 2 || abs[1] != ':') {
		return fmt.Errorf("存储路径必须是绝对路径")
	}
	return nil
}

// LUNProvider provides LUN access for iSCSI sessions
type LUNProvider interface {
	Get(id int) (*LUN, bool)
	List() []*LUN
}

// SingleLUNProvider exposes only one LUN from a Manager, remapped to LUN 0
type SingleLUNProvider struct {
	Mgr   *Manager
	LunID int
}

func (p *SingleLUNProvider) Get(id int) (*LUN, bool) {
	// LUN 0 always maps to the underlying LUN
	if id == 0 {
		return p.Mgr.Get(p.LunID)
	}
	return nil, false
}

func (p *SingleLUNProvider) List() []*LUN {
	lun, ok := p.Mgr.Get(p.LunID)
	if !ok {
		return nil
	}
	// Return a copy with ID=0 so REPORT LUNs returns LUN 0
	cp := *lun
	cp.ID = 0
	return []*LUN{&cp}
}

// LUN 逻辑单元
type LUN struct {
	ID       int
	Name     string
	Thin     bool
	Backend  Backend
	FilePath string
}

func (l *LUN) Type() string {
	if l.Thin {
		return "thin"
	}
	return "fixed"
}

// registryJSON 注册表持久化格式
type registryEntry struct {
	StorageDir string `json:"storageDir"`
	LUNID      int    `json:"lunId"`
}

// --- 后端实现与管理器已拆分到：---
//   - backend_fixed.go  （FixedBackend）
//   - backend_thin.go   （ThinBackend）
//   - lun_manager.go    （Manager）
