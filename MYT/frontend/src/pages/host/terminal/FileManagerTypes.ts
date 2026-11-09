// 跨栏复制/粘贴的统一条目载荷（FileManagerModal + useFileManagerTransfer 共用）
export interface ClipboardItem {
  name: string
  path: string
  isDir: boolean
}

/** 双栏标识（云机/本地） */
export type Side = 'cloud' | 'local'

// 目录树节点
export interface DirectoryNode {
  name: string
  path: string
  isDir: boolean
  size: number
  modTime: number
  children: DirectoryNode[]
}

// 共享目录路径信息
export interface SharedDirPathInfo {
  path: string
  isDefault: boolean
  defaultPath: string
}
