import { useState, useEffect, useRef, useCallback } from 'react'
import { gsap } from 'gsap'
import { useGSAP } from '@gsap/react'
import { MessageSquare, Loader2, Check, QrCode, RefreshCw, Unlink, Radio } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { agentC as c } from './agentTheme'
import { platformsApi, type PlatformItem } from './agentApi'

gsap.registerPlugin(useGSAP)

interface Props { onAlert: (msg: string) => void }

function PlatformConfigPage({ onAlert }: Props) {
  const { t } = useTranslation()
  const [platforms, setPlatforms] = useState<PlatformItem[]>([])
  const [loading, setLoading] = useState(true)
  const containerRef = useRef<HTMLDivElement>(null)

  const load = useCallback(async () => {
    setLoading(true)
    try { setPlatforms(await platformsApi.list()) } catch { /* */ }
    setLoading(false)
  }, [])

  useEffect(() => { load() }, [load])

  const handleDisconnect = async (name: string) => {
    try { await platformsApi.disconnect(name); await load() } catch (e) { onAlert(t('agent:platform.toast.disconnectFailed', { error: e })) }
  }

  useGSAP(() => {
    gsap.fromTo('.platform-card', { y: 16, opacity: 0 }, { y: 0, opacity: 1, duration: 0.35, stagger: 0.1, ease: 'power2.out' })
  }, { dependencies: [loading], scope: containerRef })

  return (
    <div ref={containerRef} className="h-full overflow-y-auto" style={{ background: c.bg }}>
      <div className="flex items-center gap-3 border-b px-5 py-3 sticky top-0 z-10" style={{ borderColor: c.line, background: c.surface }}>
        <Radio className="size-4" style={{ color: c.accent }} />
        <span className="text-sm font-semibold" style={{ color: c.text }}>{t('agent:platform.title')}</span>
        <button onClick={load} title={t('agent:tooltip.refresh')} className="ml-auto rounded-lg p-1.5 transition-colors hover:bg-black/5" style={{ color: c.textMuted }}>
          <RefreshCw className={`size-4 ${loading ? 'animate-spin' : ''}`} />
        </button>
      </div>
      <div className="p-5 grid grid-cols-2 gap-4 h-full content-start">
        {['feishu', 'wechat'].map(name => {
          const p = platforms.find(x => x.platform === name)
          return <PlatformCard key={name} name={name} platform={p} onChanged={load} onAlert={onAlert} onDisconnect={handleDisconnect} />
        })}
      </div>
    </div>
  )
}

function PlatformCard({ name, platform, onChanged, onAlert, onDisconnect }: {
  name: string
  platform?: PlatformItem
  onChanged: () => void
  onAlert: (msg: string) => void
  onDisconnect: (name: string) => void
}) {
  const { t } = useTranslation()
  const label = name === 'feishu' ? t('agent:platform.feishu') : t('agent:platform.wechat')
  const desc = name === 'feishu' ? t('agent:platform.feishuDesc') : t('agent:platform.wechatDesc')
  const [qrState, setQrState] = useState<'idle' | 'pending' | 'success' | 'expired'>('idle')
  const [qrUrl, setQrUrl] = useState('')
  const pollRef = useRef<ReturnType<typeof setInterval> | null>(null)
  const connected = platform?.enabled

  const createQR = useCallback(async () => {
    try {
      const res = await platformsApi.createQR(name)
      if (res.ok && res.qr_url) {
        setQrUrl(`https://api.qrserver.com/v1/create-qr-code/?size=200x200&data=${encodeURIComponent(res.qr_url)}`)
        setQrState('pending')
        startPolling(res.qr_id)
      }
    } catch (e) { onAlert(t('agent:platform.toast.qrFailed', { error: e })) }
  }, [name, onAlert])

  const startPolling = (id: string) => {
    if (pollRef.current) clearInterval(pollRef.current)
    pollRef.current = setInterval(async () => {
      try {
        const res = await platformsApi.pollQR(id)
        if (res.status === 'success') {
          setQrState('success')
          if (pollRef.current) clearInterval(pollRef.current)
          setTimeout(() => { setQrState('idle'); onChanged() }, 1500)
        } else if (res.status === 'expired') {
          setQrState('expired')
          if (pollRef.current) clearInterval(pollRef.current)
        }
      } catch { /* */ }
    }, 2000)
  }

  useEffect(() => () => { if (pollRef.current) clearInterval(pollRef.current) }, [])

  return (
    <div className="platform-card rounded-2xl border p-5 flex flex-col h-full" style={{ borderColor: c.line, background: c.surface }}>
      <div className="flex items-center gap-2.5 mb-2">
        <div className="flex size-9 items-center justify-center rounded-xl" style={{ background: connected ? `${c.success}12` : c.surfaceAlt }}>
          <MessageSquare className="size-4" style={{ color: connected ? c.success : c.textMuted }} />
        </div>
        <span className="text-sm font-semibold" style={{ color: c.text }}>{label}</span>
        {connected && <span className="flex items-center gap-1 text-[10px] px-2 py-0.5 rounded-full font-medium" style={{ background: `${c.success}12`, color: c.success }}><Check className="size-2.5" />{t('agent:platform.connected')}</span>}
      </div>
      <p className="text-xs mb-4 leading-relaxed" style={{ color: c.textMuted }}>{desc}</p>

      {connected ? (
        <div className="space-y-2.5 pt-2 border-t flex-1 flex flex-col justify-center" style={{ borderColor: c.line }}>
          {platform?.config && Object.entries(platform.config).filter(([k]) => !['webhook_secret', 'token'].includes(k)).map(([k, v]) => (
            <div key={k} className="flex items-center justify-between text-xs py-1">
              <span style={{ color: c.textMuted }}>{k}</span>
              <span className="truncate max-w-[200px] font-mono" style={{ color: c.text }} title={String(v)}>{String(v)}</span>
            </div>
          ))}
          <div className="mt-auto pt-2">
            <button onClick={() => onDisconnect(name)} className="flex items-center gap-1.5 rounded-lg px-3.5 py-2 text-xs transition-colors hover:bg-red-500/10" style={{ color: c.danger }}>
              <Unlink className="size-3.5" />{t('agent:platform.disconnect')}
            </button>
          </div>
        </div>
      ) : (
        <div className="pt-2 border-t flex-1 flex flex-col items-center justify-center gap-3" style={{ borderColor: c.line }}>
          {qrState === 'idle' && (
            <button onClick={createQR} className="flex items-center gap-2 rounded-xl px-4 py-2.5 text-xs text-white transition-opacity hover:opacity-90" style={{ background: c.accent }}>
              <QrCode className="size-4" />{t('agent:platform.scanConnect')}
            </button>
          )}
          {qrState === 'pending' && (
            <>
              <img src={qrUrl} alt="QR" className="w-44 h-44 rounded-xl" style={{ boxShadow: '0 4px 12px rgba(0,0,0,0.1)' }} />
              <div className="flex items-center gap-2 text-xs" style={{ color: c.textMuted }}>
                <Loader2 className="size-3.5 animate-spin" style={{ color: c.accent }} />{t('agent:platform.waitingScan')}
              </div>
              <button onClick={createQR} className="text-[10px] hover:underline" style={{ color: c.accent }}>{t('agent:platform.regenerate')}</button>
            </>
          )}
          {qrState === 'success' && (
            <div className="flex items-center gap-2 text-xs" style={{ color: c.success }}>
              <Check className="size-4" />{t('agent:platform.connectSuccess')}
            </div>
          )}
          {qrState === 'expired' && (
            <>
              <span className="text-xs" style={{ color: c.danger }}>{t('agent:platform.qrExpired')}</span>
              <button onClick={createQR} className="flex items-center gap-2 rounded-xl px-4 py-2.5 text-xs text-white transition-opacity hover:opacity-90" style={{ background: c.accent }}>
                <RefreshCw className="size-4" />{t('agent:platform.regenerate')}
              </button>
            </>
          )}
        </div>
      )}
    </div>
  )
}

export default PlatformConfigPage
