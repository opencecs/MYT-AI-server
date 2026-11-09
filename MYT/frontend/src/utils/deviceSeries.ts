/**
 * 设备型号 → 系列字母（设备列表左侧徽标用，完整型号移到 title 悬浮）。
 *
 * 依据现有型号取值（Go 侧 normalizeDeviceType 去掉 _vN 后缀后的值）：p1 / q1 / r1s / r1q / c1。
 * 与镜像页的 QRC/P1 二分法不同：这里 c1 单列一档 C（用户指定），故显式列举而非套用 isPSeries。
 */
export function deviceSeriesLetter(deviceType?: string): string {
  const t = (deviceType || '').trim().toLowerCase()
  if (!t) return '?'
  if (t.startsWith('p1')) return 'P'
  if (t.startsWith('c1')) return 'C'
  if (t.startsWith('q1') || t.startsWith('r1q') || t.startsWith('r1s')) return 'Q'
  return t[0].toUpperCase()
}
