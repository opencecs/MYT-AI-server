import { tagStyle } from '../utils/tagColor'

interface Props {
  tags: string[]
  /** 最多展示几个标签，超出显示 +N（紧凑场景用） */
  max?: number
  /** chips 尺寸：xs 用于表格行/卡片底部，sm 用于编辑器 */
  size?: 'xs' | 'sm'
  className?: string
  /** 点击某个标签（如触发按标签筛选） */
  onTagClick?: (tag: string) => void
}

/**
 * 标签 chips 只读展示。颜色由标签名派生（utils/tagColor），无需存储色板。
 */
function SlotTagChips({ tags, max, size = 'xs', className = '', onTagClick }: Props) {
  if (!tags || tags.length === 0) return null
  const shown = max && tags.length > max ? tags.slice(0, max) : tags
  const rest = tags.length - shown.length
  const pad = size === 'xs' ? 'px-1.5 py-px text-[10px]' : 'px-2 py-0.5 text-xs'

  return (
    <div className={`flex items-center gap-1 flex-wrap min-w-0 ${className}`}>
      {shown.map(tag => {
        const style = tagStyle(tag)
        return (
          <span
            key={tag}
            title={tag}
            onClick={onTagClick ? (e) => { e.stopPropagation(); onTagClick(tag) } : undefined}
            className={`${pad} rounded border whitespace-nowrap truncate max-w-[80px] ${style.chip} ${onTagClick ? 'cursor-pointer hover:brightness-95' : ''}`}
          >
            {tag}
          </span>
        )
      })}
      {rest > 0 && <span className={`${pad} text-gray-400`}>+{rest}</span>}
    </div>
  )
}

export default SlotTagChips
