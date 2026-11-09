import * as SdkApiService from '../../../../../bindings/github.com/moyunteng/myt/sdkapiservicewrapper'
import { formatBytes } from './CreateInstanceConstants'
import i18n from '../../../../i18n'

/**
 * 通用设备 REST API 请求（经 Go 代理，自动适配 local/public 模式）。
 * local 模式：deviceIP 为局域网 IP，直连设备。public 模式：deviceIP 为 device_id，走网关透传。
 * 调用方需用 getDeviceId(device, mode) 传入正确的标识符。
 * taskId 非空时走 DeviceFetchCancelable，支持 CancelTaskOp(taskId) 中断在途 HTTP。
 */
export async function deviceFetch(ip: string, path: string, init?: RequestInit, taskId?: string) {
  const method = init?.method || 'GET'
  const body = init?.body ? String(init.body) : ''
  const resp = taskId
    ? await SdkApiService.DeviceFetchCancelable(ip, method, path, body, taskId)
    : await SdkApiService.DeviceFetch(ip, method, path, body)
  const json = JSON.parse(resp || '{}')
  if (json.code !== undefined && json.code !== 0 && json.code !== 9001)
    throw new Error(json.message || json.msg || `Device API error: code=${json.code}`)
  return json.data ?? json
}

/**
 * 拉取在线镜像到设备。
 * local 模式：浏览器直连设备 SSE 获取详细进度。
 * public 模式：经 Go 代理拉取（无详细进度，仅完成/失败）。
 * 调用方需用 getDeviceId(device, mode) 传入正确的标识符。
 */
export async function pullImageToDevice(
  ip: string, imageUrl: string,
  onProgress: (pct: number, text: string) => void,
  signal?: AbortSignal,
  taskId?: string,
) {
  // public 模式：ip 实为 device_id，走 Go 代理（无 SSE 进度）
  if (signal?.aborted) return
  // 判断是否 public 模式：device_id 以字母开头（如 r99d3...），IP 以数字开头
  const isPublic = ip && /^[a-zA-Z]/.test(ip)
  if (isPublic) {
    onProgress(50, i18n.t('host:create.pullingImage'))
    if (taskId) await SdkApiService.PullImageCancelable(ip, imageUrl, taskId)
    else await SdkApiService.PullImage(ip, imageUrl)
    if (signal?.aborted) return
    onProgress(100, '')
    return
  }

  // local 模式：直连设备 SSE 获取详细进度
  const headers: Record<string, string> = { 'Content-Type': 'application/json' }
  try {
    const pwd = await SdkApiService.GetDevicePassword(ip)
    if (pwd) headers['Authorization'] = `Basic ${btoa(`admin:${pwd}`)}`
  } catch {}
  const res = await fetch(`http://${ip}:8000/android/pullImage`, {
    method: 'POST', headers, body: JSON.stringify({ imageUrl }), signal,
  })
  if (!res.ok) throw new Error(`Pull image failed: ${res.status} ${res.statusText}`)
  if (!res.body) { onProgress(100, ''); return }
  const reader = res.body.getReader()
  const decoder = new TextDecoder()
  let buffer = ''
  for (;;) {
    if (signal?.aborted) return
    const { done, value } = await reader.read()
    if (done) break
    buffer += decoder.decode(value, { stream: true })
    const lines = buffer.split('\n')
    buffer = lines.pop() || ''
    for (const line of lines) {
      const jsonStr = line.startsWith('data: ') ? line.slice(6) : line.trim()
      if (!jsonStr || jsonStr === '[DONE]') continue
      try {
        const obj = JSON.parse(jsonStr)
        if (obj.progressDetail?.current && obj.progressDetail?.total) {
          const pct = Math.round(obj.progressDetail.current / obj.progressDetail.total * 100)
          onProgress(pct, `${formatBytes(obj.progressDetail.current)} / ${formatBytes(obj.progressDetail.total)}`)
        }
      } catch {}
    }
  }
  onProgress(100, '')
}

// CIDR 子网转 IP 范围
export function cidrToRange(cidr: string): { start: string; end: string } | null {
  const [ip, bits] = cidr.split('/')
  if (!ip || !bits) return null
  const mask = (~0 << (32 - parseInt(bits))) >>> 0
  const ipNum = (ipToNumber(ip) >>> 0) & mask
  const broadcast = (ipNum | (~mask >>> 0)) >>> 0
  return { start: numberToIp(ipNum + 1), end: numberToIp(broadcast - 1) }
}

// IPv4 字符串转 32 位无符号整数
function ipToNumber(ip: string): number {
  const parts = ip.split('.').map(p => parseInt(p))
  if (parts.length !== 4 || parts.some(p => isNaN(p) || p < 0 || p > 255)) return 0
  return (((parts[0] << 24) | (parts[1] << 16) | (parts[2] << 8) | parts[3]) >>> 0)
}

// 32 位无符号整数转 IPv4 字符串
function numberToIp(n: number): string {
  return [(n >>> 24) & 0xff, (n >>> 16) & 0xff, (n >>> 8) & 0xff, n & 0xff].join('.')
}

// 批量创建时 macVlanIp 按末段递增（offset 从 0 开始，溢出进位前三段）
// 超出 255.255.255.255 边界时回退到原始 IP（避免无效地址）
export function incrementIp(ip: string, offset: number): string {
  const base = ipToNumber(ip)
  if (!base) return ip
  const next = base + offset
  if (next > 0xffffffff) return ip
  return numberToIp(next >>> 0)
}
