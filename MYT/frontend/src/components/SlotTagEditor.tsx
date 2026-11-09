import { useEffect, useRef, useState } from 'react'
import { gsap } from 'gsap'
import { useGSAP } from '@gsap/react'
import { useTranslation } from 'react-i18next'
import { X } from 'lucide-react'
import { tagStyle } from '../utils/tagColor'

gsap.registerPlugin(useGSAP)

interface Props {
  x: number
  y: number
  /** 当前容器的标签 */
  tags: string[]
  /** 全局已有标签（来自数据聚合，用于快速复用） */
  allTags: string[]
  /** 保存（传空数组 = 清除）；异步写设备端 /android/label，调用方负责乐观更新 + 权威收敛 */
  onSave: (tags: string[]) => Promise<void> | void
  onClose: () => void
}

/**
 * 容器标签编辑浮层：输入新标签 / 点选已有标签 / 单个删除 / 全部清除。
 *
 * 即时保存：每次变更直接写设备端（POST /android/label）并乐观更新本地条目，
 * 设备端 WS/查询推送后的权威 tags 收敛 UI。不做「草稿 + 确认」——标签是高频轻量
 * 操作，多一次确认反而碍事。保存期间浮层禁止重复提交（busy 状态）。
 */
function SlotTagEditor({ x, y, tags, allTags, onSave, onClose }: Props) {
  const { t } = useTranslation()
  const ref = useRef<HTMLDivElement>(null)
  const inputRef = useRef<HTMLInputElement>(null)
  const [pos, setPos] = useState({ x, y })
  const [input, setInput] = useState('')
  const [busy, setBusy] = useState(false)
  useEffect(() => {
    const el = ref.current
    if (!el) return
    const margin = 8
    const nx = Math.max(margin, Math.min(x, window.innerWidth - el.offsetWidth - margin))
    let ny = y
    if (y + el.offsetHeight + margin > window.innerHeight) ny = window.innerHeight - el.offsetHeight - margin
    setPos({ x: nx, y: Math.max(margin, ny) })
  }, [x, y, allTags.length])

  useGSAP(() => {
    if (!ref.current) return
    gsap.fromTo(ref.current, { scale: 0.94, autoAlpha: 0 }, {
      scale: 1, autoAlpha: 1, duration: 0.16, ease: 'power3.out',
      // autoAlpha 从 0 起会同步设 visibility:hidden，把 React autoFocus 刚聚焦的
      // input 强制 blur（焦点丢失后键盘事件到不了输入框）。动画结束后补回焦点。
      onComplete: () => inputRef.current?.focus(),
    })
  }, [])

  const commit = async (next: string[]) => {
    if (busy) return
    // 与设备端 normalizeTags 同规则：去空白 + 去重（本地先收敛，避免闪烁）
    const cleaned: string[] = []
    for (const raw of next) {
      const name = raw.trim()
      if (name && !cleaned.includes(name)) cleaned.push(name)
    }
    setBusy(true)
    try {
      await onSave(cleaned)
    } finally {
      setBusy(false)
    }
  }

  const addTag = (name: string) => {
    const value = name.trim()
    if (!value || tags.includes(value)) { setInput(''); return }
    commit([...tags, value])
    setInput('')
  }

  const removeTag = (name: string) => commit(tags.filter(tag => tag !== name))

  // 回车 = 与「点选建议标签」同路径：即时保存当前输入。IME 组合中（中文候选确认）
  // 不提交；preventDefault 防止表单默认提交；stopPropagation 防止冒泡触发宿主
  // 卡片/页面的全局快捷键（浮层在卡片之上）。
  const handleKeyDown = (e: React.KeyboardEvent<HTMLInputElement>) => {
    if (e.key === 'Enter') {
      e.preventDefault()
      e.stopPropagation()
      if (e.nativeEvent.isComposing) return
      addTag(input)
    } else if (e.key === 'Escape') {
      e.stopPropagation()
      onClose()
    }
  }

  // 建议列表：全局已有标签中排除当前已选的
  const suggestions = allTags.filter(tag => !tags.includes(tag))

  return (
    <div className="fixed inset-0 z-[100]" onClick={onClose} onContextMenu={e => { e.preventDefault(); onClose() }}>
      <div
        ref={ref}
        className="fixed top-0 left-0 w-[240px] bg-white border border-gray-200 rounded-lg shadow-lg p-2.5"
        style={{ left: pos.x, top: pos.y }}
        onClick={e => e.stopPropagation()}
      >
        <div className="flex items-center justify-between mb-2">
          <span className="text-xs font-medium text-gray-700">
            {busy ? t('host:tags.batchSaving') : t('host:tags.title')}
          </span>
          <button onClick={onClose} className="text-gray-400 hover:text-gray-600 cursor-pointer" title={t('button.close')}>
            <X size={13} />
          </button>
        </div>

        <input
          type="text"
          ref={inputRef}
          value={input}
          autoFocus
          onChange={e => setInput(e.target.value)}
          onKeyDown={handleKeyDown}
          placeholder={t('host:tags.inputPlaceholder')}
          className="w-full text-xs px-2 py-1.5 border border-gray-200 rounded focus:outline-none focus:border-blue-400 placeholder:text-gray-300"
        />

        {tags.length > 0 && (
          <div className="flex flex-wrap gap-1 mt-2">
            {tags.map(tag => {
              const style = tagStyle(tag)
              return (
                <span key={tag} className={`inline-flex items-center gap-1 px-1.5 py-0.5 text-[11px] rounded border ${style.chip}`}>
                  <span className="truncate max-w-[100px]">{tag}</span>
                  <button onClick={() => removeTag(tag)} className="hover:text-red-500 cursor-pointer shrink-0" title={t('button.delete')}>
                    <X size={10} />
                  </button>
                </span>
              )
            })}
          </div>
        )}

        {tags.length === 0 && (
          <div className="mt-2 text-[10px] text-gray-400">
            {t('host:tags.emptyHint')}
          </div>
        )}

        {suggestions.length > 0 && (
          <div className="mt-2 pt-2 border-t border-gray-100">
            {/* 措辞强调「可点选添加」而非「已有」：容器无标签时 suggestions 会显示
                全部聚合标签，旧措辞「已有标签」让用户误读成「当前容器的标签全集」，
                且建议 chip 无删除按钮，被当成「删不掉」（2026-10-17 实测反馈） */}
            <div className="text-[10px] text-gray-400 mb-1">{t('host:tags.existing')}</div>
            <div className="flex flex-wrap gap-1 max-h-[120px] overflow-y-auto">
              {suggestions.map(tag => {
                const style = tagStyle(tag)
                return (
                  <button
                    key={tag}
                    onClick={() => addTag(tag)}
                    title={t('host:tags.addToThis')}
                    className={`px-1.5 py-0.5 text-[11px] rounded border cursor-pointer hover:brightness-95 ${style.chip}`}
                  >
                    {tag}
                  </button>
                )
              })}
            </div>
          </div>
        )}

        {tags.length > 0 && (
          <button
            onClick={() => commit([])}
            className="mt-2 w-full text-[11px] text-red-500 hover:text-red-600 hover:bg-red-50 rounded py-1 transition-colors cursor-pointer"
          >
            {t('host:tags.clearAll')}
          </button>
        )}
      </div>
    </div>
  )
}

export default SlotTagEditor
