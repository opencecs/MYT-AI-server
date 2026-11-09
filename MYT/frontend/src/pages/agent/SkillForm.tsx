import { useState, useCallback } from 'react'
import { X, Plus, Trash2, FileCode } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { agentC as c } from './agentTheme'
import type { SkillItem, FormFieldDef } from './agentApi'

interface Props {
  initial?: SkillItem
  onClose: () => void
  onSave: (skill: SkillItem) => Promise<void>
}

function SkillForm({ initial, onClose, onSave }: Props) {
  const { t } = useTranslation()
  const [name, setName] = useState(initial?.name || '')
  const [description, setDescription] = useState(initial?.description || '')
  const [content, setContent] = useState(initial?.content || '')
  const [scripts, setScripts] = useState<Record<string, string>>(initial?.scripts || {})
  const [formFields, setFormFields] = useState<FormFieldDef[]>(initial?.form_schema || [])
  const [enabled, setEnabled] = useState(initial?.enabled !== false)
  const [saving, setSaving] = useState(false)
  const [newFileName, setNewFileName] = useState('')

  const handleSave = useCallback(async () => {
    if (!name.trim()) return
    setSaving(true)
    try {
      await onSave({ name: name.trim(), description, content, scripts, form_schema: formFields.length > 0 ? formFields : undefined, enabled })
      onClose()
    } catch { /* */ }
    setSaving(false)
  }, [name, description, content, scripts, formFields, enabled, onSave, onClose])

  const addScript = () => {
    const fn = newFileName.trim()
    if (!fn || scripts[fn]) return
    setScripts(prev => ({ ...prev, [fn]: '' }))
    setNewFileName('')
  }
  const removeScript = (fn: string) => setScripts(prev => { const n = { ...prev }; delete n[fn]; return n })

  const addField = () => setFormFields(prev => [...prev, { name: '', label: '', type: 'text' }])
  const updateField = (i: number, patch: Partial<FormFieldDef>) => setFormFields(prev => prev.map((f, idx) => idx === i ? { ...f, ...patch } : f))
  const removeField = (i: number) => setFormFields(prev => prev.filter((_, idx) => idx !== i))

  const scriptNames = Object.keys(scripts)

  return (
    <div className="fixed inset-0 z-[200] flex items-center justify-center bg-black/30" onClick={onClose}>
      <div className="rounded-xl border shadow-xl w-[600px] max-w-[calc(100vw-32px)] max-h-[85vh] flex flex-col" style={{ borderColor: c.line, background: c.surface }} onClick={e => e.stopPropagation()}>
        <div className="flex items-center gap-2 border-b px-5 py-3" style={{ borderColor: c.line }}>
          <span className="text-sm font-semibold" style={{ color: c.text }}>{initial ? t('agent:skillForm.editTitle') : t('agent:skillForm.newTitle')}</span>
          <button onClick={onClose} className="ml-auto" style={{ color: c.textMuted }}><X className="size-4" /></button>
        </div>
        <div className="flex-1 overflow-y-auto p-5 space-y-4">
          <Field label={t('agent:skillForm.name')}>
            <input value={name} onChange={e => setName(e.target.value)} disabled={!!initial} placeholder="my_skill"
              className="w-full rounded-lg border px-3 py-2 text-sm font-mono outline-none focus:border-blue-400 disabled:opacity-60"
              style={{ borderColor: c.line, background: c.surfaceAlt, color: c.text }} />
          </Field>
          <Field label={t('agent:skillForm.description')}>
            <input value={description} onChange={e => setDescription(e.target.value)} placeholder={t('agent:skillForm.descriptionPlaceholder')}
              className="w-full rounded-lg border px-3 py-2 text-sm outline-none focus:border-blue-400"
              style={{ borderColor: c.line, background: c.surfaceAlt, color: c.text }} />
          </Field>
          <Field label={t('agent:skillForm.content')}>
            <textarea value={content} onChange={e => setContent(e.target.value)} rows={8} placeholder={t('agent:skillForm.contentPlaceholder')}
              className="w-full rounded-lg border px-3 py-2 text-sm font-mono outline-none focus:border-blue-400 resize-y"
              style={{ borderColor: c.line, background: c.surfaceAlt, color: c.text }} />
          </Field>
          {/* 脚本文件 */}
          <Field label={t('agent:skillForm.scripts')}>
            <div className="space-y-1.5">
              {scriptNames.map(fn => (
                <div key={fn} className="rounded-lg border p-2" style={{ borderColor: c.line, background: c.surfaceAlt }}>
                  <div className="flex items-center gap-2 mb-1">
                    <FileCode className="size-3.5" style={{ color: c.accent }} />
                    <span className="text-xs font-mono flex-1" style={{ color: c.text }}>{fn}</span>
                    <button onClick={() => removeScript(fn)} className="text-red-500 hover:bg-red-500/10 rounded p-0.5"><Trash2 className="size-3" /></button>
                  </div>
                  <textarea value={scripts[fn]} onChange={e => setScripts(prev => ({ ...prev, [fn]: e.target.value }))} rows={4}
                    className="w-full text-[11px] font-mono rounded border px-2 py-1 outline-none resize-y"
                    style={{ borderColor: c.line, background: c.surface, color: c.text }} />
                </div>
              ))}
              <div className="flex items-center gap-1.5">
                <input value={newFileName} onChange={e => setNewFileName(e.target.value)} onKeyDown={e => e.key === 'Enter' && addScript()} placeholder="script.py"
                  className="flex-1 rounded border px-2 py-1 text-xs font-mono outline-none" style={{ borderColor: c.line, background: c.surfaceAlt, color: c.text }} />
                <button onClick={addScript} className="flex items-center gap-1 rounded px-2 py-1 text-xs" style={{ background: c.surfaceAlt, color: c.textMuted }}><Plus className="size-3" />{t('agent:skillForm.addScript')}</button>
              </div>
            </div>
          </Field>
          {/* 表单参数 */}
          <Field label={t('agent:skillForm.formFields')}>
            <div className="space-y-1.5">
              {formFields.map((f, i) => (
                <div key={i} className="flex items-center gap-1.5 rounded-lg border p-2" style={{ borderColor: c.line, background: c.surfaceAlt }}>
                  <input value={f.name} onChange={e => updateField(i, { name: e.target.value })} placeholder={t('agent:skillForm.paramName')} className="flex-1 text-xs font-mono rounded border px-1.5 py-1 outline-none" style={{ borderColor: c.line, color: c.text }} />
                  <input value={f.label} onChange={e => updateField(i, { label: e.target.value })} placeholder={t('agent:skillForm.paramLabel')} className="flex-1 text-xs rounded border px-1.5 py-1 outline-none" style={{ borderColor: c.line, color: c.text }} />
                  <select value={f.type} onChange={e => updateField(i, { type: e.target.value as FormFieldDef['type'] })} className="text-xs rounded border px-1 py-1 outline-none" style={{ borderColor: c.line, color: c.text, background: c.surface }}>
                    {['text', 'number', 'select', 'textarea', 'password'].map(t => <option key={t} value={t}>{t}</option>)}
                  </select>
                  <button onClick={() => removeField(i)} className="text-red-500 hover:bg-red-500/10 rounded p-0.5"><Trash2 className="size-3" /></button>
                </div>
              ))}
              <button onClick={addField} className="flex items-center gap-1 text-xs" style={{ color: c.accent }}><Plus className="size-3" />{t('agent:skillForm.addParam')}</button>
            </div>
          </Field>
          <label className="flex items-center gap-2 text-xs" style={{ color: c.text }}>
            <input type="checkbox" checked={enabled} onChange={e => setEnabled(e.target.checked)} className="rounded" />
            {t('agent:skillForm.enableSkill')}
          </label>
        </div>
        <div className="flex justify-end gap-2 border-t px-5 py-3" style={{ borderColor: c.line }}>
          <button onClick={onClose} className="px-4 py-1.5 text-xs rounded-lg hover:bg-black/5" style={{ color: c.textMuted }}>{t('button.cancel')}</button>
          <button onClick={handleSave} disabled={!name.trim() || saving} className="px-4 py-1.5 text-xs rounded-lg text-white disabled:opacity-40" style={{ background: c.accent }}>{saving ? t('agent:skillForm.saving') : t('button.save')}</button>
        </div>
      </div>
    </div>
  )
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div>
      <label className="text-[11px] font-semibold uppercase tracking-wider mb-1.5 block" style={{ color: c.textMuted }}>{label}</label>
      {children}
    </div>
  )
}

export default SkillForm
