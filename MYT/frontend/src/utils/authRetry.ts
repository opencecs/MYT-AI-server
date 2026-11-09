import * as SdkApiService from '../../bindings/github.com/moyunteng/myt/sdkapiservicewrapper'
import AuthDialog from '../components/AuthDialog'

// 判断错误是否为认证失败
export function isAuthError(err: unknown): boolean {
  const msg = err instanceof Error ? err.message : String(err)
  return msg.includes('code=61') || msg.includes('Authentication Failed') || msg.includes('Authentication Failed')
}

// 获取错误中的设备IP（从 "POST /xxx 失败: ..." 类错误中提取）
function extractDeviceIP(err: unknown): string {
  const msg = err instanceof Error ? err.message : String(err)
  // Go端错误格式: "POST http://192.168.x.x:8000/xxx 失败: ..."
  const match = msg.match(/http:\/\/(\d+\.\d+\.\d+\.\d+)/)
  return match ? match[1] : ''
}

// 认证重试：捕获认证错误 → 弹认证弹窗 → 保存密码 → 重试
// 用法: authRetry(deviceIP, () => SdkApiService.SomeApiCall(...))
export async function authRetry<T>(
  deviceIP: string,
  fn: () => Promise<T>,
  showAuthDialogFn?: (ip: string) => Promise<string>,
): Promise<T> {
  try {
    return await fn()
  } catch (err) {
    if (!isAuthError(err)) throw err

    // 需要认证
    const ip = extractDeviceIP(err) || deviceIP
    const password = await (showAuthDialogFn || defaultShowAuthDialog)(ip)
    if (!password) throw err // 用户取消

    // 保存密码
    try {
      await SdkApiService.UpdateDevicePasswordForIP(ip, password)
    } catch {}

    // 重试
    return await fn()
  }
}

// 默认的认证弹窗实现（用 React render 的方式）
let pendingAuthDialogs: { ip: string; resolve: (pwd: string) => void }[] = []
let authDialogRenderer: ((dialogs: { ip: string; resolve: (pwd: string) => void }[]) => void) | null = null

export function setAuthDialogRenderer(renderer: (dialogs: { ip: string; resolve: (pwd: string) => void }[]) => void) {
  authDialogRenderer = renderer
}

function defaultShowAuthDialog(ip: string): Promise<string> {
  return new Promise((resolve) => {
    pendingAuthDialogs.push({ ip, resolve })
    // 200ms 内收集所有需要认证的设备，避免逐个弹窗
    setTimeout(() => {
      if (authDialogRenderer && pendingAuthDialogs.length > 0) {
        authDialogRenderer([...pendingAuthDialogs])
        pendingAuthDialogs = []
      }
    }, 200)
  })
}

export { AuthDialog }
