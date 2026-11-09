import { useRef, useState, useCallback } from 'react'
import { Send, Square, Mic, Volume2, VolumeX, Loader2, ImagePlus, Paperclip, X, FileText, UploadCloud } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { agentC as c } from './agentTheme'
import type { AgentConnInfo } from './agentTypes'
import { agentUpload } from './agentApi'

const MAX_ATTACHMENTS = 9
const FILE_ACCEPT = '.txt,.md,.json,.csv,.log,.xml,.yaml,.yml,.html,.js,.ts,.go,.py,.sh,.sql,.conf,.ini,.env,.doc,.docx,.pdf'

interface Props {
  onSend: (text: string, images?: string[]) => void
  onCancel: () => void
  disabled: boolean
  sending: boolean
  recording: boolean
  ttsEnabled: boolean
  ttsLoading: boolean
  connInfo: AgentConnInfo | null
  inputText: string
  setInputText: (text: string) => void
  onStartRecording: (onResult: (text: string) => void) => void
  onStopRecording: () => void
  onToggleTTS: () => void
}

function AgentInput({ onSend, onCancel, disabled, sending, recording, ttsEnabled, ttsLoading, connInfo, inputText, setInputText, onStartRecording, onStopRecording, onToggleTTS }: Props) {
  const { t } = useTranslation()
  const [pendingImages, setPendingImages] = useState<string[]>([])
  const [pendingFiles, setPendingFiles] = useState<{ name: string; size: number; file: File }[]>([])
  const [fileUploading, setFileUploading] = useState(false)
  const [isDragging, setIsDragging] = useState(false)
  const dragCounter = useRef(0)
  const ref = useRef<HTMLTextAreaElement>(null)
  const imgInputRef = useRef<HTMLInputElement>(null)
  const fileInputRef = useRef<HTMLInputElement>(null)

  const handleSend = useCallback(async () => {
    const trimmed = inputText.trim()
    if (!trimmed && pendingImages.length === 0 && pendingFiles.length === 0) return
    if (disabled || fileUploading) return

    let fileText = ''
    if (pendingFiles.length > 0 && connInfo) {
      setFileUploading(true)
      try {
        for (const f of pendingFiles) {
          const data = new Uint8Array(await f.file.arrayBuffer())
          const res = await agentUpload(f.name, data)
          if (!res.ok) throw new Error(t('agent:input.uploadFailed'))
          const sizeKB = (f.size / 1024).toFixed(1)
          const sizeHint = f.size > 100 * 1024 ? t('agent:input.fileTooLarge') : ''
          fileText += `\n[File attached: ${res.path}（${f.name}，${sizeKB}KB${sizeHint}）]`
        }
      } catch (e) {
        alert(t('agent:input.fileUploadFailed', { error: e instanceof Error ? e.message : t('agent:chat.unknownError') }))
        setFileUploading(false)
        return
      }
      setFileUploading(false)
    }

    const msg = (trimmed || (pendingImages.length > 0 ? t('agent:chat.analyzeImages') : t('agent:input.processFiles'))) + fileText
    onSend(msg, pendingImages.length > 0 ? pendingImages : undefined)
    setInputText('')
    setPendingImages([])
    setPendingFiles([])
    if (ref.current) ref.current.style.height = 'auto'
  }, [inputText, disabled, pendingImages, pendingFiles, fileUploading, connInfo, onSend, setInputText, t])

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); handleSend() }
  }

  const handleInput = (e: React.ChangeEvent<HTMLTextAreaElement>) => {
    setInputText(e.target.value)
    const el = e.target
    el.style.height = 'auto'
    el.style.height = Math.min(el.scrollHeight, 160) + 'px'
  }

  const handleFile = useCallback((file: File) => {
    const total = pendingImages.length + pendingFiles.length
    if (total >= MAX_ATTACHMENTS) { alert(t('agent:input.maxAttachments', { count: MAX_ATTACHMENTS })); return }
    if (file.type.startsWith('image/')) {
      const reader = new FileReader()
      reader.onload = () => {
        const result = reader.result as string
        setPendingImages(prev => [...prev, result.split(',')[1]])
      }
      reader.readAsDataURL(file)
      return
    }
    setPendingFiles(prev => [...prev, { name: file.name, size: file.size, file }])
  }, [pendingImages.length, pendingFiles.length, t])

  const handlePaste = useCallback((e: React.ClipboardEvent) => {
    for (const item of e.clipboardData.items) {
      if (item.type.startsWith('image/')) {
        const file = item.getAsFile()
        if (file) { handleFile(file); e.preventDefault(); return }
      }
    }
  }, [handleFile])

  const handleDrop = useCallback((e: React.DragEvent) => {
    e.preventDefault()
    dragCounter.current = 0
    setIsDragging(false)
    const files = e.dataTransfer.files
    for (let i = 0; i < files.length; i++) {
      if (pendingImages.length + pendingFiles.length + i >= MAX_ATTACHMENTS) break
      handleFile(files[i])
    }
  }, [handleFile, pendingImages.length, pendingFiles.length])

  const handleDragEnter = (e: React.DragEvent) => {
    e.preventDefault()
    if (e.dataTransfer.types.includes('Files')) { dragCounter.current++; setIsDragging(true) }
  }
  const handleDragLeave = (e: React.DragEvent) => {
    e.preventDefault()
    dragCounter.current--
    if (dragCounter.current <= 0) { dragCounter.current = 0; setIsDragging(false) }
  }
  const handleDragOver = (e: React.DragEvent) => e.preventDefault()

  return (
    <div className="shrink-0 border-t px-8 py-3" style={{ borderColor: c.line, background: c.surface, minHeight: 96 }}
      onDrop={handleDrop} onDragEnter={handleDragEnter} onDragLeave={handleDragLeave} onDragOver={handleDragOver}>
      <div className="mx-auto w-full max-w-4xl">
        {/* 拖拽遮罩 */}
        {isDragging && (
          <div className="mb-2 flex items-center justify-center gap-2 rounded-xl border-2 border-dashed py-6" style={{ borderColor: c.accent, background: `${c.accent}08` }}>
            <UploadCloud className="size-6" style={{ color: c.accent }} />
            <span className="text-sm" style={{ color: c.accent }}>{t('agent:input.releaseToUpload')}</span>
          </div>
        )}
        {/* 附件预览 */}
        {(pendingImages.length > 0 || pendingFiles.length > 0) && (
          <div className="mb-1.5 flex flex-wrap gap-1.5 px-1">
            {pendingImages.map((img, i) => (
              <div key={`img-${i}`} className="relative">
                <img src={`data:image/jpeg;base64,${img}`} alt="preview" className="h-12 w-12 rounded object-cover" />
                <button onClick={() => setPendingImages(prev => prev.filter((_, idx) => idx !== i))} className="absolute -right-1 -top-1 flex size-4 items-center justify-center rounded-full text-white" style={{ background: c.danger }}>
                  <X className="size-2.5" />
                </button>
              </div>
            ))}
            {pendingFiles.map((f, i) => (
              <div key={`file-${i}`} className="relative flex h-12 items-center gap-1.5 rounded px-2" style={{ background: c.surfaceAlt, border: `1px solid ${c.line}` }}>
                <FileText className="size-4 shrink-0" style={{ color: c.accent }} />
                <span className="max-w-24 truncate text-[11px]" style={{ color: c.text }}>{f.name}</span>
                <span className="text-[10px]" style={{ color: c.textMuted }}>{(f.size / 1024).toFixed(0)}K</span>
                <button onClick={() => setPendingFiles(prev => prev.filter((_, idx) => idx !== i))} className="flex size-4 items-center justify-center rounded-full text-white shrink-0" style={{ background: c.danger }}>
                  <X className="size-2.5" />
                </button>
              </div>
            ))}
          </div>
        )}
        <div className="flex items-end gap-2 rounded-xl border p-2" style={{ borderColor: c.line, background: c.surfaceAlt }}>
          <input ref={imgInputRef} type="file" accept="image/*" multiple className="hidden" onChange={(e) => { for (const f of Array.from(e.target.files ?? [])) handleFile(f); e.target.value = '' }} />
          <button onClick={() => imgInputRef.current?.click()} disabled={disabled} title={t('agent:tooltip.uploadImage')}
            className="flex size-9 shrink-0 items-center justify-center rounded-lg transition-colors disabled:opacity-40 hover:bg-black/5">
            <ImagePlus className="size-4" style={{ color: c.textMuted }} />
          </button>
          <input ref={fileInputRef} type="file" accept={FILE_ACCEPT} multiple className="hidden" onChange={(e) => { for (const f of Array.from(e.target.files ?? [])) handleFile(f); e.target.value = '' }} />
          <button onClick={() => fileInputRef.current?.click()} disabled={disabled} title={t('agent:tooltip.uploadFile')}
            className="flex size-9 shrink-0 items-center justify-center rounded-lg transition-colors disabled:opacity-40 hover:bg-black/5">
            <Paperclip className="size-4" style={{ color: c.textMuted }} />
          </button>
          {/* TTS */}
          <button onClick={onToggleTTS} disabled={disabled} title={ttsEnabled ? t('agent:tooltip.ttsOff') : t('agent:tooltip.ttsOn')}
            className="flex size-9 shrink-0 items-center justify-center rounded-lg transition-colors disabled:opacity-40 hover:bg-black/5"
            style={{ background: ttsEnabled ? `${c.accent}15` : 'transparent' }}>
            {ttsLoading ? <Loader2 className="size-4 animate-spin" style={{ color: c.accent }} />
              : ttsEnabled ? <Volume2 className="size-4" style={{ color: c.accent }} />
              : <VolumeX className="size-4" style={{ color: c.textMuted }} />}
          </button>
          {/* 录音 */}
          <button onMouseDown={() => onStartRecording(setInputText)} onMouseUp={onStopRecording}
            onMouseLeave={() => { if (recording) onStopRecording() }}
            disabled={disabled} title={t('agent:tooltip.holdToSpeak')}
            className="flex size-9 shrink-0 items-center justify-center rounded-lg transition-colors disabled:opacity-40"
            style={{ background: recording ? `${c.danger}20` : 'transparent' }}>
            <Mic className={`size-4 ${recording ? 'animate-pulse' : ''}`} style={{ color: recording ? c.danger : c.textMuted }} />
          </button>
          <textarea ref={ref} value={inputText} onChange={handleInput} onKeyDown={handleKeyDown} onPaste={handlePaste}
            placeholder={disabled ? t('agent:input.connecting') : recording ? t('agent:input.recording') : t('agent:input.placeholder')}
            disabled={disabled} rows={1}
            className="flex-1 resize-none bg-transparent px-3 py-2 text-sm outline-none disabled:cursor-not-allowed"
            style={{ color: c.text }} />
          {sending ? (
            <button onClick={onCancel} title={t('agent:tooltip.stopGeneration')}
              className="flex size-9 shrink-0 items-center justify-center rounded-lg text-white transition-opacity hover:opacity-80"
              style={{ background: c.danger }}>
              <Square className="size-4" />
            </button>
          ) : fileUploading ? (
            <div className="flex size-9 shrink-0 items-center justify-center rounded-lg" style={{ background: `${c.accent}40` }}>
              <Loader2 className="size-4 animate-spin" style={{ color: c.accent }} />
            </div>
          ) : (
            <button onClick={handleSend} disabled={disabled || (!inputText.trim() && pendingImages.length === 0 && pendingFiles.length === 0)} title={t('agent:tooltip.send')}
              className="flex size-9 shrink-0 items-center justify-center rounded-lg text-white transition-opacity disabled:opacity-40"
              style={{ background: c.accent }}>
              <Send className="size-4" />
            </button>
          )}
        </div>
      </div>
    </div>
  )
}

export default AgentInput
