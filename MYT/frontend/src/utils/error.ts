// Wails 后端 Go 方法返回 error 时，前端收到 RuntimeError
// err.message 是 JSON 字符串，如: '{"message":"密码错误","cause":{},"kind":"RuntimeError"}'
// String(err) 会输出 'Error: {"message":...}'，对用户不友好
// 此函数提取最内层的 message 文本

export function getErrorMessage(err: unknown): string {
  if (err == null) return ''
  if (typeof err === 'string') return err

  const raw = err instanceof Error ? err.message : (typeof err === 'object' && err !== null && 'message' in err) ? String((err as { message: unknown }).message) : String(err)

  // 尝试从 JSON 中提取内层 message
  try {
    const obj = JSON.parse(raw)
    if (obj && typeof obj.message === 'string' && obj.message) return obj.message
  } catch {
    // raw 不是 JSON，直接返回
  }
  return raw
}
