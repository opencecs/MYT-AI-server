import { useState, useCallback, useRef, createContext, useContext, ReactNode, createElement } from 'react'
import { createPortal } from 'react-dom'
import { useTranslation } from 'react-i18next'
import { agentC as c } from './agentTheme'

interface DialogOptions {
  title?: string
  message: string
  confirmLabel?: string
  cancelLabel?: string
  danger?: boolean
}

type DialogResolver = (value: boolean | string | null) => void

interface DialogState {
  options: DialogOptions
  resolve: DialogResolver
  id: number
}

interface PromptOptions extends DialogOptions {
  placeholder?: string
  defaultValue?: string
}

interface PromptState extends DialogState {
  prompt: boolean
  value: string
  placeholder?: string
}

type DialogResult = boolean | string | null

const DialogCtx = createContext<{
  confirm: (opts: string | DialogOptions) => Promise<boolean>
  alert: (opts: string | DialogOptions) => Promise<void>
  error: (opts: string | DialogOptions) => Promise<void>
  prompt: (opts: string | PromptOptions) => Promise<string | null>
}>({ confirm: async () => false, alert: async () => {}, error: async () => {}, prompt: async () => null })

export function AgentDialogProvider({ children }: { children: ReactNode }) {
  const { t } = useTranslation()
  const [dialogs, setDialogs] = useState<PromptState[]>([])
  const idRef = useRef(0)

  const resolve = useCallback((id: number, result: DialogResult) => {
    setDialogs(prev => {
      const d = prev.find(x => x.id === id)
      if (d) d.resolve(result)
      return prev.filter(x => x.id !== id)
    })
  }, [])

  const add = useCallback((options: DialogOptions, promptOpts?: { placeholder?: string; defaultValue?: string }): Promise<DialogResult> => {
    const id = ++idRef.current
    return new Promise<DialogResult>(resolveFn => {
      setDialogs(prev => [...prev, { options, resolve: resolveFn as DialogResolver, id, prompt: !!promptOpts, value: promptOpts?.defaultValue || '', placeholder: promptOpts?.placeholder }])
    })
  }, [])

  const wrap = (opts: string | DialogOptions): DialogOptions =>
    typeof opts === 'string' ? { message: opts } : opts

  const confirm = useCallback((opts: string | DialogOptions) => add(wrap(opts)).then(r => r === true), [add])
  const alert = useCallback((opts: string | DialogOptions) => add({ ...wrap(opts), cancelLabel: t('agent:dialog.gotIt') }).then(() => undefined), [add, t])
  const error = useCallback((opts: string | DialogOptions) => add({ ...wrap(opts), danger: true, cancelLabel: t('agent:dialog.gotIt') }).then(() => undefined), [add, t])
  const prompt = useCallback((opts: string | PromptOptions) => {
    const o = typeof opts === 'string' ? { message: opts } : opts
    return add({ ...o }, { placeholder: o.placeholder, defaultValue: o.defaultValue }).then(r => typeof r === 'string' ? r : null)
  }, [add])

  const api = { confirm, alert, error, prompt }

  return createElement(DialogCtx.Provider, { value: api },
    children,
    createPortal(
      <div>
        {dialogs.map(d => (
          <div key={d.id} className="fixed inset-0 z-[300] flex items-center justify-center bg-black/30" onClick={() => resolve(d.id, false)}>
            <div className="rounded-xl border shadow-xl p-6 w-80" style={{ borderColor: c.line, background: c.surface }} onClick={e => e.stopPropagation()}>
              {d.options.title && <h3 className="text-sm font-semibold mb-2" style={{ color: c.text }}>{d.options.title}</h3>}
              <p className="text-xs mb-4 whitespace-pre-wrap" style={{ color: c.textMuted }}>{d.options.message}</p>
              {d.prompt && (
                <input autoFocus value={d.value} placeholder={d.placeholder}
                  onChange={e => setDialogs(prev => prev.map(x => x.id === d.id ? { ...x, value: e.target.value } : x))}
                  onKeyDown={e => { if (e.key === 'Enter') resolve(d.id, d.value); if (e.key === 'Escape') resolve(d.id, null) }}
                  className="w-full mb-4 rounded-lg border px-3 py-2 text-sm outline-none focus:border-blue-400"
                  style={{ borderColor: c.line, background: c.surfaceAlt, color: c.text }} />
              )}
              <div className="flex justify-end gap-2">
                {!d.prompt && (
                  <button onClick={() => resolve(d.id, false)} className="px-3 py-1 text-xs rounded-lg hover:bg-black/5" style={{ color: c.textMuted }}>
                    {d.options.cancelLabel || t('button.cancel')}
                  </button>
                )}
                {d.prompt && (
                  <button onClick={() => resolve(d.id, null)} className="px-3 py-1 text-xs rounded-lg hover:bg-black/5" style={{ color: c.textMuted }}>{t('button.cancel')}</button>
                )}
                {d.prompt ? (
                  <button onClick={() => resolve(d.id, d.value)} className="px-3 py-1 text-xs rounded-lg text-white" style={{ background: c.accent }}>{t('button.confirm')}</button>
                ) : (
                  <button onClick={() => resolve(d.id, true)} className="px-3 py-1 text-xs rounded-lg text-white" style={{ background: d.options.danger ? c.danger : c.accent }}>
                    {d.options.confirmLabel || t('button.confirm')}
                  </button>
                )}
              </div>
            </div>
          </div>
        ))}
      </div>,
      document.body
    )
  )
}

export function useAgentDialog() {
  return useContext(DialogCtx)
}
