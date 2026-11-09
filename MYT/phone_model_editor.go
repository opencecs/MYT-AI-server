package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ModelEditParams 机型编辑参数——非空字段才会被修改
type ModelEditParams struct {
	Model            string            `json:"model"`
	Brand            string            `json:"brand"`
	Manufacturer     string            `json:"manufacturer"`
	AndroidVersion   string            `json:"androidVersion"`
	SecurityPatch    string            `json:"securityPatch"`
	Width            int               `json:"width"`
	Height           int               `json:"height"`
	DPI              int               `json:"dpi"`
	RefreshRate      int               `json:"refreshRate"`
	RandomizeIDs     bool              `json:"randomizeIDs"`
	OverlayKeys      []string          `json:"overlayKeys"`
	Prop             map[string]string `json:"prop"`
	SupportedVersion string            `json:"supportedVersion"`

	// overlay 值覆盖：key→value，value可以是正则表达式或固定值
	Overlay map[string]string `json:"overlay"`
	// dg_config 参数覆盖：模块→key→value
	SimConfig    map[string]string `json:"simConfig"`
	NetConfig    map[string]string `json:"netConfig"`
	KernelConfig map[string]string `json:"kernelConfig"`

	// Build 补全字段
	BuildDevice      string `json:"buildDevice"`
	BuildID          string `json:"buildId"`
	BuildIncremental string `json:"buildIncremental"`
	BuildBoard       string `json:"buildBoard"`
	BuildHardware    string `json:"buildHardware"`
	BuildHost        string `json:"buildHost"`
	BuildTime        string `json:"buildTime"`
	BuildUser        string `json:"buildUser"`
	BuildBootloader  string `json:"buildBootloader"`

	// 扩展配置
	DrmConfig       map[string]string `json:"drmConfig"`
	GpuConfig       map[string]string `json:"gpuConfig"`
	SystemConfig    map[string]string `json:"systemConfig"`
	PmConfig        map[string]string `json:"pmConfig"`
	SimPresetConfig map[string]string `json:"simPresetConfig"`
	LocaleConfig    map[string]string `json:"localeConfig"`
}

// EditPhoneModel 编辑已下载的机型，生成新的机型包
func (s *PhoneModelService) EditPhoneModel(sourceName string, paramsJSON string) error {
	var params ModelEditParams
	if err := json.Unmarshal([]byte(paramsJSON), &params); err != nil {
		return fmt.Errorf("解析参数失败: %w", err)
	}

	log.Printf("[edit] EditPhoneModel source=%s params=%s", sourceName, paramsJSON)

	var matches []string
	for _, dir := range s.modelDirs() {
		m, err := filepath.Glob(filepath.Join(dir, sourceName+"*"))
		if err == nil && len(m) > 0 {
			matches = m
			break
		}
	}
	if len(matches) == 0 {
		return fmt.Errorf("未找到源机型文件: %s", sourceName)
	}

	outer, err := readZipFile(matches[0])
	if err != nil {
		return fmt.Errorf("读取zip失败: %w", err)
	}

	var innerBytes []byte
	var cfgMap map[string]interface{}
	var ktBytes []byte

	for name, data := range outer {
		if strings.HasSuffix(name, ".zip") && !strings.Contains(name, "/") {
			innerBytes = data
		} else if name == "cfg.json" {
			json.Unmarshal(data, &cfgMap)
		} else if strings.HasPrefix(filepath.Base(name), ".") {
			ktBytes = data
		}
	}

	if innerBytes == nil {
		return fmt.Errorf("zip中未找到内层机型数据")
	}

	// 修改内层 zip
	modifiedInner, err := s.modifyInnerZip(innerBytes, &params)
	if err != nil {
		return fmt.Errorf("修改机型数据失败: %w", err)
	}

	// 修改 cfg.json
	if cfgMap != nil {
		if params.Model != "" {
			cfgMap["model"] = params.Model
		}
		if params.AndroidVersion != "" {
			cfgMap["android_version"] = params.AndroidVersion
		}
		if params.SecurityPatch != "" {
			if prop, ok := cfgMap["prop"].(map[string]interface{}); ok {
				prop["ro.build.version.security_patch"] = params.SecurityPatch
				prop["ro.vendor.build.security_patch"] = params.SecurityPatch
			}
		}
		// model/brand/manufacturer/build 变化时同步 cfg.json 的 prop
		if prop, ok := cfgMap["prop"].(map[string]interface{}); ok {
			if params.Model != "" {
				prop["ro.product.model"] = params.Model
			}
			if params.Brand != "" {
				prop["ro.product.brand"] = params.Brand
			}
			if params.Manufacturer != "" {
				prop["ro.product.manufacturer"] = params.Manufacturer
			}
			if params.BuildDevice != "" {
				prop["ro.product.device"] = params.BuildDevice
			}
			if params.BuildID != "" {
				prop["ro.build.display.id"] = params.BuildID
			}
			if params.BuildIncremental != "" {
				prop["ro.build.version.incremental"] = params.BuildIncremental
			}
			if params.BuildUser != "" {
				prop["ro.build.user"] = params.BuildUser
			}
		}
		if !params.RandomizeIDs {
			delete(cfgMap, "overlay")
		} else if len(params.OverlayKeys) > 0 {
			overlay, _ := cfgMap["overlay"].(map[string]interface{})
			if overlay == nil {
				overlay = make(map[string]interface{})
			}
			enabledSet := make(map[string]bool, len(params.OverlayKeys))
			for _, k := range params.OverlayKeys {
				enabledSet[k] = true
			}
			for k := range overlay {
				if !enabledSet[k] {
					delete(overlay, k)
				}
			}
			cfgMap["overlay"] = overlay
		} else if params.RandomizeIDs {
			// randomizeIDs=true 但未指定 overlayKeys，保留所有现有 overlay key
			if _, ok := cfgMap["overlay"]; !ok {
				cfgMap["overlay"] = make(map[string]interface{})
			}
		}
		if len(params.Overlay) > 0 {
			overlay, _ := cfgMap["overlay"].(map[string]interface{})
			if overlay == nil {
				overlay = make(map[string]interface{})
			}
			for k, v := range params.Overlay {
				overlay[k] = v
			}
			cfgMap["overlay"] = overlay
		}
		if len(params.Prop) > 0 {
			prop, _ := cfgMap["prop"].(map[string]interface{})
			if prop == nil {
				prop = make(map[string]interface{})
			}
			for k, v := range params.Prop {
				if v == "" {
					delete(prop, k)
				} else {
					prop[k] = v
				}
			}
			cfgMap["prop"] = prop
		}
		if params.SupportedVersion != "" {
			cfgMap["supported_version"] = params.SupportedVersion
		}
	}

	// 新文件名——使用时间戳确保唯一
	newName := params.Model
	if newName == "" {
		newName = sourceName
	}
	newInnerName := newName + ".zip"
	ts := time.Now().Format("0102-150405")
	newFilePath := filepath.Join(s.editedDir(), fmt.Sprintf("%s_%s.zip", newName, ts))

	outFile, err := os.Create(newFilePath)
	if err != nil {
		return fmt.Errorf("创建文件失败: %w", err)
	}
	defer outFile.Close()

	zw := zip.NewWriter(outFile)
	defer zw.Close()

	// 写入内层 zip
	w, err := zw.Create(newInnerName)
	if err != nil {
		return fmt.Errorf("创建zip条目失败: %w", err)
	}
	if _, err := w.Write(modifiedInner); err != nil {
		return fmt.Errorf("写入zip条目失败: %w", err)
	}

	// 写入 cfg.json
	if cfgMap != nil {
		w, err = zw.Create("cfg.json")
		if err != nil {
			return fmt.Errorf("创建cfg.json条目失败: %w", err)
		}
		cfgBytes, err := json.MarshalIndent(cfgMap, "", "  ")
		if err != nil {
			return fmt.Errorf("序列化cfg.json失败: %w", err)
		}
		if _, err := w.Write(cfgBytes); err != nil {
			return fmt.Errorf("写入cfg.json失败: %w", err)
		}
	}

	// 写入 .kt
	if ktBytes != nil {
		w, err = zw.Create(".kt")
		if err != nil {
			return fmt.Errorf("创建.kt条目失败: %w", err)
		}
		if _, err := w.Write(ktBytes); err != nil {
			return fmt.Errorf("写入.kt失败: %w", err)
		}
	}

	return nil
}

// findZipEntryName 在 zip 条目名列表中定位目标文件：
// 先精确匹配；否则按 filepath.Base 匹配（条目可能带目录前缀），
// base 命中唯一才返回，多个命中视为不明确返回 false。
func findZipEntryName(order []string, base string) (string, bool) {
	for _, n := range order {
		if n == base {
			return n, true
		}
	}
	var hits []string
	for _, n := range order {
		if filepath.Base(n) == base {
			hits = append(hits, n)
		}
	}
	if len(hits) == 1 {
		return hits[0], true
	}
	return "", false
}

func (s *PhoneModelService) modifyInnerZip(data []byte, params *ModelEditParams) ([]byte, error) {
	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}

	// 读取所有文件
	files := make(map[string][]byte)
	var order []string
	for _, f := range r.File {
		rc, err := f.Open()
		if err != nil {
			continue
		}
		content, _ := io.ReadAll(rc)
		rc.Close()
		files[f.Name] = content
		order = append(order, f.Name)
	}

	// 修改 device_info.json（条目可能带目录前缀，如 "PKR110_16/device_info.json"）
	diName, ok := findZipEntryName(order, "device_info.json")
	if !ok {
		log.Printf("[edit] modifyInnerZip: 内层zip未找到 device_info.json 条目，跳过 device/dg_config/扩展配置修改")
	} else {
		var di map[string]interface{}
		if err := json.Unmarshal(files[diName], &di); err != nil {
			log.Printf("[edit] modifyInnerZip: 解析 %s 失败: %v", diName, err)
		} else {
			applyDeviceEdits(di, params)
			applyDgConfigEdits(di, params)
			if b, err := json.Marshal(di); err == nil {
				files[diName] = b
			}
		}
	}

	// 修改 display.json（同样支持带前缀条目）
	dispName, ok := findZipEntryName(order, "display.json")
	if !ok {
		log.Printf("[edit] modifyInnerZip: 内层zip未找到 display.json 条目，跳过屏幕参数修改")
	} else {
		var disp map[string]interface{}
		if err := json.Unmarshal(files[dispName], &disp); err != nil {
			log.Printf("[edit] modifyInnerZip: 解析 %s 失败: %v", dispName, err)
		} else {
			applyDisplayEdits(disp, params)
			if b, err := json.Marshal(disp); err == nil {
				files[dispName] = b
			}
		}
	}

	// 重新打包
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, name := range order {
		w, _ := zw.Create(name)
		w.Write(files[name])
	}
	zw.Close()
	return buf.Bytes(), nil
}

// readZipFile 读取 zip 文件，返回 name→content 映射
func readZipFile(path string) (map[string][]byte, error) {
	r, err := zip.OpenReader(path)
	if err != nil {
		return nil, err
	}
	defer r.Close()

	result := make(map[string][]byte)
	for _, f := range r.File {
		rc, err := f.Open()
		if err != nil {
			continue
		}
		data, _ := io.ReadAll(rc)
		rc.Close()
		result[f.Name] = data
	}
	return result, nil
}
