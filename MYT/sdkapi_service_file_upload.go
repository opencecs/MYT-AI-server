package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/moyunteng/myt/internal/util"
)

// UploadFileEntry 上传目录中的文件条目
type UploadFileEntry struct {
	Name    string `json:"name"`    // 文件名
	Path    string `json:"path"`    // 完整路径
	Size    int64  `json:"size"`    // 文件大小(字节)
	ModTime int64  `json:"modTime"` // 修改时间(Unix秒)
	Ext     string `json:"ext"`     // 扩展名(小写, 含.)
}

// GetUploadDir 获取批量上传专用目录路径
func (f *FileService) GetUploadDir() string {
	return f.cm.UploadDir()
}

// OpenUploadDir 在系统文件管理器中打开上传目录
func (f *FileService) OpenUploadDir() error {
	dir := f.cm.UploadDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("创建上传目录失败: %w", err)
	}
	return util.OpenDirectory(dir)
}

// ScanUploadFiles 扫描上传目录中的文件（跳过子目录、隐藏文件和临时文件）
func (f *FileService) ScanUploadFiles() ([]UploadFileEntry, error) {
	dir := f.cm.UploadDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("创建上传目录失败: %w", err)
	}

	var files []UploadFileEntry
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("扫描上传目录失败: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "~") || strings.HasSuffix(strings.ToLower(name), ".tmp") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		files = append(files, UploadFileEntry{
			Name:    name,
			Path:    filepath.Join(dir, name),
			Size:    info.Size(),
			ModTime: info.ModTime().Unix(),
			Ext:     strings.ToLower(filepath.Ext(name)),
		})
	}

	sort.Slice(files, func(i, j int) bool {
		return strings.ToLower(files[i].Name) < strings.ToLower(files[j].Name)
	})
	return files, nil
}

// BuildUploadEntries 将文件路径列表转为 UploadFileEntry 列表（stat 真实文件，跳过目录和不存在项）。
// 供原生文件选择对话框和窗口拖拽 drop handler 复用，消除多处重复的 stat 逻辑。
func (f *FileService) BuildUploadEntries(paths []string) []UploadFileEntry {
	entries := make([]UploadFileEntry, 0, len(paths))
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil || info.IsDir() {
			continue
		}
		name := filepath.Base(p)
		entries = append(entries, UploadFileEntry{
			Name:    name,
			Path:    p,
			Size:    info.Size(),
			ModTime: info.ModTime().Unix(),
			Ext:     strings.ToLower(filepath.Ext(name)),
		})
	}
	return entries
}
