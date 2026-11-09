package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"

	"github.com/moyunteng/myt/internal/config"
	"github.com/moyunteng/myt/internal/ports"
	"github.com/moyunteng/myt/internal/util"
)

// UploadService 文件上传服务
type UploadService struct {
	cm        *config.Manager
	mirrorSvc *MirrorServiceWrapper
	sdkApiSvc *SdkApiServiceWrapper
}

func NewUploadService(cm *config.Manager) *UploadService {
	return &UploadService{cm: cm}
}

// SetMirrorService 注入镜像服务（用于按镜像类型分派 mytos 客户端）
func (u *UploadService) SetMirrorService(ms *MirrorServiceWrapper) { u.mirrorSvc = ms }

// SetSdkApiService 注入 SDK API 服务（用于公网模式获取 token）
func (u *UploadService) SetSdkApiService(svc *SdkApiServiceWrapper) { u.sdkApiSvc = svc }

// UploadResult 单次上传结果
type UploadResult struct {
	DeviceIP    string `json:"deviceIP"`
	AndroidName string `json:"androidName"`
	Success     bool   `json:"success"`
	Error       string `json:"error,omitempty"`
	RemotePath  string `json:"remotePath,omitempty"`
}

// UploadTarget 上传目标
type UploadTarget struct {
	DeviceIP    string `json:"deviceIP"`
	AndroidName string `json:"androidName"`
}

// DirectoryNode 目录树节点
type DirectoryNode struct {
	Name     string          `json:"name"`
	Path     string          `json:"path"`
	IsDir    bool            `json:"isDir"`
	Size     int64           `json:"size"`
	ModTime  int64           `json:"modTime"`
	Children []DirectoryNode `json:"children"`
}

// SharedDirPathInfo 共享目录路径信息
type SharedDirPathInfo struct {
	Path        string `json:"path"`
	IsDefault   bool   `json:"isDefault"`
	DefaultPath string `json:"defaultPath"`
}

// UploadFile 上传文件到指定云机
func (u *UploadService) UploadFile(deviceIP, androidName, filePath string) UploadResult {
	androids, err := u.getAndroid(deviceIP, androidName)
	if err != nil {
		return UploadResult{DeviceIP: deviceIP, AndroidName: androidName, Error: err.Error()}
	}

	// 公网模式：走网关透传
	if u.cm != nil && u.cm.GetDeployMode() == "public" && u.sdkApiSvc != nil {
		token, err := u.sdkApiSvc.getPublicToken()
		if err != nil {
			return UploadResult{DeviceIP: deviceIP, AndroidName: androidName, Error: err.Error()}
		}
		client := newPublicMytosClientForAndroid(u.mirrorSvc, androids, deviceIP, token)
		remotePath, err := client.UploadFile(filePath)
		if err != nil {
			return UploadResult{DeviceIP: deviceIP, AndroidName: androidName, Error: err.Error()}
		}
		return UploadResult{DeviceIP: deviceIP, AndroidName: androidName, Success: true, RemotePath: remotePath}
	}

	var addr ports.Addr
	if ports.IsMacvlanDirect(androids.NetworkName, androids.PortBindings) && androids.ContainerIP != "" {
		addr = ports.MYTOSDirect(*androids)
	} else {
		// 主机地址用 androids.IP：v2/public 下传入的 deviceIP 可能是序列号，不能当主机名连
		addr = ports.MYTOS(*androids, androids.IP)
	}

	client := newMytosClientForAndroid(u.mirrorSvc, androids, addr.Host, addr.Port)
	remotePath, err := client.UploadFile(filePath)
	if err != nil {
		return UploadResult{DeviceIP: deviceIP, AndroidName: androidName, Error: err.Error()}
	}
	return UploadResult{DeviceIP: deviceIP, AndroidName: androidName, Success: true, RemotePath: remotePath}
}

// BatchUpload 批量上传文件到多台云机
func (u *UploadService) BatchUpload(targets []UploadTarget, filePath string) []UploadResult {
	results := make([]UploadResult, len(targets))
	var wg sync.WaitGroup
	for i, t := range targets {
		wg.Add(1)
		go func(idx int, target UploadTarget) {
			defer wg.Done()
			results[idx] = u.UploadFile(target.DeviceIP, target.AndroidName, filePath)
		}(i, t)
	}
	wg.Wait()
	return results
}

func (u *UploadService) getAndroid(deviceIP, name string) (*config.AndroidInfo, error) {
	if u.cm == nil {
		return nil, fmt.Errorf("配置管理器未初始化")
	}
	for _, a := range u.cm.GetAndroidList() {
		if a.Name == name && (a.IP == deviceIP || a.DeviceID == deviceIP) {
			return &a, nil
		}
	}
	return nil, fmt.Errorf("未找到云机 %s@%s", name, deviceIP)
}

// --- 共享目录相关 API ---

const sharedDirPathKey = "shared_dir_path.txt"

// getDefaultSharedDirPath 获取默认共享目录路径（程序根目录/data/shared）
func (u *UploadService) getDefaultSharedDirPath() string {
	if u.cm != nil {
		dir := filepath.Join(u.cm.RootDir(), "data", "shared")
		_ = os.MkdirAll(dir, 0755)
		return dir
	}
	// fallback: 使用系统缓存目录
	dataDir, err := os.UserCacheDir()
	if err != nil {
		dataDir = os.TempDir()
	}
	dir := filepath.Join(dataDir, "myt", "shared")
	_ = os.MkdirAll(dir, 0755)
	return dir
}

// getCustomSharedDirPath 获取实际使用的共享目录（优先使用用户自定义路径）
func (u *UploadService) getCustomSharedDirPath() string {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return u.getDefaultSharedDirPath()
	}
	cfgFile := filepath.Join(configDir, "myt", sharedDirPathKey)
	data, err := os.ReadFile(cfgFile)
	if err != nil || len(strings.TrimSpace(string(data))) == 0 {
		return u.getDefaultSharedDirPath()
	}
	customPath := strings.TrimSpace(string(data))
	if err := os.MkdirAll(customPath, 0755); err != nil {
		log.Printf("[getCustomSharedDirPath] 自定义路径无效(%s): %v，使用默认路径", customPath, err)
		return u.getDefaultSharedDirPath()
	}
	return customPath
}

// GetSharedDirPath 获取当前共享目录路径
func (u *UploadService) GetSharedDirPath() SharedDirPathInfo {
	configDir, err := os.UserConfigDir()
	defaultPath := u.getDefaultSharedDirPath()
	if err != nil {
		return SharedDirPathInfo{Path: defaultPath, IsDefault: true, DefaultPath: defaultPath}
	}
	cfgFile := filepath.Join(configDir, "myt", sharedDirPathKey)
	data, err := os.ReadFile(cfgFile)
	if err != nil || len(strings.TrimSpace(string(data))) == 0 {
		return SharedDirPathInfo{Path: defaultPath, IsDefault: true, DefaultPath: defaultPath}
	}
	customPath := strings.TrimSpace(string(data))
	if err2 := os.MkdirAll(customPath, 0755); err2 != nil {
		log.Printf("[GetSharedDirPath] 自定义路径无效(%s): %v，自动恢复默认路径", customPath, err2)
		_ = os.Remove(cfgFile)
		return SharedDirPathInfo{Path: defaultPath, IsDefault: true, DefaultPath: defaultPath}
	}
	return SharedDirPathInfo{Path: customPath, IsDefault: false, DefaultPath: defaultPath}
}

// SetSharedDirPath 设置共享目录路径
func (u *UploadService) SetSharedDirPath(path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		// 恢复默认
		configDir, err := os.UserConfigDir()
		if err != nil {
			return fmt.Errorf("获取配置目录失败: %w", err)
		}
		cfgFile := filepath.Join(configDir, "myt", sharedDirPathKey)
		_ = os.Remove(cfgFile)
		return nil
	}
	if err := os.MkdirAll(path, 0755); err != nil {
		return fmt.Errorf("路径无效或无法创建: %w", err)
	}
	configDir, err := os.UserConfigDir()
	if err != nil {
		return fmt.Errorf("获取配置目录失败: %w", err)
	}
	cfgDir := filepath.Join(configDir, "myt")
	_ = os.MkdirAll(cfgDir, 0777)
	cfgFile := filepath.Join(cfgDir, sharedDirPathKey)
	if err := os.WriteFile(cfgFile, []byte(path), 0666); err != nil {
		return fmt.Errorf("保存路径失败: %w", err)
	}
	log.Printf("[SetSharedDirPath] 保存成功: %s", path)
	return nil
}

// ListSharedDirFiles 列出共享目录中的文件
func (u *UploadService) ListSharedDirFiles() (*DirectoryNode, error) {
	sharedPath := u.getCustomSharedDirPath()
	if _, err := os.Stat(sharedPath); os.IsNotExist(err) {
		if err := os.MkdirAll(sharedPath, 0755); err != nil {
			return nil, fmt.Errorf("创建共享目录失败: %w", err)
		}
	}
	tree, err := buildDirectoryTree(sharedPath, sharedPath)
	if err != nil {
		return nil, fmt.Errorf("读取共享目录失败: %w", err)
	}
	return tree, nil
}

// OpenSharedDirectory 打开共享目录
func (u *UploadService) OpenSharedDirectory() error {
	sharedPath := u.getCustomSharedDirPath()
	if err := os.MkdirAll(sharedPath, 0755); err != nil {
		return fmt.Errorf("创建共享目录失败: %w", err)
	}
	return util.OpenDirectory(sharedPath)
}

// buildDirectoryTree 构建目录树
func buildDirectoryTree(rootPath, currentPath string) (*DirectoryNode, error) {
	info, err := os.Stat(currentPath)
	if err != nil {
		return nil, err
	}

	node := &DirectoryNode{
		Name:    filepath.Base(currentPath),
		Path:    currentPath,
		IsDir:   info.IsDir(),
		Size:    info.Size(),
		ModTime: info.ModTime().Unix(),
	}

	if info.IsDir() {
		entries, err := os.ReadDir(currentPath)
		if err != nil {
			return node, nil
		}
		// 排序：目录在前，文件在后，各自按名称排序
		sort.Slice(entries, func(i, j int) bool {
			if entries[i].IsDir() != entries[j].IsDir() {
				return entries[i].IsDir()
			}
			return strings.ToLower(entries[i].Name()) < strings.ToLower(entries[j].Name())
		})
		for _, entry := range entries {
			entryPath := filepath.Join(currentPath, entry.Name())
			childNode, err := buildDirectoryTree(rootPath, entryPath)
			if err != nil {
				continue
			}
			node.Children = append(node.Children, *childNode)
		}
	}

	return node, nil
}

// SelectDirectory 打开目录选择对话框（简化版：使用系统命令打开目录让用户操作）
func (u *UploadService) SelectDirectory(startPath string) (string, error) {
	// Wails3 暂无内置 SelectFolderDialog，用 explorer 打开让用户手动操作
	// 后续可接入 cfd 库实现原生目录选择
	path := u.getCustomSharedDirPath()
	if startPath != "" {
		path = startPath
	}
	cmd := "explorer"
	args := []string{path}
	switch runtime.GOOS {
	case "darwin":
		cmd = "open"
	case "linux":
		cmd = "xdg-open"
	}
	if err := exec.Command(cmd, args...).Start(); err != nil {
		return "", fmt.Errorf("打开目录失败: %w", err)
	}
	return "", fmt.Errorf("请直接在打开的目录中操作，然后点击刷新")
}
