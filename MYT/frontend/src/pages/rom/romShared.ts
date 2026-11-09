import type { V4RomVariant } from '../../../bindings/github.com/moyunteng/myt/models'

/** 三件套文件名（服务端唯一合法集合，见 internal/v4host/image.go 的 ImageFileNames） */
export const IMAGE_FILE_NAMES = ['system.img', 'vendor.img', 'initramfs.img'] as const

/** ROM 列表行（页面视图模型：在 Go 返回结构上补「可下载/可删除」判定） */
export interface RomVariantRow extends V4RomVariant {
  /** 缺失或未就绪的文件名（空 = 三件套齐全） */
  missing: string[]
}

/** 字节数格式化（列表里显示文件大小） */
export function formatBytes(bytes: number): string {
  if (!bytes || bytes <= 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let v = bytes
  let i = 0
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024
    i++
  }
  return `${v >= 100 || i === 0 ? Math.round(v) : v.toFixed(1)} ${units[i]}`
}

/** 变体名合法性（服务端只允许字母/数字/下划线/中划线） */
export function isValidVariantName(name: string): boolean {
  return /^[A-Za-z0-9_-]+$/.test(name)
}
