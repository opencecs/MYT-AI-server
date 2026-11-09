import { useState, useRef } from 'react'
import { createPortal } from 'react-dom'
import { ZoomIn, X, FileDown, Volume2, Play, Pause, FileText, FileCode, FileJson, FileImage, FileArchive, FileSpreadsheet, File } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import i18n from '../../i18n'
import { agentC as c } from './agentTheme'

type IconType = typeof FileText

function getFileIcon(filename: string): { icon: IconType; color: string; label: string } {
  const ext = filename.split('.').pop()?.toLowerCase() ?? ''
  const map: Record<string, { icon: IconType; color: string; label: string }> = {
    txt:  { icon: FileText,        color: '#6B7280', label: 'TXT' },
    log:  { icon: FileText,        color: '#6B7280', label: 'LOG' },
    md:   { icon: FileText,        color: '#6B7280', label: 'MD' },
    json: { icon: FileJson,        color: '#F59E0B', label: 'JSON' },
    js:   { icon: FileCode,        color: '#EAB308', label: 'JS' },
    ts:   { icon: FileCode,        color: '#3B82F6', label: 'TS' },
    tsx:  { icon: FileCode,        color: '#3B82F6', label: 'TSX' },
    go:   { icon: FileCode,        color: '#06B6D4', label: 'GO' },
    py:   { icon: FileCode,        color: '#10B981', label: 'PY' },
    sh:   { icon: FileCode,        color: '#6B7280', label: 'SH' },
    yml:  { icon: FileCode,        color: '#8B5CF6', label: 'YML' },
    yaml: { icon: FileCode,        color: '#8B5CF6', label: 'YAML' },
    csv:  { icon: FileSpreadsheet, color: '#10B981', label: 'CSV' },
    xls:  { icon: FileSpreadsheet, color: '#10B981', label: 'XLS' },
    xlsx: { icon: FileSpreadsheet, color: '#10B981', label: 'XLSX' },
    jpg:  { icon: FileImage,       color: '#EC4899', label: 'JPG' },
    jpeg: { icon: FileImage,       color: '#EC4899', label: 'JPEG' },
    png:  { icon: FileImage,       color: '#EC4899', label: 'PNG' },
    gif:  { icon: FileImage,       color: '#EC4899', label: 'GIF' },
    webp: { icon: FileImage,       color: '#EC4899', label: 'WEBP' },
    zip:  { icon: FileArchive,     color: '#F97316', label: 'ZIP' },
    tar:  { icon: FileArchive,     color: '#F97316', label: 'TAR' },
    gz:   { icon: FileArchive,     color: '#F97316', label: 'GZ' },
    mp3:  { icon: Volume2,         color: '#8B5CF6', label: 'MP3' },
    wav:  { icon: Volume2,         color: '#8B5CF6', label: 'WAV' },
  }
  return map[ext] ?? { icon: File, color: '#6B7280', label: ext.toUpperCase() || 'FILE' }
}

export function FileCard({ href, filename }: { href: string; filename: string }) {
  const { icon: Icon, color, label } = getFileIcon(filename)
  return (
    <a href={href} download
      className="group flex items-center gap-3 rounded-xl border p-3 transition-all hover:shadow-md no-underline"
      style={{ borderColor: c.line, background: c.surface }}>
      <span className="flex size-11 shrink-0 items-center justify-center rounded-lg"
        style={{ background: color + '15', border: `1px solid ${color}30` }}>
        <Icon className="size-5" style={{ color }} />
      </span>
      <span className="min-w-0 flex-1">
        <span className="block truncate text-sm font-medium" style={{ color: c.text }}>{filename}</span>
        <span className="block text-[10px] font-medium tracking-wider" style={{ color: c.textMuted }}>{label}</span>
      </span>
      <FileDown className="size-4 shrink-0 opacity-0 transition-opacity group-hover:opacity-100" style={{ color: c.textMuted }} />
    </a>
  )
}

export function ClickableImage({ src, alt, className, style }: { src: string; alt?: string; className?: string; style?: React.CSSProperties }) {
  const [zoomed, setZoomed] = useState(false)
  return (
    <>
      <span className="group relative inline-block" onClick={() => setZoomed(true)}>
        <img src={src} alt={alt ?? ''} className={`cursor-zoom-in transition-opacity ${className ?? ''}`} style={style} />
        <span className="pointer-events-none absolute inset-0 flex items-center justify-center bg-black/0 opacity-0 transition-all group-hover:bg-black/30 group-hover:opacity-100">
          <ZoomIn className="size-5 text-white drop-shadow" />
        </span>
      </span>
      {zoomed && createPortal(
        <div className="fixed inset-0 z-[200] flex items-center justify-center bg-black/80 backdrop-blur-sm animate-fadeIn" onClick={() => setZoomed(false)}>
          <img src={src} alt={alt ?? ''} className="max-h-[90vh] max-w-[90vw] rounded-lg object-contain shadow-2xl" />
          <button className="absolute right-4 top-4 rounded-full bg-white/10 p-2 text-white transition-colors hover:bg-white/20" onClick={(e) => { e.stopPropagation(); setZoomed(false) }}>
            <X className="size-5" />
          </button>
        </div>,
        document.body
      )}
    </>
  )
}

function AudioPlayer({ src }: { src: string }) {
  const { t } = useTranslation()
  const audioRef = useRef<HTMLAudioElement>(null)
  const [playing, setPlaying] = useState(false)
  const toggle = () => {
    if (!audioRef.current) return
    if (playing) { audioRef.current.pause() } else { audioRef.current.play() }
  }
  return (
    <span className="my-2 inline-flex items-center gap-2 rounded-lg border px-3 py-2" style={{ borderColor: c.line, background: c.surfaceAlt }}>
      <audio ref={audioRef} src={src} onPlay={() => setPlaying(true)} onPause={() => setPlaying(false)} onEnded={() => setPlaying(false)} />
      <button onClick={toggle} className="flex size-8 items-center justify-center rounded-full text-white" style={{ background: c.accent }}>
        {playing ? <Pause className="size-4" /> : <Play className="size-4" />}
      </button>
      <Volume2 className="size-4" style={{ color: c.textMuted }} />
      <span className="text-xs" style={{ color: c.textMuted }}>{t('agent:audioVoice')}</span>
    </span>
  )
}

function toSandboxURL(url: string, baseUrl: string): string {
  url = url.trim()
  if (url.startsWith('http')) return url
  if (url.startsWith('/api/')) return baseUrl + url
  if (url.startsWith('data:')) return url
  const params = new URLSearchParams()
  params.set('path', url)
  return `${baseUrl}/api/sandbox-files?${params.toString()}`
}

function appendToken(url: string, token: string): string {
  if (!token) return url
  const sep = url.includes('?') ? '&' : '?'
  return url + sep + 'token=' + encodeURIComponent(token)
}

export function transformAttachments(text: string, baseUrl: string, token: string): string {
  return text
    .replace(/\[(?:Image attached at|图片已附于|图片附于)[:：]?\s*(.+?)\]/g, (_m, url) => {
      const full = appendToken(toSandboxURL(url, baseUrl), token)
      // 不加 cache-bust：流式输出时 msg.text 每帧变化会触发重渲染，
      // 若每次生成新 _t= 会导致 <img src> 每帧变化，远程图片重复请求闪烁。
      return `\n\n![](${full})\n\n`
    })
    .replace(/\[File attached at:\s*(.+?)\]/g, (_m, url) => {
      const full = toSandboxURL(url, baseUrl)
      const pathParam = full.split('path=')[1]?.split('&')[0] ?? ''
      const filename = pathParam ? (decodeURIComponent(pathParam).split('/').pop() ?? i18n.t('agent:fileFallback')) : i18n.t('agent:fileFallback')
      return `\n\n[${filename}](${appendToken(full, token)})\n\n`
    })
    .replace(/\[Audio attached at:\s*(.+?)\]/g, (_m, url) => `\n\n[🔊${i18n.t('agent:audioVoice')}](${appendToken(toSandboxURL(url, baseUrl), token)})\n\n`)
}

export function parseUserFileAttachments(text: string): { cleanText: string; files: { path: string; name: string; size: string }[] } {
  const files: { path: string; name: string; size: string }[] = []
  const cleanText = text.replace(/\[File attached:\s*(.+?)（(.+?)，(.+?)）\]/g, (_m, path, name, size) => {
    files.push({ path: path.trim(), name: name.trim(), size: size.trim() })
    return ''
  }).trim()
  return { cleanText, files }
}

export function buildFileHref(path: string, baseUrl: string, token: string): string {
  const params = new URLSearchParams()
  params.set('path', path)
  return appendToken(`${baseUrl}/api/sandbox-files?${params.toString()}`, token)
}

export function createMdComponents(baseUrl: string, token: string) {
  return {
    img: ({ src, alt }: { src?: string; alt?: string }) => {
      if (!src) return null
      if (/^data:image\//.test(src) || /^https?:\/\//i.test(src)) {
        return <ClickableImage src={src} alt={alt} className="my-2 max-h-96 max-w-full rounded-lg" style={{ border: `1px solid ${c.line}` }} />
      }
      if (src.startsWith(`${baseUrl}/api/sandbox-files`)) {
        return <ClickableImage src={src} alt={alt} className="my-2 max-h-96 max-w-full rounded-lg" style={{ border: `1px solid ${c.line}` }} />
      }
      return null
    },
    a: ({ href, children }: { href?: string; children?: React.ReactNode }) => {
      if (href?.startsWith(`${baseUrl}/api/sandbox-files`)) {
        const text = String(children ?? '')
        if (text.includes('🔊') || text.includes('音频')) {
          return <AudioPlayer src={href} />
        }
        const pathParam = href.split('path=')[1]?.split('&')[0] ?? ''
        const filename = pathParam ? (decodeURIComponent(pathParam).split('/').pop() ?? text) : text
        return <span className="my-2 block max-w-xs"><FileCard href={href} filename={filename} /></span>
      }
      if (href && !/^https?:|^mailto:|^tel:/i.test(href)) return <span>{children}</span>
      return <a href={href} target="_blank" rel="noopener noreferrer" className="underline underline-offset-2" style={{ color: c.accent }}>{children}</a>
    },
  }
}
