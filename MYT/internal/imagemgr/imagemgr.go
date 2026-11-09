// Package imagemgr 管理本地已保存的镜像
// 以文件系统为真实来源：扫描 data/images/*.tar 文件，读取同名伴生的 .meta.json 获取元数据
// 下载中的文件使用 .tar.downloading 后缀，完成后重命名为 .tar
// 元数据存储在伴生文件 xxx.meta.json 中，不污染 tar 包本身，确保推送到设备时完全兼容
package imagemgr

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// SavedImage 表示一个已保存到本地的镜像（也用于伴生 .meta.json 文件的序列化）
type SavedImage struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	URL      string `json:"url"`
	TType2   string `json:"ttype2"`
	OsVer    string `json:"os_ver"`
	SysVer   string `json:"sys_ver"`
	SysVerDes string `json:"sys_ver_des"`
	UDesc    string `json:"udesc"`
	SavedAt  string `json:"saved_at"`
}

// Service 管理本地镜像，以文件系统为真实来源
type Service struct {
	imagesDir string
}

// NewService 创建镜像管理服务，rootDir 为应用根目录
func NewService(rootDir string) *Service {
	return &Service{
		imagesDir: filepath.Join(rootDir, "data", "images"),
	}
}

// ImagesDir 返回镜像文件存储目录
func (s *Service) ImagesDir() string {
	return s.imagesDir
}

// Load 确保镜像目录存在
func (s *Service) Load() error {
	return os.MkdirAll(s.imagesDir, 0755)
}

// GetSavedImages 扫描 data/images/*.tar 文件，读取对应的 .meta.json，返回已下载镜像列表
func (s *Service) GetSavedImages() []SavedImage {
	entries, err := os.ReadDir(s.imagesDir)
	if err != nil {
		return nil
	}

	var result []SavedImage
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".tar") {
			continue
		}
		metaPath := filepath.Join(s.imagesDir, strings.TrimSuffix(entry.Name(), ".tar")+".meta.json")
		meta, err := readMetaFile(metaPath)
		if err != nil {
			continue
		}
		result = append(result, *meta)
	}
	return result
}

// DeleteSavedImage 根据镜像ID删除对应的 .tar 和 .meta.json 文件
func (s *Service) DeleteSavedImage(id string) error {
	tarPath := s.FindTarPath(id)
	if tarPath == "" {
		return nil
	}
	metaPath := strings.TrimSuffix(tarPath, ".tar") + ".meta.json"
	os.Remove(metaPath)
	return os.Remove(tarPath)
}

// FindTarPath 根据镜像ID查找对应的 .tar 文件路径
func (s *Service) FindTarPath(id string) string {
	entries, err := os.ReadDir(s.imagesDir)
	if err != nil {
		return ""
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".tar") {
			continue
		}
		metaPath := filepath.Join(s.imagesDir, strings.TrimSuffix(entry.Name(), ".tar")+".meta.json")
		meta, err := readMetaFile(metaPath)
		if err != nil {
			continue
		}
		if meta.ID == id {
			return filepath.Join(s.imagesDir, entry.Name())
		}
	}
	return ""
}

// CleanupStaleDownloads 删除所有残留文件：.downloading 文件、没有对应 .tar 的孤立 .meta.json
func (s *Service) CleanupStaleDownloads() {
	entries, err := os.ReadDir(s.imagesDir)
	if err != nil {
		return
	}

	// 收集所有 .tar 文件对应的 meta 基名
	tarBaseNames := make(map[string]bool)
	for _, entry := range entries {
		name := entry.Name()
		if !entry.IsDir() && strings.HasSuffix(name, ".tar") {
			tarBaseNames[strings.TrimSuffix(name, ".tar")] = true
		}
	}

	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() {
			continue
		}
		// 删除 .downloading 残留
		if strings.HasSuffix(name, ".downloading") {
			os.Remove(filepath.Join(s.imagesDir, name))
			continue
		}
		// 删除没有对应 .tar 的孤立 .meta.json
		if strings.HasSuffix(name, ".meta.json") {
			baseName := strings.TrimSuffix(name, ".meta.json")
			if !tarBaseNames[baseName] {
				os.Remove(filepath.Join(s.imagesDir, name))
			}
		}
	}
}

// DownloadingPath 返回镜像的下载中文件路径
func (s *Service) DownloadingPath(imageURL string) string {
	safeName := SanitizeFileName(imageURL)
	return filepath.Join(s.imagesDir, safeName+".tar.downloading")
}

// FinalPath 返回镜像的最终 .tar 文件路径
func (s *Service) FinalPath(imageURL string) string {
	safeName := SanitizeFileName(imageURL)
	return filepath.Join(s.imagesDir, safeName+".tar")
}

// MetaPath 返回镜像的元数据文件路径
func (s *Service) MetaPath(imageURL string) string {
	safeName := SanitizeFileName(imageURL)
	return filepath.Join(s.imagesDir, safeName+".meta.json")
}

// WriteMetaFile 写入伴生 .meta.json 文件
func (s *Service) WriteMetaFile(imageURL string, meta *SavedImage) error {
	metaPath := s.MetaPath(imageURL)
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(metaPath, data, 0644)
}

// FinalizeDownload 将 .downloading 文件重命名为 .tar
func (s *Service) FinalizeDownload(imageURL string) error {
	src := s.DownloadingPath(imageURL)
	dst := s.FinalPath(imageURL)
	return os.Rename(src, dst)
}

// NewSavedImage 创建镜像元数据
func NewSavedImage(id, name, url, ttype2, osVer, sysVer, sysVerDes, udesc string) *SavedImage {
	return &SavedImage{
		ID:        id,
		Name:      name,
		URL:       url,
		TType2:    ttype2,
		OsVer:     osVer,
		SysVer:    sysVer,
		SysVerDes: sysVerDes,
		UDesc:     udesc,
		SavedAt:   time.Now().Format("2006-01-02 15:04:05"),
	}
}

// readMetaFile 读取 .meta.json 文件
func readMetaFile(path string) (*SavedImage, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var meta SavedImage
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, err
	}
	return &meta, nil
}
