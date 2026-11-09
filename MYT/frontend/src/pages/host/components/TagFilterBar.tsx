import { useTranslation } from 'react-i18next'
import { Tag } from 'lucide-react'
import { tagStyle } from '../../../utils/tagColor'

interface Props {
  /** 全局已有标签（已排序） */
  allTags: string[]
  /** 当前选中的标签：多选为「或」语义（命中任一即显示） */
  selected: Set<string>
  /** 切换某个标签的选中态 */
  onToggle: (tag: string) => void
  /** 清空全部选中 */
  onClear: () => void
}

/**
 * 标签筛选栏：截图模式与列表模式共用（两处 UI 原本逐字符相同，故收敛于此）。
 *
 * 由调用方决定是否渲染（无标签时不占位），组件内不做空判断——
 * 截图模式还要叠加 hideTagFilter 开关，判断权留给宿主更直白。
 */
function TagFilterBar({ allTags, selected, onToggle, onClear }: Props) {
  const { t } = useTranslation()
  return (
    <div className="flex items-center gap-1.5 px-1 pb-3 flex-wrap">
      <span className="inline-flex items-center gap-1 text-[11px] text-gray-400 shrink-0">
        <Tag className="size-3" />
        {t('host:tags.filter')}
      </span>
      {allTags.map(tag => {
        const active = selected.has(tag)
        const style = tagStyle(tag)
        return (
          <button
            key={tag}
            onClick={() => onToggle(tag)}
            className={`px-1.5 py-0.5 text-[11px] rounded border whitespace-nowrap cursor-pointer transition-colors ${
              active ? `${style.chip} font-medium` : 'bg-white text-gray-400 border-gray-200 hover:border-gray-300'
            }`}
          >
            {tag}
          </button>
        )
      })}
      {selected.size > 0 && (
        <button
          onClick={onClear}
          className="text-[11px] text-gray-400 hover:text-blue-500 cursor-pointer shrink-0"
        >
          {t('host:tags.filterClear')}
        </button>
      )}
    </div>
  )
}

export default TagFilterBar
