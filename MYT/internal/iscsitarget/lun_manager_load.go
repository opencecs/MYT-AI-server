package iscsitarget

import (
	"encoding/binary"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
)

// LoadExisting 扫描存储目录，加载已有的 .img 文件（保留注册表中的 LUN ID）
func (m *Manager) LoadExisting() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 先加载注册表
	if err := m.loadRegistry(); err != nil {
		log.Printf("[iSCSI Target] 加载注册表失败: %v", err)
	}

	// 构建已用 LUN ID 集合
	usedIDs := make(map[int]bool)

	// 从注册表加载已知路径的磁盘
	for name, storageDir := range m.paths {
		if _, exists := m.byName[name]; exists {
			continue
		}
		dd, err := m.diskDir(storageDir)
		if err != nil {
			log.Printf("[iSCSI Target] 磁盘 %s 存储路径无效: %v", name, err)
			continue
		}
		path := filepath.Join(dd, name+".img")
		if _, err := os.Stat(path); os.IsNotExist(err) {
			continue
		}
		backend, thin, err := openBackend(path)
		if err != nil {
			log.Printf("[iSCSI Target] 加载磁盘 %s 失败: %v", name, err)
			continue
		}

		// 从注册表恢复 LUN ID，若不存在则分配新的
		lunID := -1
		if entry, ok := m.loadRegistryEntry(name); ok && entry.LUNID >= 0 {
			lunID = entry.LUNID
		} else {
			lunID = m.nextLUNIDUnlocked()
		}
		// 确保不与已有 ID 冲突
		for usedIDs[lunID] {
			lunID = m.nextLUNIDUnlocked()
		}

		lun := &LUN{ID: lunID, Name: name, Thin: thin, Backend: backend, FilePath: path}
		m.luns[lunID] = lun
		m.byName[name] = lun
		usedIDs[lunID] = true
	}

	// 扫描默认目录中可能遗漏的磁盘
	entries, err := os.ReadDir(m.dir)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".img" {
			continue
		}
		name := entry.Name()[:len(entry.Name())-4]
		if _, exists := m.byName[name]; exists {
			continue
		}

		path := filepath.Join(m.dir, entry.Name())
		backend, thin, err := openBackend(path)
		if err != nil {
			continue
		}
		log.Printf("[iSCSI Target] 发现注册表外的孤儿映像文件，重新注册: %s", path)

		lunID := m.nextLUNIDUnlocked()
		for usedIDs[lunID] {
			lunID = m.nextLUNIDUnlocked()
		}

		lun := &LUN{ID: lunID, Name: name, Thin: thin, Backend: backend, FilePath: path}
		m.luns[lunID] = lun
		m.byName[name] = lun
		m.paths[name] = ""
		usedIDs[lunID] = true
	}

	if err := m.saveRegistry(); err != nil {
		log.Printf("[iSCSI Target] 保存注册表失败: %v", err)
	}

	return nil
}

// loadRegistryEntry 从注册表获取指定磁盘的条目
func (m *Manager) loadRegistryEntry(name string) (registryEntry, bool) {
	b, err := os.ReadFile(m.registryPath())
	if err != nil {
		return registryEntry{}, false
	}
	var data map[string]registryEntry
	if err := json.Unmarshal(b, &data); err != nil {
		return registryEntry{}, false
	}
	entry, ok := data[name]
	return entry, ok
}

// openBackend 自动检测并打开磁盘映像后端
func openBackend(path string) (Backend, bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, false, err
	}

	// 尝试作为 thin backend 打开
	f, err := os.Open(path)
	if err != nil {
		return nil, false, err
	}
	var header [8]byte
	n, _ := f.Read(header[:])
	f.Close()

	if n == 8 {
		virtSize := int64(binary.LittleEndian.Uint64(header[:]))
		expectedMeta := 8 + (virtSize+thinBlockSize-1)/thinBlockSize + 1
		expectedMeta = (expectedMeta + 4095) & ^4095
		if virtSize > 0 && virtSize > info.Size() && expectedMeta <= info.Size() {
			if tb, err := OpenThinBackend(path); err == nil {
				return tb, true, nil
			}
		}
	}

	b, err := OpenFixedBackend(path)
	if err != nil {
		return nil, false, err
	}
	return b, false, nil
}

func (m *Manager) nextLUNIDUnlocked() int {
	maxID := -1
	for id := range m.luns {
		if id > maxID {
			maxID = id
		}
	}
	return maxID + 1
}
