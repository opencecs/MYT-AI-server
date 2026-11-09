import * as SdkApiService from '../../../../../bindings/github.com/moyunteng/myt/sdkapiservicewrapper'
import type { MirrorItem } from '../../../../types/index'
import { isEmulatorMirrorType } from '../../../mirror/constants'

/** 设备 API 请求封装 */
export async function deviceFetch(deviceId: string, path: string) {
  const resp = await SdkApiService.DeviceFetch(deviceId, 'GET', path, '')
  const json = JSON.parse(resp || '{}')
  if (json.code !== undefined && json.code !== 0 && json.code !== 9001) throw new Error(json.message || `Device API error: code=${json.code}`)
  return json.data ?? json
}

export const inputCls = 'h-9 px-3 text-sm rounded-md border border-gray-200 bg-white text-gray-700 hover:border-gray-300 transition-colors focus:border-blue-400 focus:ring-1 focus:ring-blue-100 min-w-0'

/** 从 image 推导安卓版本号（数字字符串，如 "12"） */
export function getAndroidVerFromImage(image: string, mirrors: MirrorItem[]): string {
  const m = mirrors.find(m => m.url === image)
  if (m?.os_ver) {
    const v = m.os_ver.replace('and', '')
    if (v) return v
  }
  const idx = image.lastIndexOf(':')
  if (idx !== -1) {
    const tag = image.slice(idx + 1)
    const match = tag.match(/(\d+)/)
    if (match) return match[1]
  }
  return ''
}

/** 判断镜像是否为容器（非模拟器类型：含 _v3 且非特质版、sys_ver!=5） */
export function isContainerImage(imageUrl: string, mirrors: MirrorItem[]): boolean {
  if (!imageUrl) return false
  const m = mirrors.find(it => it.url === imageUrl)
  if (!m) return false
  return !isEmulatorMirrorType(m)
}
