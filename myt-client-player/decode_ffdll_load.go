//go:build windows

package main

import (
	"fmt"
	"os"
	"path/filepath"

	"crypto/sha256"

	"golang.org/x/sys/windows"
)

// loadFFDecode 提取并加载 go_legacy_ffdecode.dll，解析导出符号。
// 首次调用用 sync.Once 保证只加载一次；失败缓存到 ffLoadError。
func loadFFDecode() error {
	ffInitOnce.Do(func() {
		dllName := "go_legacy_ffdecode.dll"

		exePath, _ := os.Executable()
		candidates := []string{
			filepath.Dir(exePath),
			os.TempDir(),
		}
		if dir, ok := os.LookupEnv("LOCALAPPDATA"); ok && dir != "" {
			candidates = append(candidates, dir)
		}
		if dir, ok := os.LookupEnv("USERPROFILE"); ok && dir != "" {
			candidates = append(candidates, dir)
		}

		var lastErr error
		for _, dir := range candidates {
			dllPath := filepath.Join(dir, dllName)

			extract := true
			// 不能只比较大小：新版 dll 大小可能碰巧与旧版相同，导致跳过更新跑旧 dll。
			if data, err := os.ReadFile(dllPath); err == nil && sha256.Sum256(data) == sha256.Sum256(ffdecodeDLL) {
				extract = false
			}
			if extract {
				if err := os.WriteFile(dllPath, ffdecodeDLL, 0o644); err != nil {
					lastErr = fmt.Errorf("ffdecode: write %s: %w", dllPath, err)
					continue
				}
			}

			handle, err := windows.LoadLibrary(dllPath)
			if err != nil {
				lastErr = fmt.Errorf("ffdecode: load %s: %w", dllPath, err)
				continue
			}
			lastErr = nil
			if err := resolveFFProcs(handle); err != nil {
				windows.FreeLibrary(handle)
				lastErr = err
				continue
			}
			return
		}

		ffLoadError = lastErr
	})
	return ffLoadError
}

// resolveFFProcs 解析 ffdecode.dll 的导出符号到 ffProc* 全局变量。
// 必需符号缺失则报错回退；可选符号缺失则跳过（旧版 dll 兼容）。
func resolveFFProcs(handle windows.Handle) error {
	requiredProcs := []struct {
		name string
		dst  *uintptr
	}{
		{"FF_Init", &ffProcInit},
		{"FF_CreateDecoder", &ffProcCreateDecoder},
		{"FF_DestroyDecoder", &ffProcDestroyDecoder},
		{"FF_Decode", &ffProcDecode},
	}
	for _, p := range requiredProcs {
		addr, err := windows.GetProcAddress(handle, p.name)
		if err != nil {
			return fmt.Errorf("ffdecode: %s not found: %w", p.name, err)
		}
		*p.dst = uintptr(addr)
	}

	optionalProcs := []struct {
		name string
		dst  *uintptr
	}{
		{"FF_Flush", &ffProcFlush},
		{"FF_CreateAACDecoder", &ffProcCreateAACDecoder},
		{"FF_DecodeAAC", &ffProcDecodeAAC},
		{"FF_DestroyAACDecoder", &ffProcDestroyAACDecoder},
		{"FF_GetDecoderInfo", &ffProcGetDecoderInfo},
		{"FF_GetD3D11Device", &ffProcGetD3D11Device},
		{"FF_SetD3D11Device", &ffProcSetD3D11Device},
		{"FF_ReceiveBGRA", &ffProcReceiveBGRA},
		{"FF_DecodeTexture", &ffProcDecodeTexture},
		{"FF_ReceiveTexture", &ffProcReceiveTexture},
		{"FF_UnrefFrame", &ffProcUnrefFrame},
		{"FF_SetHWAccel", &ffProcSetHWAccel},
		{"FF_SetTextureOutput", &ffProcSetTextureOutput},
		{"FF_ReopenSoftware", &ffProcReopenSoftware},
		{"FF_IsSoftware", &ffProcIsSoftware},
		{"FF_GetFrameErrorFlags", &ffProcFrameErrFlags},
	}
	for _, p := range optionalProcs {
		addr, err := windows.GetProcAddress(handle, p.name)
		if err == nil {
			*p.dst = uintptr(addr)
		}
	}
	return nil
}
