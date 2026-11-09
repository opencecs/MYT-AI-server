import { useMemo, useState, useRef, useCallback } from 'react'
import { useTranslation } from 'react-i18next'
import * as V2Service from '../../../../../bindings/github.com/moyunteng/myt/v2servicewrapper'
import { showToast } from '../../../../components/Toast'

interface Chip {
  text: string
  valid: boolean
}

/** ProbeV2Hosts 返回的单台探测结果（与 Go v2host.ProbeResult 对齐） */
interface ProbeResult {
  ip: string
  ok: boolean
  hostId?: string
  hostName?: string
  version?: string
  containers?: number
  err?: string
  superSdkReady?: boolean
  sdkErr?: string
}

/** AddV2Hosts 返回结果（与 Go V2AddHostsResult 对齐） */
interface AddResult {
  added?: ProbeResult[]
  exists?: ProbeResult[]
  failed?: ProbeResult[]
}

const IP_RE = /^(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})$/
const isValidIP = (p: string) => {
  const m = p.match(IP_RE)
  return !!m && m.slice(1).every(n => +n <= 255)
}

/**
 * V2 添加主机 Tab（v2 模式专用）：IP 芯片输入 → 探测（2375 准入 + 81 就绪度）→ 确认添加。
 * 准入放宽语义：2375 Docker daemon 可达即通过（81 缺失不拦截），81 探测结果
 * 只用于标注「未装超级 SDK」——设备入列后在详情处再引导安装。
 * 两步流程：先探测预览（主机名/云机数/失败原因），再确认写入设备列表。
 * 添加成功后主机异步拉容器列表，云机 tab 经 androidlist:updated 自动刷新。
 */
function AddDeviceV2Tab({ onClose }: { onClose: () => void }) {
  const { t } = useTranslation()
  const [chips, setChips] = useState<Chip[]>([])
  const [buffer, setBuffer] = useState('')
  const [loading, setLoading] = useState(false)
  const [probed, setProbed] = useState<ProbeResult[] | null>(null)
  const [addResult, setAddResult] = useState<AddResult | null>(null)
  const inputRef = useRef<HTMLInputElement>(null)

  const validCount = useMemo(() => chips.filter(c => c.valid).length, [chips])
  const okProbes = useMemo(() => (probed ?? []).filter(p => p.ok), [probed])
  const failProbes = useMemo(() => (probed ?? []).filter(p => !p.ok), [probed])

  const commitText = useCallback((str: string, prev: Chip[]): Chip[] => {
    const parts = str.split(/[\s,，]+/).map(s => s.trim()).filter(Boolean)
    const next = [...prev]
    for (const p of parts) {
      if (next.some(c => c.text === p)) continue
      next.push({ text: p, valid: isValidIP(p) })
    }
    return next
  }, [])

  const handleKeyDown = (e: React.KeyboardEvent<HTMLInputElement>) => {
    if (e.key === 'Enter') {
      e.preventDefault()
      setChips(prev => commitText(buffer, prev))
      setBuffer('')
    } else if (e.key === 'Backspace' && buffer === '' && chips.length > 0) {
      setChips(prev => prev.slice(0, -1))
    }
  }

  const handleInput = (v: string) => {
    if (/[\s,，]/.test(v)) {
      const segs = v.split(/[\s,，]+/)
      const rest = segs.pop() ?? ''
      setChips(prev => commitText(segs.join(' '), prev))
      setBuffer(rest)
    } else {
      setBuffer(v)
    }
  }

  const handlePaste = (e: React.ClipboardEvent<HTMLInputElement>) => {
    e.preventDefault()
    const text = e.clipboardData.getData('text')
    setChips(prev => commitText(buffer + ' ' + text, prev))
    setBuffer('')
  }

  // 输入变化后作废上一次探测结果（芯片与结果不再对应）
  const invalidateProbe = () => { setProbed(null); setAddResult(null) }

  const runProbe = async () => {
    const ips = chips.filter(c => c.valid).map(c => c.text)
    if (ips.length === 0 || loading) return
    setLoading(true)
    setAddResult(null)
    try {
      const results = await V2Service.ProbeV2Hosts(ips.join(', '))
      setProbed(results || [])
    } catch {
      setProbed([])
    } finally {
      setLoading(false)
    }
  }

  const runAdd = async () => {
    if (okProbes.length === 0 || loading) return
    setLoading(true)
    try {
      const result = await V2Service.AddV2Hosts(okProbes.map(p => p.ip).join(', '))
      setAddResult(result || {})
      const added = result?.added?.length ?? 0
      const failed = result?.failed?.length ?? 0
      if (added > 0 && failed === 0) {
        showToast(t('host:addDevice.v2.addedToast', { count: added }), 'success')
        setTimeout(onClose, 650)
      }
    } catch {
      setAddResult({ failed: okProbes })
    } finally {
      setLoading(false)
    }
  }

  const inputWidth = chips.length > 0 ? `${Math.max(buffer.length + 1, 2)}ch` : undefined

  return (
    <div>
      <label htmlFor="add-v2-host-ip-input" className="block text-[13px] font-semibold text-gray-800 mb-1">
        {t('host:addDevice.v2.hostIpLabel')}
      </label>
      <p className="text-xs text-gray-400 leading-relaxed mb-2.5">{t('host:addDevice.v2.hint')}</p>

      <div
        onClick={e => { if (e.target === e.currentTarget) inputRef.current?.focus() }}
        className={`add-chipbox ${chips.length > 0 ? 'add-chipbox--has-chips' : ''}`}
      >
        {chips.map((chip, i) => (
          <span key={chip.text} className={`add-chip ${chip.valid ? '' : 'add-chip--invalid'}`} title={chip.valid ? undefined : t('host:addDevice.invalidIp')}>
            <span className="add-chip__text">{chip.text}</span>
            <button
              type="button"
              aria-label={`${t('host:addDevice.removeChip')} ${chip.text}`}
              disabled={loading}
              onClick={() => { setChips(prev => prev.filter((_, idx) => idx !== i)); invalidateProbe() }}
            >
              <svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.4" strokeLinecap="round">
                <line x1="18" y1="6" x2="6" y2="18" /><line x1="6" y1="6" x2="18" y2="18" />
              </svg>
            </button>
          </span>
        ))}
        <input
          ref={inputRef}
          id="add-v2-host-ip-input"
          className="add-chip-input"
          style={chips.length > 0 ? { width: inputWidth } : undefined}
          value={buffer}
          autoComplete="off"
          spellCheck={false}
          disabled={loading}
          placeholder={chips.length > 0 ? '' : t('host:addDevice.chipPlaceholder')}
          aria-label={t('host:addDevice.v2.hostIpLabel')}
          onKeyDown={e => { handleKeyDown(e); if (e.key === 'Enter') invalidateProbe() }}
          onChange={e => { handleInput(e.target.value); invalidateProbe() }}
          onPaste={handlePaste}
        />
      </div>

      {/* 探测结果预览 */}
      {probed !== null && (
        <div className="mt-3 rounded-lg border border-gray-100 max-h-44 overflow-y-auto">
          {probed.length === 0 ? (
            <p className="px-3 py-2.5 text-xs text-red-600">{t('host:addDevice.v2.probeEmpty')}</p>
          ) : (
            probed.map(p => (
              <div key={p.ip} className="flex items-center gap-2 px-3 py-1.5 text-xs border-b border-gray-50 last:border-b-0">
                {p.ok ? (
                  <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.4" strokeLinecap="round" strokeLinejoin="round" className="shrink-0 text-emerald-500">
                    <polyline points="20 6 9 17 4 12" />
                  </svg>
                ) : (
                  <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" className="shrink-0 text-red-400">
                    <line x1="18" y1="6" x2="6" y2="18" /><line x1="6" y1="6" x2="18" y2="18" />
                  </svg>
                )}
                <span className="font-mono text-gray-700 w-32 shrink-0">{p.ip}</span>
                {p.ok ? (
                  <span className="text-gray-500 truncate">
                    {p.hostName || p.hostId?.slice(0, 12)} · {t('host:addDevice.v2.containerCount', { count: p.containers ?? 0 })}
                    {!p.superSdkReady && (
                      <span className="ml-1.5 text-amber-600" title={p.sdkErr}>{t('host:superSdk.notInstalled')}</span>
                    )}
                  </span>
                ) : (
                  <span className="text-red-400 truncate" title={p.err}>{p.err || t('host:addDevice.v2.probeFail')}</span>
                )}
              </div>
            ))
          )}
        </div>
      )}

      <div className="flex gap-2 mt-3">
        <button
          onClick={() => { setChips([]); setBuffer(''); setProbed(null); setAddResult(null) }}
          disabled={loading || chips.length === 0}
          className="h-8.5 px-2.5 text-[13px] text-gray-400 hover:text-gray-600 hover:bg-gray-50 rounded-lg transition-colors disabled:opacity-50"
        >
          {t('host:addDevice.clear')}
        </button>
        {probed === null || okProbes.length === 0 ? (
          <button
            onClick={runProbe}
            disabled={loading || validCount === 0}
            className="flex-1 h-9 rounded-lg bg-blue-600 text-white text-sm font-medium
              hover:bg-blue-700 active:translate-y-px transition-all disabled:opacity-40 disabled:cursor-not-allowed
              inline-flex items-center justify-center gap-2"
          >
            {loading && (
              <svg className="animate-spin h-3.5 w-3.5" viewBox="0 0 24 24" fill="none">
                <circle cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="3" opacity="0.25" />
                <path d="M12 2a10 10 0 0 1 10 10" stroke="currentColor" strokeWidth="3" strokeLinecap="round" />
              </svg>
            )}
            {loading
              ? t('host:addDevice.v2.probing')
              : validCount > 0
                ? `${t('host:addDevice.v2.probe')} (${validCount})`
                : t('host:addDevice.v2.probe')}
          </button>
        ) : (
          <button
            onClick={runAdd}
            disabled={loading || okProbes.length === 0}
            className="flex-1 h-9 rounded-lg bg-blue-600 text-white text-sm font-medium
              hover:bg-blue-700 active:translate-y-px transition-all disabled:opacity-40 disabled:cursor-not-allowed"
          >
            {loading
              ? t('host:addDevice.querying')
              : t('host:addDevice.v2.confirmAdd', { count: okProbes.length })}
          </button>
        )}
      </div>

      {/* 添加结果 */}
      {addResult && (
        <div className="mt-3.5 rounded-lg px-3 py-2.5 text-xs leading-relaxed bg-amber-50 text-amber-700">
          {t('host:addDevice.v2.addResult', {
            added: addResult.added?.length ?? 0,
            exists: addResult.exists?.length ?? 0,
            failed: addResult.failed?.length ?? 0,
          })}
          {(addResult.failed?.length ?? 0) > 0 && (
            <span className="block mt-1 text-red-500">
              {t('host:addDevice.v2.failedIps', { ips: (addResult.failed ?? []).map(p => p.ip).join(', ') })}
            </span>
          )}
          {/* 准入通过但 81 缺失：已入列，提示去设备详情安装超级 SDK */}
          {(addResult.added ?? []).concat(addResult.exists ?? []).filter(p => p.ok && !p.superSdkReady).length > 0 && (
            <span className="block mt-1 text-amber-700 font-medium">
              {t('host:superSdk.addedNeedSdk', {
                ips: (addResult.added ?? []).concat(addResult.exists ?? []).filter(p => p.ok && !p.superSdkReady).map(p => p.ip).join(', '),
              })}
            </span>
          )}
        </div>
      )}
    </div>
  )
}

export default AddDeviceV2Tab
