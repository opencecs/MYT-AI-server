import { useState, useRef} from 'react'
import { X, FileCode } from 'lucide-react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { useTranslation } from 'react-i18next'
import { agentC as c } from './agentTheme'
import type { SkillItem } from './agentApi'
import { useModalEnter } from '../../hooks/useModalAnimation'

interface Props {
  skill: SkillItem
  actions: { label: string; icon: React.ReactNode; onClick: () => void; danger?: boolean }[]
  onClose: () => void
}

function SkillDetailDialog({ skill, actions, onClose }: Props) {
  const { t } = useTranslation()
  const [activeFile, setActiveFile] = useState('SKILL.md')
  const scriptNames = skill.scripts ? Object.keys(skill.scripts) : []
  const files = ['SKILL.md', ...scriptNames]

  const rootRef = useRef<HTMLDivElement>(null)
  useModalEnter(rootRef)
  return (
    <div className="fixed inset-0 z-[200] flex items-center justify-center bg-black/30 modal-overlay" ref={rootRef} onClick={onClose}>
      <div className="rounded-xl border shadow-xl w-[680px] max-w-[calc(100vw-32px)] max-h-[80vh] flex flex-col modal-content" style={{ borderColor: c.line, background: c.surface }} onClick={e => e.stopPropagation()}>
        <div className="flex items-center gap-2 border-b px-5 py-3" style={{ borderColor: c.line }}>
          <span className="text-sm font-semibold" style={{ color: c.text }}>{skill.name}</span>
          {skill.description && <span className="text-xs truncate" style={{ color: c.textMuted }}>{skill.description}</span>}
          <button onClick={onClose} className="ml-auto" style={{ color: c.textMuted }}><X className="size-4" /></button>
        </div>
        <div className="flex flex-1 overflow-hidden">
          {/* 文件列表 */}
          <div className="w-40 border-r overflow-y-auto shrink-0" style={{ borderColor: c.line }}>
            {files.map(fn => (
              <button key={fn} onClick={() => setActiveFile(fn)}
                className="flex items-center gap-1.5 w-full px-3 py-2 text-left text-xs hover:bg-black/[0.02]"
                style={{ background: activeFile === fn ? `${c.accent}08` : undefined, color: activeFile === fn ? c.accent : c.text }}>
                {fn === 'SKILL.md' ? <FileCode className="size-3 shrink-0" /> : <FileCode className="size-3 shrink-0" />}
                <span className="truncate">{fn}</span>
              </button>
            ))}
          </div>
          {/* 内容 */}
          <div className="flex-1 overflow-y-auto p-4">
            {activeFile === 'SKILL.md' ? (
              <div className="prose-chat text-sm leading-relaxed" style={{ color: c.text }}>
                <ReactMarkdown remarkPlugins={[remarkGfm]}>{skill.content || t('agent:skillDetail.noContent')}</ReactMarkdown>
              </div>
            ) : (
              <pre className="text-[11px] font-mono whitespace-pre-wrap break-words" style={{ color: c.text }}>{skill.scripts?.[activeFile] || ''}</pre>
            )}
          </div>
        </div>
        {/* 操作栏 */}
        <div className="flex items-center gap-2 border-t px-5 py-3" style={{ borderColor: c.line }}>
          {actions.map((a, i) => (
            <button key={i} onClick={a.onClick} className={`flex items-center gap-1 rounded-lg px-3 py-1.5 text-xs ${a.danger ? 'hover:bg-red-500/10' : 'hover:bg-black/5'}`}
              style={{ color: a.danger ? c.danger : c.textMuted }}>
              {a.icon}{a.label}
            </button>
          ))}
          <div className="flex-1" />
        </div>
      </div>
    </div>
  )
}

export default SkillDetailDialog
