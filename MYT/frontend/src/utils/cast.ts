// Wails 绑定返回类型为 any，需要安全强转。
// 统一通过此函数处理，避免散落各处的 as unknown 断言。
export function cast<T>(value: unknown): T {
  return value as T
}
