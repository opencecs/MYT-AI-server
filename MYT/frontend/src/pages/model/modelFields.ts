export interface FieldDef { key: string; label: string; placeholder?: string }

export const OVERLAY_LABELS: Record<string, string> = {
  EMMCID: 'fields.overlay.EMMCID', EMMCSD: 'fields.overlay.EMMCSD', GFID: 'fields.overlay.GFID', MEDIAID: 'fields.overlay.MEDIAID',
  RNDID: 'fields.overlay.RNDID', SERIALNUMBER: 'fields.overlay.SERIALNUMBER', aaid: 'fields.overlay.aaid', android_id: 'fields.overlay.android_id',
  bluetooth_mac: 'fields.overlay.bluetooth_mac', bluetooth_name: 'fields.overlay.bluetooth_name', bootid: 'fields.overlay.bootid',
  cellular_mac: 'fields.overlay.cellular_mac', gaid: 'fields.overlay.gaid', gnssModel: 'fields.overlay.gnssModel', oaid: 'fields.overlay.oaid',
  ovLibc: 'fields.overlay.ovLibc', serial_number: 'fields.overlay.serial_number', udid: 'fields.overlay.udid', vaid: 'fields.overlay.vaid',
  wifi_list: 'fields.overlay.wifi_list', wifi_mac: 'fields.overlay.wifi_mac', wifi_name: 'fields.overlay.wifi_name', region: 'fields.overlay.region',
}

export const SIM_FIELDS: FieldDef[] = [
  { key: 'imei', label: 'fields.sim.imei', placeholder: 'fields.sim.imeiPlaceholder' },
  { key: 'iccid', label: 'fields.sim.iccid', placeholder: 'fields.sim.iccidPlaceholder' },
  { key: 'imsi', label: 'fields.sim.imsi', placeholder: 'fields.sim.imsiPlaceholder' },
  { key: 'msisdn', label: 'fields.sim.msisdn', placeholder: 'fields.sim.msisdnPlaceholder' },
  { key: 'meid', label: 'fields.sim.meid', placeholder: 'fields.sim.meidPlaceholder' },
  { key: 'esn', label: 'fields.sim.esn', placeholder: 'fields.sim.esnPlaceholder' },
  { key: 'nai', label: 'fields.sim.nai', placeholder: 'fields.sim.naiPlaceholder' },
  { key: 'spn', label: 'fields.sim.spn', placeholder: 'fields.sim.spnPlaceholder' },
  { key: 'operatorLongName', label: 'fields.sim.operatorLongName', placeholder: 'fields.sim.operatorLongNamePlaceholder' },
  { key: 'operatorShortName', label: 'fields.sim.operatorShortName', placeholder: 'fields.sim.operatorShortNamePlaceholder' },
  { key: 'numeric', label: 'fields.sim.numeric', placeholder: 'fields.sim.numericPlaceholder' },
  { key: 'alphaTag', label: 'fields.sim.alphaTag', placeholder: 'fields.sim.alphaTagPlaceholder' },
  { key: 'sid', label: 'fields.sim.sid', placeholder: 'fields.sim.sidPlaceholder' },
  { key: 'lac', label: 'fields.sim.lac', placeholder: 'fields.sim.lacPlaceholder' },
  { key: 'cid', label: 'fields.sim.cid', placeholder: 'fields.sim.cidPlaceholder' },
  { key: 'pci', label: 'fields.sim.pci', placeholder: 'fields.sim.pciPlaceholder' },
  { key: 'imeisv', label: 'fields.sim.imeisv', placeholder: 'fields.sim.imeisvPlaceholder' },
  { key: 'state', label: 'fields.sim.state', placeholder: 'fields.sim.statePlaceholder' },
  { key: 'netType', label: 'fields.sim.netType', placeholder: 'fields.sim.netTypePlaceholder' },
  { key: 'radioType', label: 'fields.sim.radioType', placeholder: 'fields.sim.radioTypePlaceholder' },
  { key: 'rilImplVersion', label: 'fields.sim.rilImplVersion', placeholder: 'fields.sim.rilImplVersionPlaceholder' },
  { key: 'baseband', label: 'fields.sim.baseband', placeholder: 'fields.sim.basebandPlaceholder' },
]

export const NET_FIELDS: FieldDef[] = [
  { key: 'wifi.ssid', label: 'fields.net.wifiSsid', placeholder: 'fields.net.wifiSsidPlaceholder' },
  { key: 'wifi.bssid', label: 'fields.net.wifiBssid', placeholder: 'fields.net.wifiBssidPlaceholder' },
  { key: 'wifi.frequency', label: 'fields.net.wifiFrequency', placeholder: 'fields.net.wifiFrequencyPlaceholder' },
  { key: 'wifi.ipaddress', label: 'fields.net.wifiIpaddress', placeholder: 'fields.net.wifiIpaddressPlaceholder' },
  { key: 'wifi.linkspeed', label: 'fields.net.wifiLinkspeed', placeholder: 'fields.net.wifiLinkspeedPlaceholder' },
  { key: 'wifi.rssi', label: 'fields.net.wifiRssi', placeholder: 'fields.net.wifiRssiPlaceholder' },
  { key: 'wifi.dhcp.ip', label: 'fields.net.wifiDhcpIp', placeholder: 'fields.net.wifiDhcpIpPlaceholder' },
  { key: 'wifi.dhcp.gateway', label: 'fields.net.wifiDhcpGateway', placeholder: 'fields.net.wifiDhcpGatewayPlaceholder' },
  { key: 'wifi.dhcp.mask', label: 'fields.net.wifiDhcpMask', placeholder: 'fields.net.wifiDhcpMaskPlaceholder' },
  { key: 'wifi.dhcp.dns1', label: 'fields.net.wifiDhcpDns1', placeholder: 'fields.net.wifiDhcpDns1Placeholder' },
  { key: 'wifi.dhcp.dns2', label: 'fields.net.wifiDhcpDns2', placeholder: 'fields.net.wifiDhcpDns2Placeholder' },
  { key: 'wifi.dhcp.server', label: 'fields.net.wifiDhcpServer', placeholder: 'fields.net.wifiDhcpServerPlaceholder' },
  { key: 'wifi.dhcp.leasetime', label: 'fields.net.wifiDhcpLeasetime', placeholder: 'fields.net.wifiDhcpLeasetimePlaceholder' },
  { key: 'wifi.capabilities', label: 'fields.net.wifiCapabilities', placeholder: 'fields.net.wifiCapabilitiesPlaceholder' },
  { key: 'wifi.networkid', label: 'fields.net.wifiNetworkid', placeholder: 'fields.net.wifiNetworkidPlaceholder' },
]

export const KERNEL_FIELDS: FieldDef[] = [
  { key: 'uname.sysname', label: 'fields.kernel.unameSysname', placeholder: 'fields.kernel.unameSysnamePlaceholder' },
  { key: 'uname.machine', label: 'fields.kernel.unameMachine', placeholder: 'fields.kernel.unameMachinePlaceholder' },
  { key: 'uname.release', label: 'fields.kernel.unameRelease', placeholder: 'fields.kernel.unameReleasePlaceholder' },
  { key: 'uname.nodename', label: 'fields.kernel.unameNodename', placeholder: 'fields.kernel.unameNodenamePlaceholder' },
  { key: 'uname.version', label: 'fields.kernel.unameVersion', placeholder: 'fields.kernel.unameVersionPlaceholder' },
  { key: 'mem.total', label: 'fields.kernel.memTotal', placeholder: 'fields.kernel.memTotalPlaceholder' },
  { key: 'mem.used', label: 'fields.kernel.memUsed', placeholder: 'fields.kernel.memUsedPlaceholder' },
  { key: 'disk.data.total', label: 'fields.kernel.diskDataTotal', placeholder: 'fields.kernel.diskDataTotalPlaceholder' },
  { key: 'disk.data.used', label: 'fields.kernel.diskDataUsed', placeholder: 'fields.kernel.diskDataUsedPlaceholder' },
]

export const DRM_FIELDS: FieldDef[] = [
  { key: 'systemId', label: 'fields.drm.systemId', placeholder: 'fields.drm.systemIdPlaceholder' },
  { key: 'deviceUniqueId', label: 'fields.drm.deviceUniqueId', placeholder: 'fields.drm.deviceUniqueIdPlaceholder' },
  { key: 'securityLevel', label: 'fields.drm.securityLevel', placeholder: 'fields.drm.securityLevelPlaceholder' },
  { key: 'version', label: 'fields.drm.version', placeholder: 'fields.drm.versionPlaceholder' },
  { key: 'vendor', label: 'fields.drm.vendor', placeholder: 'fields.drm.vendorPlaceholder' },
]

export const GPU_FIELDS: FieldDef[] = [
  { key: 'renderer', label: 'fields.gpu.renderer', placeholder: 'fields.gpu.rendererPlaceholder' },
  { key: 'vendor', label: 'fields.gpu.vendor', placeholder: 'fields.gpu.vendorPlaceholder' },
  { key: 'version', label: 'fields.gpu.version', placeholder: 'fields.gpu.versionPlaceholder' },
]

export const SYSTEM_FIELDS: FieldDef[] = [
  { key: 'androidId', label: 'fields.system.androidId', placeholder: 'fields.system.androidIdPlaceholder' },
  { key: 'countryCode', label: 'fields.system.countryCode', placeholder: 'fields.system.countryCodePlaceholder' },
  { key: 'inputMethod', label: 'fields.system.inputMethod', placeholder: 'fields.system.inputMethodPlaceholder' },
]

export const BUILD_FIELDS: FieldDef[] = [
  { key: 'buildDevice', label: 'fields.build.buildDevice', placeholder: 'fields.build.buildDevicePlaceholder' },
  { key: 'buildId', label: 'fields.build.buildId', placeholder: 'fields.build.buildIdPlaceholder' },
  { key: 'buildIncremental', label: 'fields.build.buildIncremental', placeholder: 'fields.build.buildIncrementalPlaceholder' },
  { key: 'buildBoard', label: 'fields.build.buildBoard', placeholder: 'fields.build.buildBoardPlaceholder' },
  { key: 'buildHardware', label: 'fields.build.buildHardware', placeholder: 'fields.build.buildHardwarePlaceholder' },
  { key: 'buildHost', label: 'fields.build.buildHost', placeholder: 'fields.build.buildHostPlaceholder' },
  { key: 'buildTime', label: 'fields.build.buildTime', placeholder: 'fields.build.buildTimePlaceholder' },
  { key: 'buildUser', label: 'fields.build.buildUser', placeholder: 'fields.build.buildUserPlaceholder' },
  { key: 'buildBootloader', label: 'fields.build.buildBootloader', placeholder: 'fields.build.buildBootloaderPlaceholder' },
]

export const PM_FIELDS: FieldDef[] = [
  { key: 'marketPkg', label: 'fields.pm.marketPkg', placeholder: 'fields.pm.marketPkgPlaceholder' },
  { key: 'launcherPkg', label: 'fields.pm.launcherPkg', placeholder: 'fields.pm.launcherPkgPlaceholder' },
]

export const SIM_PRESET_FIELDS: FieldDef[] = [
  { key: 'countryIso', label: 'fields.simPreset.countryIso', placeholder: 'fields.simPreset.countryIsoPlaceholder' },
  { key: 'networkCountryIso', label: 'fields.simPreset.networkCountryIso', placeholder: 'fields.simPreset.networkCountryIsoPlaceholder' },
]

export const LOCALE_FIELDS: FieldDef[] = [
  { key: 'timeFormat', label: 'fields.locale.timeFormat', placeholder: 'fields.locale.timeFormatPlaceholder' },
  { key: 'dateFormat', label: 'fields.locale.dateFormat', placeholder: 'fields.locale.dateFormatPlaceholder' },
  { key: 'autoTimeZone', label: 'fields.locale.autoTimeZone', placeholder: 'fields.locale.autoTimeZonePlaceholder' },
  { key: 'preferredNetworkMode', label: 'fields.locale.preferredNetworkMode', placeholder: 'fields.locale.preferredNetworkModePlaceholder' },
  { key: 'webviewUa', label: 'fields.locale.webviewUa', placeholder: 'fields.locale.webviewUaPlaceholder' },
]

export const QUICK_PROMPTS: { label: string; prompt: string }[] = [
  { label: 'quickPrompts.imei35', prompt: 'quickPrompts.imei35Prompt' },
  { label: 'quickPrompts.operatorChinaMobile', prompt: 'quickPrompts.operatorChinaMobilePrompt' },
  { label: 'quickPrompts.dns8888', prompt: 'quickPrompts.dns8888Prompt' },
  { label: 'quickPrompts.drmRandomize', prompt: 'quickPrompts.drmRandomizePrompt' },
  { label: 'quickPrompts.gpuMatch', prompt: 'quickPrompts.gpuMatchPrompt' },
  { label: 'quickPrompts.usTmobile', prompt: 'quickPrompts.usTmobilePrompt' },
  { label: 'quickPrompts.fullEnhance', prompt: 'quickPrompts.fullEnhancePrompt' },
]

export const PARAM_LABELS: Record<string, string> = (() => {
  const m: Record<string, string> = {
    model: 'fields.basic.model', brand: 'fields.basic.brand', manufacturer: 'fields.basic.manufacturer',
    androidVersion: 'fields.basic.androidVersion', securityPatch: 'fields.basic.securityPatch',
    width: 'fields.basic.width', height: 'fields.basic.height', dpi: 'fields.basic.dpi', refreshRate: 'fields.basic.refreshRate',
    randomizeIDs: 'fields.basic.randomizeIDs', supportedVersion: 'fields.basic.supportedVersion',
  }
  for (const [k, v] of Object.entries(OVERLAY_LABELS)) m[`overlay.${k}`] = v
  for (const f of SIM_FIELDS) m[`simConfig.${f.key}`] = f.label
  for (const f of NET_FIELDS) m[`netConfig.${f.key}`] = f.label
  for (const f of KERNEL_FIELDS) m[`kernelConfig.${f.key}`] = f.label
  for (const f of DRM_FIELDS) m[`drmConfig.${f.key}`] = f.label
  for (const f of GPU_FIELDS) m[`gpuConfig.${f.key}`] = f.label
  for (const f of SYSTEM_FIELDS) m[`systemConfig.${f.key}`] = f.label
  for (const f of BUILD_FIELDS) m[f.key] = f.label
  for (const f of PM_FIELDS) m[`pmConfig.${f.key}`] = f.label
  for (const f of SIM_PRESET_FIELDS) m[`simPresetConfig.${f.key}`] = f.label
  for (const f of LOCALE_FIELDS) m[`localeConfig.${f.key}`] = f.label
  return m
})()
