package iscsitarget

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Manager LUN 管理器
type Manager struct {
	luns   map[int]*LUN
	byName map[string]*LUN
	dir    string
	paths  map[string]string // diskName -> storageDir
	mu     sync.RWMutex
}

func NewManager(dir string) *Manager {
	return &Manager{
		luns:   make(map[int]*LUN),
		byName: make(map[string]*LUN),
		dir:    dir,
		paths:  make(map[string]string),
	}
}

func (m *Manager) Dir() string { return m.dir }

// registryPath 返回注册表文件路径
func (m *Manager) registryPath() string {
	return filepath.Join(m.dir, "registry.json")
}

// saveRegistry 持久化磁盘名 -> 存储目录映射
func (m *Manager) saveRegistry() error {
	data := make(map[string]registryEntry)
	for name, storageDir := range m.paths {
		lun := m.byName[name]
		entry := registryEntry{StorageDir: storageDir}
		if lun != nil {
			entry.LUNID = lun.ID
		}
		data[name] = entry
	}
	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(m.registryPath(), b, 0600)
}

// loadRegistry 从注册表加载磁盘名 -> 存储目录映射
func (m *Manager) loadRegistry() error {
	b, err := os.ReadFile(m.registryPath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var data map[string]registryEntry
	if err := json.Unmarshal(b, &data); err != nil {
		return err
	}
	for name, entry := range data {
		m.paths[name] = entry.StorageDir
	}
	return nil
}

// diskDir 返回磁盘映像文件的存储目录
func (m *Manager) diskDir(storageDir string) (string, error) {
	if storageDir == "" {
		return m.dir, nil
	}
	if err := validateStorageDir(storageDir); err != nil {
		return "", err
	}
	d := filepath.Join(storageDir, "myt-iscsi-storage")
	if err := os.MkdirAll(d, 0755); err != nil {
		return "", fmt.Errorf("创建存储目录失败: %w", err)
	}
	return d, nil
}

// diskPath 返回磁盘映像文件的完整路径
func (m *Manager) diskPath(name, storageDir string) (string, error) {
	dd, err := m.diskDir(storageDir)
	if err != nil {
		return "", err
	}
	return filepath.Join(dd, name+".img"), nil
}

// CreateAutoID 创建 LUN 并自动分配 LUN ID（线程安全，ID分配在锁内完成）
func (m *Manager) CreateAutoID(name string, sizeGB int64, thin bool, storageDir string) (*LUN, error) {
	if err := validateName(name); err != nil {
		return nil, err
	}
	if err := validateStorageDir(storageDir); err != nil {
		return nil, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.byName[name]; exists {
		return nil, fmt.Errorf("LUN %q already exists", name)
	}

	lunID := m.nextLUNIDUnlocked()
	return m.createLocked(name, sizeGB, lunID, thin, storageDir)
}

func (m *Manager) Create(name string, sizeGB int64, lunID int, thin bool, storageDir string) (*LUN, error) {
	if err := validateName(name); err != nil {
		return nil, err
	}
	if err := validateStorageDir(storageDir); err != nil {
		return nil, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.byName[name]; exists {
		return nil, fmt.Errorf("LUN %q already exists", name)
	}

	return m.createLocked(name, sizeGB, lunID, thin, storageDir)
}

func (m *Manager) createLocked(name string, sizeGB int64, lunID int, thin bool, storageDir string) (*LUN, error) {
	dd, err := m.diskDir(storageDir)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(dd, name+".img")
	var backend Backend

	if thin {
		backend, err = NewThinBackend(path, sizeGB)
	} else {
		backend, err = NewFixedBackend(path, sizeGB)
	}
	if err != nil {
		return nil, err
	}

	lun := &LUN{ID: lunID, Name: name, Thin: thin, Backend: backend, FilePath: path}
	m.luns[lunID] = lun
	m.byName[name] = lun
	m.paths[name] = storageDir

	if err := m.saveRegistry(); err != nil {
		// registry 保存失败时回滚
		backend.Close()
		os.Remove(path)
		delete(m.luns, lunID)
		delete(m.byName, name)
		delete(m.paths, name)
		return nil, fmt.Errorf("保存注册表失败: %w", err)
	}

	return lun, nil
}

// Delete 删除 LUN：关闭 backend → 带重试删除映像文件 → 更新内存与注册表。
// 文件删除最终失败时回滚（重新打开 backend，磁盘保持可用）并返回真实错误，
// 避免「注册表已删、img 文件残留」的孤儿状态（残留文件会在下次启动 LoadExisting 时被复活）。
func (m *Manager) Delete(name string) error {
	m.mu.Lock()
	lun, exists := m.byName[name]
	if !exists {
		m.mu.Unlock()
		return fmt.Errorf("LUN %q not found", name)
	}
	if err := lun.Backend.Close(); err != nil {
		log.Printf("[iSCSI Target] 磁盘 %s 关闭 backend 失败: %v", name, err)
	}
	m.mu.Unlock()

	// 锁外删文件：Windows 上刚关闭的大文件可能被 Defender/索引服务瞬时占用，重试吸收；
	// 同时避免长时间持有写锁阻塞其他 session 的 LUN I/O
	if err := removeFileWithRetry(lun.FilePath); err != nil {
		log.Printf("[iSCSI Target] 磁盘 %s 映像文件删除失败，回滚: %v", name, err)
		m.mu.Lock()
		if b, _, oerr := openBackend(lun.FilePath); oerr == nil {
			lun.Backend = b
		} else {
			log.Printf("[iSCSI Target] 磁盘 %s 回滚时重新打开 backend 失败: %v", name, oerr)
		}
		m.mu.Unlock()
		return fmt.Errorf("删除磁盘映像文件失败（文件可能正被占用，请稍后重试）: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.luns, lun.ID)
	delete(m.byName, name)
	delete(m.paths, name)

	if err := m.saveRegistry(); err != nil {
		log.Printf("[iSCSI Target] 保存注册表失败（磁盘已删除但注册表未更新）: %v", err)
	}

	log.Printf("[iSCSI Target] 磁盘已删除: name=%s path=%s", name, lun.FilePath)
	return nil
}

// removeFileWithRetry 带重试删除文件（最多 attempts 次，间隔 500ms）
func removeFileWithRetry(path string) error {
	const attempts = 10
	var lastErr error
	for i := 1; i <= attempts; i++ {
		lastErr = os.Remove(path)
		if lastErr == nil || os.IsNotExist(lastErr) {
			return nil
		}
		log.Printf("[iSCSI Target] 映像文件删除失败（第 %d/%d 次）: %v", i, attempts, lastErr)
		if i < attempts {
			time.Sleep(500 * time.Millisecond)
		}
	}
	return lastErr
}

// Resize 扩容 LUN（只支持 Fixed，只能扩大）
func (m *Manager) Resize(name string, newSizeGB int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	lun, exists := m.byName[name]
	if !exists {
		return fmt.Errorf("LUN %q not found", name)
	}
	if lun.Thin {
		return fmt.Errorf("不支持扩容 thin 类型磁盘")
	}

	fb, ok := lun.Backend.(*FixedBackend)
	if !ok {
		return fmt.Errorf("只支持扩容 fixed 类型磁盘")
	}

	newSize := newSizeGB * 1024 * 1024 * 1024
	if err := fb.Truncate(newSize); err != nil {
		return fmt.Errorf("扩容失败: %w", err)
	}

	log.Printf("[iSCSI Target] LUN %q resized to %d GB", name, newSizeGB)
	return nil
}

func (m *Manager) Get(id int) (*LUN, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	lun, ok := m.luns[id]
	return lun, ok
}

func (m *Manager) List() []*LUN {
	m.mu.RLock()
	defer m.mu.RUnlock()
	list := make([]*LUN, 0, len(m.luns))
	for _, lun := range m.luns {
		list = append(list, lun)
	}
	return list
}

// NextLUNID 返回下一个可用的 LUN ID（注意：调用方应持锁或立刻使用）
func (m *Manager) NextLUNID() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.nextLUNIDUnlocked()
}

// StorageDirOf 返回指定磁盘的存储目录
func (m *Manager) StorageDirOf(name string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if d, ok := m.paths[name]; ok {
		return d
	}
	return ""
}

// LoadExisting/loadRegistryEntry/openBackend/nextLUNIDUnlocked 已拆分到 lun_manager_load.go
