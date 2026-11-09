package main

import (
	"fmt"
	"strconv"
	"strings"
)

func applyDeviceEdits(di map[string]interface{}, params *ModelEditParams) {
	prop, _ := di["prop"].(map[string]interface{})
	build, _ := prop["Build"].(map[string]interface{})
	if build == nil {
		return
	}

	// 基础信息
	if params.Model != "" {
		build["Build.MODEL"] = params.Model
		build["Build.PRODUCT"] = params.Model
		if base, ok := di["base"].(map[string]interface{}); ok {
			base["deviceName"] = params.Model
		}
	}
	if params.Brand != "" {
		build["Build.BRAND"] = params.Brand
	}
	if params.Manufacturer != "" {
		build["Build.MANUFACTURER"] = params.Manufacturer
	}
	if params.AndroidVersion != "" {
		build["VERSION.RELEASE"] = params.AndroidVersion
		sdkMap := map[string]string{"10": "29", "11": "30", "12": "31", "13": "33", "14": "34", "15": "35", "16": "36"}
		sdk := params.AndroidVersion
		if v, ok := sdkMap[params.AndroidVersion]; ok {
			sdk = v
			build["VERSION.SDK"] = sdk
			if n, err := strconv.Atoi(sdk); err == nil {
				build["VERSION.SDK_INT"] = n
			}
		}
		if base, ok := di["base"].(map[string]interface{}); ok {
			if did, ok := base["deviceId"].(string); ok {
				base["deviceId"] = rebuildDeviceId(did, params.Brand, params.Model, sdk)
			}
		}
	} else {
		// androidVersion 未改但 brand/model 变了，也需更新 deviceId 的品牌/机型段
		if base, ok := di["base"].(map[string]interface{}); ok {
			if did, ok := base["deviceId"].(string); ok {
				base["deviceId"] = rebuildDeviceId(did, params.Brand, params.Model, "")
			}
		}
	}
	if params.SecurityPatch != "" {
		build["VERSION.SECURITY_PATCH"] = params.SecurityPatch
	}

	// Build 补全字段
	if params.BuildDevice != "" {
		build["Build.DEVICE"] = params.BuildDevice
	}
	if params.BuildID != "" {
		build["Build.ID"] = params.BuildID
	}
	if params.BuildIncremental != "" {
		build["VERSION.INCREMENTAL"] = params.BuildIncremental
	}
	if params.BuildBoard != "" {
		build["Build.BOARD"] = params.BuildBoard
	}
	if params.BuildHardware != "" {
		build["Build.HARDWARE"] = params.BuildHardware
	}
	if params.BuildHost != "" {
		build["Build.HOST"] = params.BuildHost
	}
	if params.BuildTime != "" {
		if n, err := strconv.ParseInt(params.BuildTime, 10, 64); err == nil {
			build["Build.TIME"] = n
		} else {
			build["Build.TIME"] = params.BuildTime
		}
	}
	if params.BuildUser != "" {
		build["Build.USER"] = params.BuildUser
	}
	if params.BuildBootloader != "" {
		build["Build.BOOTLOADER"] = params.BuildBootloader
	}

	// 自动更新 FINGERPRINT（使用最新的 Build 字段）
	brand, _ := build["Build.BRAND"].(string)
	model, _ := build["Build.MODEL"].(string)
	device, _ := build["Build.DEVICE"].(string)
	ver, _ := build["VERSION.RELEASE"].(string)
	buildId, _ := build["Build.ID"].(string)
	incremental, _ := build["VERSION.INCREMENTAL"].(string)
	if brand != "" && model != "" && device != "" {
		build["Build.FINGERPRINT"] = fmt.Sprintf(
			"%s/%s/%s:%s/%s/%s:user/release-keys",
			brand, model, device, ver, buildId, incremental,
		)
		build["Build.DISPLAY"] = fmt.Sprintf("%s_%s", model, incremental)
	}

	// 系统属性
	if sys, ok := di["system"].(map[string]interface{}); ok {
		if sp, ok := sys["systemProperties"].(map[string]interface{}); ok {
			if params.Model != "" {
				sp["ro.product.model"] = params.Model
			}
			if params.Brand != "" {
				sp["ro.product.brand"] = params.Brand
			}
			if params.Manufacturer != "" {
				sp["ro.product.manufacturer"] = params.Manufacturer
			}
			if params.AndroidVersion != "" {
				sp["ro.build.version.sdk"] = build["VERSION.SDK"]
			}
			// Build 补全同步到 systemProperties
			if params.BuildDevice != "" || params.BuildID != "" || params.BuildIncremental != "" {
				sp["ro.build.description"] = fmt.Sprintf("%s/%s/%s:%s/%s/%s:user/release-keys",
					brand, model, device, ver, buildId, incremental)
				sp["ro.build.version.incremental"] = incremental
			}
			if params.BuildUser != "" {
				sp["ro.build.user"] = params.BuildUser
			}
		}

		// SystemConfig：settings.secure.android_id 同步
		if len(params.SystemConfig) > 0 {
			if sc, ok := sys["settings.secure"].(map[string]interface{}); ok {
				if aid, ok := params.SystemConfig["androidId"]; ok && aid != "" {
					sc["android_id"] = aid
				}
			}
			if cc, ok := params.SystemConfig["countryCode"]; ok && cc != "" {
				if sg, ok := sys["settings.global"].(map[string]interface{}); ok {
					sg["wifi_country_code"] = cc
				}
			}
			if im, ok := params.SystemConfig["inputMethod"]; ok && im != "" {
				if sc, ok := sys["settings.secure"].(map[string]interface{}); ok {
					sc["default_input_method"] = im
				}
			}
		}

		// LocaleConfig：地区/本地化设置
		if len(params.LocaleConfig) > 0 {
			if tf, ok := params.LocaleConfig["timeFormat"]; ok && tf != "" {
				if ss, ok := sys["settings.system"].(map[string]interface{}); ok {
					ss["time_12_24"] = tf
				}
			}
			if df, ok := params.LocaleConfig["dateFormat"]; ok {
				if ss, ok := sys["settings.system"].(map[string]interface{}); ok {
					ss["date_format"] = df
				}
			}
			if atz, ok := params.LocaleConfig["autoTimeZone"]; ok && atz != "" {
				if sg, ok := sys["settings.global"].(map[string]interface{}); ok {
					sg["auto_time_zone"] = atz
				}
			}
			if pnm, ok := params.LocaleConfig["preferredNetworkMode"]; ok && pnm != "" {
				if sg, ok := sys["settings.global"].(map[string]interface{}); ok {
					sg["preferred_network_mode"] = pnm
				}
			}
			if wua, ok := params.LocaleConfig["webviewUa"]; ok && wua != "" {
				sys["system.webview.ua"] = wua
			}
		}

		// http.agent 同步更新
		if params.Model != "" || params.AndroidVersion != "" || params.BuildID != "" || params.BuildIncremental != "" {
			ua := fmt.Sprintf("Dalvik/2.1.0 (Linux; U; Android %s; %s Build/%s)",
				ver, model, buildId)
			if sp, ok := sys["systemProperties"].(map[string]interface{}); ok {
				sp["http.agent"] = ua
			}
			if sp, ok := sys["System.getProperties"].(map[string]interface{}); ok {
				sp["http.agent"] = ua
			}
		}
	}

	// PmConfig：包管理器配置
	if len(params.PmConfig) > 0 {
		if pm, ok := di["pm"].(map[string]interface{}); ok {
			if mp, ok := params.PmConfig["marketPkg"]; ok && mp != "" {
				pm["marketPkg"] = mp
			}
			if lp, ok := params.PmConfig["launcherPkg"]; ok && lp != "" {
				pm["launcherPkg"] = lp
			}
		}
	}

	// SimPresetConfig：SIM 国家预设
	if len(params.SimPresetConfig) > 0 {
		if sim, ok := di["sim"].(map[string]interface{}); ok {
			if ci, ok := params.SimPresetConfig["countryIso"]; ok && ci != "" {
				sim["countryIso"] = ci
				sim["getNetworkCountryIso"] = ci
			}
			if nco, ok := params.SimPresetConfig["networkCountryIso"]; ok && nco != "" {
				sim["getNetworkCountryIso"] = nco
			}
		}
	}
}

// rebuildDeviceId 重建 base.deviceId，格式为 {brand}_{model}_{device}_{androidXX}
// 只替换非空的段，其余保留原值
func rebuildDeviceId(did, brand, model, sdk string) string {
	parts := strings.Split(did, "_")
	if len(parts) < 4 {
		return did
	}
	if brand != "" {
		parts[0] = strings.ToLower(brand)
	}
	if model != "" {
		parts[1] = model
	}
	if sdk != "" {
		for i, p := range parts {
			if strings.HasPrefix(p, "android") {
				parts[i] = "android" + sdk
			}
		}
	}
	return strings.Join(parts, "_")
}

func applyDisplayEdits(disp map[string]interface{}, params *ModelEditParams) {
	if params.Width > 0 && params.Height > 0 {
		if dc, ok := disp["display_config"].(map[string]interface{}); ok {
			dc["data.display.width"] = params.Width
			dc["data.display.height"] = params.Height
		}
		if rm, ok := disp["getRealMetrics"].(map[string]interface{}); ok {
			rm["widthPixels"] = params.Width
			rm["heightPixels"] = params.Height
			rm["noncompatWidthPixels"] = params.Width
			rm["noncompatHeightPixels"] = params.Height
		}
		if gm, ok := disp["getMetrics"].(map[string]interface{}); ok {
			gm["widthPixels"] = params.Width
			gm["heightPixels"] = params.Height
			gm["noncompatWidthPixels"] = params.Width
			gm["noncompatHeightPixels"] = params.Height
		}
		if modes, ok := disp["displayModes"].([]interface{}); ok {
			for _, m := range modes {
				if mode, ok := m.(map[string]interface{}); ok {
					mode["physicalWidth"] = params.Width
					mode["physicalHeight"] = params.Height
					if psi, ok := mode["pointSizeInfo"].(map[string]interface{}); ok {
						psi["modeWidth"] = params.Width
						psi["modeHeight"] = params.Height
						psi["theoreticalPointX"] = params.Width
						psi["theoreticalPointY"] = params.Height
						psi["theoreticalRealPointX"] = params.Width
						psi["theoreticalRealPointY"] = params.Height
					}
				}
			}
		}
	}

	if params.DPI > 0 {
		density := float64(params.DPI) / 160.0
		if dc, ok := disp["display_config"].(map[string]interface{}); ok {
			dc["data.display.densityDpi"] = params.DPI
		}
		for _, key := range []string{"getRealMetrics", "getMetrics"} {
			if m, ok := disp[key].(map[string]interface{}); ok {
				m["densityDpi"] = params.DPI
				m["density"] = density
				m["noncompatDensityDpi"] = params.DPI
				m["noncompatDensity"] = density
				m["scaledDensity"] = density
				m["noncompatScaledDensity"] = density
			}
		}
	}

	if params.RefreshRate > 0 {
		if modes, ok := disp["displayModes"].([]interface{}); ok && len(modes) > 0 {
			if mode, ok := modes[0].(map[string]interface{}); ok {
				mode["refreshRate"] = float64(params.RefreshRate)
			}
		}
		if dc, ok := disp["display_config"].(map[string]interface{}); ok {
			dc["display.getRefreshRate"] = float64(params.RefreshRate)
		}
	}
}

// applyDgConfigEdits 将 simConfig/netConfig/kernelConfig 写入 device_info.json 的 dg_config
// 同时处理 drm/gpu 的顶层字段修改。di 为已 unmarshal 的 device_info.json 内容，
// 序列化写回由调用方（modifyInnerZip）负责。
func applyDgConfigEdits(di map[string]interface{}, params *ModelEditParams) {
	// 各模块的 dg_config 前缀 → params 字段映射
	type configMapping struct {
		jsonPrefix string
		configMap  map[string]string
	}
	mappings := []configMapping{
		{"sim", params.SimConfig},
		{"net", params.NetConfig},
		{"kernel", params.KernelConfig},
	}

	for _, m := range mappings {
		if len(m.configMap) == 0 {
			continue
		}
		module, ok := di[m.jsonPrefix].(map[string]interface{})
		if !ok {
			continue
		}
		dgConfig, ok := module["dg_config"].(map[string]interface{})
		if !ok {
			dgConfig = make(map[string]interface{})
		}
		for k, v := range m.configMap {
			key := m.jsonPrefix + "." + k
			if v == "" {
				delete(dgConfig, key)
			} else {
				dgConfig[key] = v
			}
		}
		module["dg_config"] = dgConfig
	}

	// DRM：直接修改顶层字段（无 dg_config）
	if len(params.DrmConfig) > 0 {
		if drm, ok := di["drm"].(map[string]interface{}); ok {
			for k, v := range params.DrmConfig {
				if v == "" {
					delete(drm, k)
				} else {
					drm[k] = v
				}
			}
		}
	}

	// GPU：直接修改顶层字段（无 dg_config）
	if len(params.GpuConfig) > 0 {
		if gpu, ok := di["gpu"].(map[string]interface{}); ok {
			for k, v := range params.GpuConfig {
				if v == "" {
					delete(gpu, k)
				} else {
					gpu[k] = v
				}
			}
		}
	}
}
