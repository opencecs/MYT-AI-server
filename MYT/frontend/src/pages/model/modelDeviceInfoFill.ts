// modelDeviceInfoFill.ts — 从 device_info.json 反向回填编辑弹窗各分区。
// 只回填非空字符串值：state 缺 key = 提交时不触碰 = 不改原值。
// 反向映射对照 phone_model_editor_edit.go 的实际写路径。
import { SIM_FIELDS, NET_FIELDS, KERNEL_FIELDS, DRM_FIELDS, GPU_FIELDS, PM_FIELDS } from './modelFields'

type StringMap = Record<string, string>
type Json = Record<string, unknown>

function asMap(v: unknown): Json | null {
  return v && typeof v === 'object' && !Array.isArray(v) ? (v as Json) : null
}

function asStr(v: unknown): string {
  return typeof v === 'string' ? v : (v === null || v === undefined ? '' : String(v))
}

// pickDgConfig 从模块的 dg_config 里按 "sim.imei" 前缀 key 提取分区字段
function pickDgConfig(di: Json, module: string, fields: { key: string }[]): StringMap {
  const out: StringMap = {}
  const mod = asMap(di[module])
  const dg = mod ? asMap(mod['dg_config']) : null
  if (!dg) return out
  const prefix = module + '.'
  for (const f of fields) {
    const v = dg[prefix + f.key]
    const s = asStr(v)
    if (s !== '') out[f.key] = s
  }
  return out
}

// pickTopFields 从顶层对象直取字段（drm/gpu/pm）
function pickTopFields(di: Json, module: string, fields: { key: string }[]): StringMap {
  const out: StringMap = {}
  const mod = asMap(di[module])
  if (!mod) return out
  for (const f of fields) {
    const s = asStr(mod[f.key])
    if (s !== '') out[f.key] = s
  }
  return out
}

// simPreset ← sim.countryIso / sim.getNetworkCountryIso
function pickSimPreset(di: Json): StringMap {
  const out: StringMap = {}
  const sim = asMap(di['sim'])
  if (!sim) return out
  const ci = asStr(sim['countryIso'])
  if (ci) out['countryIso'] = ci
  const nci = asStr(sim['getNetworkCountryIso'])
  if (nci) out['networkCountryIso'] = nci
  return out
}

// system ← settings.secure["android_id"] / settings.global["wifi_country_code"]
//          / settings.secure["default_input_method"]（对照 applyDeviceEdits 写路径）
function pickSystem(di: Json): StringMap {
  const out: StringMap = {}
  const sys = asMap(di['system'])
  if (!sys) return out
  const secure = asMap(sys['settings.secure'])
  const global = asMap(sys['settings.global'])
  const aid = secure ? asStr(secure['android_id']) : ''
  if (aid) out['androidId'] = aid
  const cc = global ? asStr(global['wifi_country_code']) : ''
  if (cc) out['countryCode'] = cc
  const im = secure ? asStr(secure['default_input_method']) : ''
  if (im) out['inputMethod'] = im
  return out
}

// locale ← settings.system["time_12_24"/"date_format"] / settings.global
//          ["auto_time_zone"/"preferred_network_mode"] / system["system.webview.ua"]
function pickLocale(di: Json): StringMap {
  const out: StringMap = {}
  const sys = asMap(di['system'])
  if (!sys) return out
  const system = asMap(sys['settings.system'])
  const global = asMap(sys['settings.global'])
  const tf = system ? asStr(system['time_12_24']) : ''
  if (tf) out['timeFormat'] = tf
  const df = system ? asStr(system['date_format']) : ''
  if (df) out['dateFormat'] = df
  const atz = global ? asStr(global['auto_time_zone']) : ''
  if (atz) out['autoTimeZone'] = atz
  const pnm = global ? asStr(global['preferred_network_mode']) : ''
  if (pnm) out['preferredNetworkMode'] = pnm
  const wua = asStr(sys['system.webview.ua'])
  if (wua) out['webviewUa'] = wua
  return out
}

// pm ← di.pm.marketPkg / launcherPkg
function pickPm(di: Json): StringMap {
  return pickTopFields(di, 'pm', PM_FIELDS)
}

export interface DeviceInfoFills {
  simConfig: StringMap
  netConfig: StringMap
  kernelConfig: StringMap
  drmConfig: StringMap
  gpuConfig: StringMap
  systemConfig: StringMap
  pmConfig: StringMap
  simPresetConfig: StringMap
  localeConfig: StringMap
}

// parseDeviceInfoFills 解析 device_info.json 文本 → 各分区回填值。
// 解析失败返回 null（调用方 catch 后静默跳过，不阻塞弹窗）。
export function parseDeviceInfoFills(raw: string): DeviceInfoFills | null {
  let di: Json
  try {
    di = JSON.parse(raw)
  } catch {
    return null
  }
  if (!di || typeof di !== 'object') return null
  return {
    simConfig: pickDgConfig(di, 'sim', SIM_FIELDS),
    netConfig: pickDgConfig(di, 'net', NET_FIELDS),
    kernelConfig: pickDgConfig(di, 'kernel', KERNEL_FIELDS),
    drmConfig: pickTopFields(di, 'drm', DRM_FIELDS),
    gpuConfig: pickTopFields(di, 'gpu', GPU_FIELDS),
    systemConfig: pickSystem(di),
    pmConfig: pickPm(di),
    simPresetConfig: pickSimPreset(di),
    localeConfig: pickLocale(di),
  }
}
