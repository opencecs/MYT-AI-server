package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/moyunteng/myt/internal/config"
	"github.com/moyunteng/myt/internal/sdkapi"
	"github.com/moyunteng/myt/internal/util"
)

type PhoneModelService struct {
	cm        *config.Manager
	sdkApiSvc *SdkApiServiceWrapper
}

func NewPhoneModelService(cm *config.Manager, sdkApiSvc *SdkApiServiceWrapper) *PhoneModelService {
	return &PhoneModelService{cm: cm, sdkApiSvc: sdkApiSvc}
}

func (s *PhoneModelService) modelDir() string {
	dir := filepath.Join(s.cm.GetDataStoragePath(), "model")
	os.MkdirAll(dir, 0777)
	return dir
}

func (s *PhoneModelService) editedDir() string {
	dir := filepath.Join(s.cm.GetDataStoragePath(), "model_edited")
	os.MkdirAll(dir, 0777)
	return dir
}

// modelDirs 返回所有机型目录（线上下载 + 用户编辑）
func (s *PhoneModelService) modelDirs() []string {
	return []string{s.modelDir(), s.editedDir()}
}

func (s *PhoneModelService) HasLocalTemplate(name string) bool {
	for _, dir := range s.modelDirs() {
		matches, _ := filepath.Glob(filepath.Join(dir, name+"*"))
		if len(matches) > 0 {
			return true
		}
	}
	return false
}

func (s *PhoneModelService) DownloadTemplate(modelID string) error {
	log.Printf("[phoneModel] 开始下载 modelID=%s modelDir=%s", modelID, s.modelDir())
	downloadURL, err := s.getTemplateDownloadURL(modelID)
	if err != nil {
		log.Printf("[phoneModel] 获取下载URL失败: %v", err)
		return err
	}
	log.Printf("[phoneModel] 下载URL: %s", downloadURL)

	fileName := filepath.Base(downloadURL)
	savePath := filepath.Join(s.modelDir(), fileName)

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Get(downloadURL)
	if err != nil {
		return fmt.Errorf("下载失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("下载失败: HTTP %d", resp.StatusCode)
	}

	log.Printf("[phoneModel] HTTP 200, 开始保存到 %s", savePath)

	out, err := os.Create(savePath)
	if err != nil {
		return fmt.Errorf("创建文件失败: %w", err)
	}
	defer out.Close()

	if _, err = io.Copy(out, resp.Body); err != nil {
		return fmt.Errorf("保存文件失败: %w", err)
	}
	log.Printf("[phoneModel] 下载完成: %s", savePath)
	return nil
}

func (s *PhoneModelService) PushModelToDevice(deviceIP, modelName string) (string, error) {
	for _, dir := range s.modelDirs() {
		matches, err := filepath.Glob(filepath.Join(dir, modelName+"*"))
		if err != nil || len(matches) == 0 {
			continue
		}
		// 先查设备端已注册机型：命中直接返回注册名（跳过 import）。
		// 重复 import 对已存在机型可能被设备端拒绝；且 import 返回空名时
		// fallback 查"最新注册"会拿到无关机型的名字（mtimestamp 最新的
		// 可能是别的机型）→ SwitchModel 传错名失败。注册名形如
		// "V2133A_v29"（带版本后缀），本地模板名是去后缀的 base 名，按
		// base 或 base_ 前缀匹配。
		if registered, ok := s.findRegisteredModel(deviceIP, modelName); ok {
			return registered, nil
		}
		// 机型 zip 走文件上传接口（公网模式还经网关透传），默认超时不够用，对齐长超时
		deviceName, err := s.sdkApiSvc.newClientWithTimeout(deviceIP, 5*time.Minute).ImportLocalPhoneModel(matches[0])
		if err != nil {
			return "", err
		}
		if deviceName != "" {
			return deviceName, nil
		}
		// import 接口未返回名称，从设备端查询最新注册的机型名
		// （走到这里说明是刚导入的新机型，"最新"即为本机型）
		client := s.sdkApiSvc.newClient(deviceIP)
		entries, listErr := client.ListLocalPhoneModels()
		if listErr == nil && len(entries) > 0 {
			var latest sdkapi.PhoneModelEntry
			for _, e := range entries {
				if e.MTimstamp > latest.MTimstamp {
					latest = e
				}
			}
			if latest.Name != "" {
				return latest.Name, nil
			}
		}
		// 最终兜底：从 zip 文件名推断，去掉版本后缀（_v19 等），与 ListLocalTemplateNames 保持一致
		deviceModelName := versionSuffix.ReplaceAllString(
			strings.TrimSuffix(filepath.Base(matches[0]), filepath.Ext(matches[0])), "")
		return deviceModelName, nil
	}
	return "", fmt.Errorf("未找到本地机型文件: %s", modelName)
}

// findRegisteredModel 查设备端已注册机型：注册名等于 base 名或以 base_ 为前缀
// （版本/时间戳后缀，如 "V2133A_v29"、"RMX3841_0731-175409"）视为命中。
// 查询失败不阻断导入流程（返回 false 走 import 兜底）。
func (s *PhoneModelService) findRegisteredModel(deviceIP, baseName string) (string, bool) {
	entries, err := s.sdkApiSvc.newClient(deviceIP).ListLocalPhoneModels()
	if err != nil {
		return "", false
	}
	for _, e := range entries {
		if e.Name == baseName || strings.HasPrefix(e.Name, baseName+"_") {
			return e.Name, true
		}
	}
	return "", false
}

// versionSuffix 匹配 _v20、_v3 等
var versionSuffix = regexp.MustCompile(`_v\d+$`)

func (s *PhoneModelService) ListLocalTemplateNames() []string {
	seen := make(map[string]bool)
	var names []string
	for _, dir := range s.modelDirs() {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			name := e.Name()
			ext := filepath.Ext(name)
			base := name[:len(name)-len(ext)]
			base = versionSuffix.ReplaceAllString(base, "")
			if !seen[base] {
				seen[base] = true
				names = append(names, base)
			}
		}
	}
	return names
}

func (s *PhoneModelService) getTemplateDownloadURL(modelID string) (string, error) {
	url := fmt.Sprintf("https://newapi.moyunteng.com/api/v1/template/download-url/%s", modelID)
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return "", fmt.Errorf("获取下载URL失败: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		CodeID int    `json:"code_id"`
		Msg    string `json:"msg"`
		Data   struct {
			DownloadURL string `json:"download_url"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("解析响应失败: %w", err)
	}
	if result.CodeID != 200 {
		return "", fmt.Errorf("获取下载URL失败: %s", result.Msg)
	}
	return result.Data.DownloadURL, nil
}

// --- Wails 绑定方法 ---

func (s *PhoneModelService) OpenModelDir() error {
	dir := s.modelDir()
	if err := os.MkdirAll(dir, 0777); err != nil {
		return fmt.Errorf("创建目录失败: %w", err)
	}
	return util.OpenDirectory(dir)
}

func (s *PhoneModelService) OpenEditedDir() error {
	dir := s.editedDir()
	if err := os.MkdirAll(dir, 0777); err != nil {
		return fmt.Errorf("创建目录失败: %w", err)
	}
	return util.OpenDirectory(dir)
}

type EditedModelInfo struct {
	Name    string `json:"name"`
	Size    int64  `json:"size"`
	ModTime string `json:"modTime"`
}

func (s *PhoneModelService) ListEditedModels() []EditedModelInfo {
	dir := s.editedDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var list []EditedModelInfo
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".zip") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		name := e.Name()
		ext := filepath.Ext(name)
		base := name[:len(name)-len(ext)]
		list = append(list, EditedModelInfo{
			Name:    base,
			Size:    info.Size(),
			ModTime: info.ModTime().Format("2006-01-02 15:04"),
		})
	}
	return list
}

func (s *PhoneModelService) DeleteEditedModel(name string) error {
	dir := s.editedDir()
	matches, _ := filepath.Glob(filepath.Join(dir, name+"*"))
	if len(matches) == 0 {
		return fmt.Errorf("未找到编辑机型: %s", name)
	}
	return os.Remove(matches[0])
}

func (s *PhoneModelService) DownloadPhoneModel(modelID string) error {
	return s.DownloadTemplate(modelID)
}

func (s *PhoneModelService) HasPhoneModel(name string) bool {
	return s.HasLocalTemplate(name)
}

func (s *PhoneModelService) PushPhoneModel(deviceIP, modelName string) (string, error) {
	return s.PushModelToDevice(deviceIP, modelName)
}

func (s *PhoneModelService) GetLocalModelNames() []string {
	return s.ListLocalTemplateNames()
}

// GetModelCfg 读取指定机型的 cfg.json 内容
func (s *PhoneModelService) GetModelCfg(modelName string) string {
	for _, dir := range s.modelDirs() {
		matches, _ := filepath.Glob(filepath.Join(dir, modelName+"*"))
		if len(matches) == 0 {
			continue
		}
		r, err := zip.OpenReader(matches[0])
		if err != nil {
			continue
		}
		defer r.Close()
		for _, f := range r.File {
			if f.Name == "cfg.json" {
				rc, err := f.Open()
				if err != nil {
					continue
				}
				data, _ := io.ReadAll(rc)
				rc.Close()
				return string(data)
			}
		}
	}
	return "{}"
}

// GetModelDeviceInfo 读取指定机型 device_info.json 的原始 JSON 文本，
// 供前端编辑弹窗回填（cfg.json 不含 sim/net/kernel/drm 等分区）。
// 条目可能带目录前缀，统一走 findZipEntryName 定位。
func (s *PhoneModelService) GetModelDeviceInfo(modelName string) string {
	for _, dir := range s.modelDirs() {
		matches, _ := filepath.Glob(filepath.Join(dir, modelName+"*"))
		if len(matches) == 0 {
			continue
		}
		outer, err := readZipFile(matches[0])
		if err != nil {
			log.Printf("[phoneModel] GetModelDeviceInfo 读取 %s 失败: %v", matches[0], err)
			continue
		}
		// 外层找内层 .zip 条目（与 EditPhoneModel 同判定）
		var innerBytes []byte
		for name, data := range outer {
			if strings.HasSuffix(name, ".zip") && !strings.Contains(name, "/") {
				innerBytes = data
				break
			}
		}
		if innerBytes == nil {
			// 单层包结构：device_info.json 直接在外层
			if data, ok := lookupZipEntry(outer, "device_info.json"); ok {
				return string(data)
			}
			continue
		}
		if data, ok := findInnerZipEntry(innerBytes, "device_info.json"); ok {
			return string(data)
		}
	}
	log.Printf("[phoneModel] GetModelDeviceInfo: 未在机型 %s 中找到 device_info.json", modelName)
	return "{}"
}

// lookupZipEntry 在外层 zip 的 name→content 映射里按 base 定位条目
func lookupZipEntry(files map[string][]byte, base string) ([]byte, bool) {
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	name, ok := findZipEntryName(names, base)
	if !ok {
		return nil, false
	}
	return files[name], true
}

// findInnerZipEntry 在内层 zip 字节里定位 base 条目并读取内容
func findInnerZipEntry(innerBytes []byte, base string) ([]byte, bool) {
	r, err := zip.NewReader(bytes.NewReader(innerBytes), int64(len(innerBytes)))
	if err != nil {
		return nil, false
	}
	names := make([]string, 0, len(r.File))
	for _, f := range r.File {
		names = append(names, f.Name)
	}
	name, ok := findZipEntryName(names, base)
	if !ok {
		return nil, false
	}
	for _, f := range r.File {
		if f.Name != name {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, false
		}
		data, _ := io.ReadAll(rc)
		rc.Close()
		return data, true
	}
	return nil, false
}
