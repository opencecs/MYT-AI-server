const DEVICE_PORT = 8000
const DEVICE_USER = 'admin'

function baseUrl(ip: string) {
  return `http://${ip}:${DEVICE_PORT}`
}

function authHeader(): Record<string, string> {
  return { Authorization: 'Basic ' + btoa(DEVICE_USER + ':') }
}

function isSuccessCode(code: unknown): boolean {
  return code === 0 || code === 200 || code === '0' || code === '200'
}

async function request(ip: string, method: string, path: string, body?: unknown, timeoutMs?: number): Promise<unknown> {
  const url = baseUrl(ip) + path
  const controller = timeoutMs ? new AbortController() : undefined
  const timer = controller ? setTimeout(() => controller.abort(), timeoutMs) : undefined

  const opts: RequestInit = {
    method,
    headers: { ...authHeader(), 'Content-Type': 'application/json' },
    signal: controller?.signal,
  }
  if (body !== undefined) opts.body = JSON.stringify(body)
  try {
    const resp = await fetch(url, opts)
    if (timer) clearTimeout(timer)
    const json = await resp.json()
    if (!isSuccessCode(json.code)) throw new Error(json.msg || '请求失败')
    return json.data
  } catch (e) {
    if (timer) clearTimeout(timer)
    throw e
  }
}

export function extractList(data: unknown): unknown[] {
  if (data && typeof data === 'object' && 'list' in data) return (data as { list: unknown[] }).list
  if (Array.isArray(data)) return data
  return []
}

export function extractObj(data: unknown): unknown {
  if (data && typeof data === 'object' && 'info' in data && !('list' in data)) return (data as { info: unknown }).info
  return data
}

export const api = {
  get: (ip: string, path: string, timeoutMs?: number) => request(ip, 'GET', path, undefined, timeoutMs),
  post: (ip: string, path: string, body?: unknown) => request(ip, 'POST', path, body),
  put: (ip: string, path: string, body?: unknown) => request(ip, 'PUT', path, body),
  del: (ip: string, path: string) => request(ip, 'DELETE', path),
}
