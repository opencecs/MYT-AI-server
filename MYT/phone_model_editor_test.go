package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"testing"
)

// buildInnerZip 构造内存内层 zip，可指定条目名前缀
func buildInnerZip(t *testing.T, prefix string) []byte {
	t.Helper()
	di := map[string]interface{}{
		"base": map[string]interface{}{"deviceName": "OldModel", "deviceId": "xiaomi_oldmodel_olddev_android14"},
		"prop": map[string]interface{}{
			"Build": map[string]interface{}{
				"Build.MODEL":      "OldModel",
				"Build.BRAND":      "Xiaomi",
				"VERSION.RELEASE":  "14",
				"Build.DEVICE":     "olddev",
			},
		},
		"sim": map[string]interface{}{
			"dg_config": map[string]interface{}{
				"sim.imei": "860000000000001",
			},
		},
	}
	disp := map[string]interface{}{
		"display_config": map[string]interface{}{
			"data.display.width":  1080,
			"data.display.height": 2400,
		},
	}
	diBytes, _ := json.Marshal(di)
	dispBytes, _ := json.Marshal(disp)

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range []struct{ name string; data []byte }{
		{prefix + "device_info.json", diBytes},
		{prefix + "display.json", dispBytes},
	} {
		w, err := zw.Create(e.name)
		if err != nil {
			t.Fatalf("创建zip条目失败: %v", err)
		}
		if _, err := w.Write(e.data); err != nil {
			t.Fatalf("写入zip条目失败: %v", err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("关闭zip失败: %v", err)
	}
	return buf.Bytes()
}

// TestFindZipEntryName 验证精确匹配 / 前缀条目命中 / 多义不命中
func TestFindZipEntryName(t *testing.T) {
	order := []string{"sub/device_info.json", "sub/display.json", "cfg.json"}

	if n, ok := findZipEntryName(order, "cfg.json"); !ok || n != "cfg.json" {
		t.Errorf("精确匹配失败: got %q %v", n, ok)
	}
	if n, ok := findZipEntryName(order, "device_info.json"); !ok || n != "sub/device_info.json" {
		t.Errorf("前缀条目匹配失败: got %q %v", n, ok)
	}
	if _, ok := findZipEntryName(order, "display.json"); !ok {
		t.Errorf("前缀条目 display.json 匹配失败")
	}
	if _, ok := findZipEntryName(order, "missing.json"); ok {
		t.Errorf("不存在的条目不应命中")
	}
	// 多个 base 命中视为不明确
	if _, ok := findZipEntryName([]string{"a/device_info.json", "b/device_info.json"}, "device_info.json"); ok {
		t.Errorf("多个命中不应返回 true")
	}
}

// TestModifyInnerZipPrefixedEntries 条目带目录前缀时修改不丢失
func TestModifyInnerZipPrefixedEntries(t *testing.T) {
	svc := &PhoneModelService{}
	inner := buildInnerZip(t, "PKR110_16/")

	modified, err := svc.modifyInnerZip(inner, &ModelEditParams{
		Model: "NewModel",
		SimConfig: map[string]string{
			"imei": "860000000000002",
		},
	})
	if err != nil {
		t.Fatalf("modifyInnerZip 失败: %v", err)
	}

	r, err := zip.NewReader(bytes.NewReader(modified), int64(len(modified)))
	if err != nil {
		t.Fatalf("重新解析zip失败: %v", err)
	}
	var di map[string]interface{}
	var found bool
	for _, f := range r.File {
		if f.Name != "PKR110_16/device_info.json" {
			continue
		}
		found = true
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("打开条目失败: %v", err)
		}
		data := new(bytes.Buffer)
		if _, err := data.ReadFrom(rc); err != nil {
			t.Fatalf("读取条目失败: %v", err)
		}
		rc.Close()
		if err := json.Unmarshal(data.Bytes(), &di); err != nil {
			t.Fatalf("解析 device_info.json 失败: %v", err)
		}
	}
	if !found {
		t.Fatalf("修改后的 zip 未保留原条目名 PKR110_16/device_info.json")
	}

	// 条目名必须保持原样（未改名为顶层 device_info.json）
	for _, f := range r.File {
		if f.Name == "device_info.json" {
			t.Errorf("条目名被改成了顶层 device_info.json，应保持原前缀")
		}
	}

	// sim.imei 已更新
	sim, _ := di["sim"].(map[string]interface{})
	dg, _ := sim["dg_config"].(map[string]interface{})
	if dg == nil || dg["sim.imei"] != "860000000000002" {
		t.Errorf("sim.imei 未更新: got %v", dg)
	}

	// Build.MODEL 已更新
	prop, _ := di["prop"].(map[string]interface{})
	build, _ := prop["Build"].(map[string]interface{})
	if build["Build.MODEL"] != "NewModel" {
		t.Errorf("Build.MODEL 未更新: got %v", build["Build.MODEL"])
	}
}

// TestModifyInnerZipPlainEntries 常规无前缀条目仍然正常
func TestModifyInnerZipPlainEntries(t *testing.T) {
	svc := &PhoneModelService{}
	inner := buildInnerZip(t, "")

	modified, err := svc.modifyInnerZip(inner, &ModelEditParams{Model: "NewModel"})
	if err != nil {
		t.Fatalf("modifyInnerZip 失败: %v", err)
	}
	r, err := zip.NewReader(bytes.NewReader(modified), int64(len(modified)))
	if err != nil {
		t.Fatalf("重新解析zip失败: %v", err)
	}
	foundModel := false
	for _, f := range r.File {
		if f.Name != "device_info.json" {
			continue
		}
		rc, _ := f.Open()
		buf := new(bytes.Buffer)
		buf.ReadFrom(rc)
		rc.Close()
		var di map[string]interface{}
		if err := json.Unmarshal(buf.Bytes(), &di); err != nil {
			t.Fatalf("解析失败: %v", err)
		}
		prop := di["prop"].(map[string]interface{})
		build := prop["Build"].(map[string]interface{})
		if build["Build.MODEL"] == "NewModel" {
			foundModel = true
		}
	}
	if !foundModel {
		t.Errorf("无前缀条目的 Build.MODEL 未更新")
	}
}

// TestModifyInnerZipMissingDeviceInfo 缺少 device_info.json 时不 panic 且不误报
func TestModifyInnerZipMissingDeviceInfo(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("other.json")
	w.Write([]byte("{}"))
	zw.Close()

	svc := &PhoneModelService{}
	if _, err := svc.modifyInnerZip(buf.Bytes(), &ModelEditParams{Model: "X"}); err != nil {
		t.Fatalf("缺少 device_info.json 时不应报错: %v", err)
	}
}
