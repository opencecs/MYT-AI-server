import { useMemo, useState, useRef, useCallback } from 'react'
import { useTranslation } from 'react-i18next'
import * as DiscoverService from '../../../../../bindings/github.com/moyunteng/myt/discoverservicewrapper'
import { showToast } from '../../../../components/Toast'
import { useDeployMode } from '../../../../hooks/useDeployMode'

interface Chip {
  text: string
  valid: boolean
}

interface ManualResult {
  ok: 'success' | 'partial' | 'failed'
  addedCount: number
  failedIPs: string[]
}

const IP_RE = /^(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})$/
const isValidIP = (p: string) => {
  const m = p.match(IP_RE)
  return !!m && m.slice(1).every(n => +n <= 255)
}

/**
 * 手动添加 Tab：IP 芯片输入。
 * 粘贴/逗号/空格/换行自动拆分为芯片（去重、逐个可移除、无效项标红不计入），
 * 添加走 DiscoverDevicesManually（约 2s），无响应 IP 可一键重试。
 * v4 模式复用同一表单：后端 DiscoverDevicesManually 在 v4 下改为直接探 IP 的
 * 8088 /health（不走 UDP 单播、不做 v3 版本过滤），因此这里只覆盖文案，不改行为。
 */
function AddDeviceManualTab({ onClose }: { onClose: () => void }) {
  const { t } = useTranslation()
  const { mode } = useDeployMode()
  const isV4 = mode === 'v4'
  const [chips, setChips] = useState<Chip[]>([])
  const [buffer, setBuffer] = useState('')
  const [loading, setLoading] = useState(false)
  const [result, setResult] = useState<ManualResult | null>(null)
  const inputRef = useRef<HTMLInputElement>(null)

  const validCount = useMemo(() => chips.filter(c => c.valid).length, [chips])
  const invalidCount = chips.length - validCount

  const commitText = useCallback((str: string, prev: Chip[]): Chip[] => {
    const parts = str.split(/[\s,，]+/).map(s => s.trim()).filter(Boolean)
    const next = [...prev]
    for (const p of parts) {
      if (next.some(c => c.text === p)) continue // 去重，对齐后端 dedupStrings
      next.push({ text: p, valid: isValidIP(p) })
    }
    return next
  }, [])

  // 把输入框当前内容视作已提交后，可添加的合法 IP 数。
  // 用于「添加」按钮的禁用与计数：未回车的 buffer 也算数（点添加时自动入列）。
  const effectiveValidCount = useMemo(
    () => commitText(buffer, chips).filter(c => c.valid).length,
    [buffer, chips, commitText],
  )

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

  const runAdd = async (ips: string[]) => {
    if (ips.length === 0 || loading) return
    setLoading(true)
    setResult(null)
    try {
      const [devices, failedIPs] = await DiscoverService.DiscoverDevicesManually(ips.join(', '))
      const failed = failedIPs || []
      if (devices && devices.length > 0) {
        setResult({ ok: failed.length > 0 ? 'partial' : 'success', addedCount: devices.length, failedIPs: failed })
        if (failed.length === 0) {
          showToast(t('host:addDevice.addedSuccess', { count: devices.length }), 'success')
          setTimeout(onClose, 650)
        }
      } else {
        setResult({ ok: 'failed', addedCount: 0, failedIPs: failed })
      }
    } catch {
      setResult({ ok: 'failed', addedCount: 0, failedIPs: [] })
    } finally {
      setLoading(false)
    }
  }

  const retryFailed = () => result && runAdd(result.failedIPs)

  // 「添加」= 自动补一次回车：先把输入框未回车的内容入列（复用 commitText，
  // 自带拆分/去重/非法标记），再按「入列后全部合法 IP」走原有添加流程。
  // buffer 为空时 commitText 返回原 chips，行为不变。
  const handleAdd = () => {
    const committed = commitText(buffer, chips)
    if (buffer.trim()) {
      setChips(committed)
      setBuffer('')
    }
    runAdd(committed.filter(c => c.valid).map(c => c.text))
  }

  // 内嵌输入区宽度：有芯片 = 跟随内容（ch），空态 = 占满整行显示占位符
  const inputWidth = chips.length > 0 ? `${Math.max(buffer.length + 1, 2)}ch` : undefined

  return (
    <div>
      <label htmlFor="add-device-ip-input" className="block text-[13px] font-semibold text-gray-800 mb-1">
        {t('host:addDevice.deviceIpLabel')}
      </label>
      <p className="text-xs text-gray-400 leading-relaxed mb-2.5">
        {isV4 ? t('host:addDevice.v4.manualHint') : t('host:addDevice.manualHint')}
      </p>

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
              onClick={() => setChips(prev => prev.filter((_, idx) => idx !== i))}
            >
              <svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.4" strokeLinecap="round">
                <line x1="18" y1="6" x2="6" y2="18" /><line x1="6" y1="6" x2="18" y2="18" />
              </svg>
            </button>
          </span>
        ))}
        <input
          ref={inputRef}
          id="add-device-ip-input"
          className="add-chip-input"
          style={chips.length > 0 ? { width: inputWidth } : undefined}
          value={buffer}
          autoComplete="off"
          spellCheck={false}
          disabled={loading}
          placeholder={chips.length > 0 ? '' : t('host:addDevice.chipPlaceholder')}
          aria-label={t('host:addDevice.deviceIpLabel')}
          onKeyDown={handleKeyDown}
          onChange={e => handleInput(e.target.value)}
          onPaste={handlePaste}
        />
      </div>

      <p className={`text-xs mt-2 min-h-4 ${validCount > 0 && invalidCount === 0 ? 'text-blue-600' : 'text-gray-400'}`}>
        {chips.length === 0 ? '' : invalidCount === 0
          ? t('host:addDevice.identifiedIps', { count: validCount })
          : validCount === 0
            ? t('host:addDevice.invalidOnly', { count: invalidCount })
            : t('host:addDevice.identifiedWithInvalid', { valid: validCount, invalid: invalidCount })}
      </p>

      <div className="flex gap-2 mt-3">
        <button
          onClick={() => { setChips([]); setBuffer(''); setResult(null) }}
          disabled={loading || chips.length === 0}
          className="h-8.5 px-2.5 text-[13px] text-gray-400 hover:text-gray-600 hover:bg-gray-50 rounded-lg transition-colors disabled:opacity-50"
        >
          {t('host:addDevice.clear')}
        </button>
        <button
          onClick={handleAdd}
          disabled={loading || effectiveValidCount === 0}
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
            ? t('host:addDevice.querying')
            : effectiveValidCount > 0
              ? `${t('host:addDevice.addDevice')} (${effectiveValidCount})`
              : t('host:addDevice.addDevice')}
        </button>
      </div>

      {result && (
        <div className={`mt-3.5 rounded-lg px-3 py-2.5 text-xs leading-relaxed flex gap-2 items-start
          ${result.ok === 'success' ? 'bg-emerald-50 text-emerald-700' : result.ok === 'partial' ? 'bg-amber-50 text-amber-700' : 'bg-red-50 text-red-600'}`}>
          {result.ok === 'success' ? (
            <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round" className="shrink-0 mt-px">
              <polyline points="20 6 9 17 4 12" />
            </svg>
          ) : (
            <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" className="shrink-0 mt-px">
              <path d="M10.3 3.9 1.8 18a2 2 0 0 0 1.7 3h17a2 2 0 0 0 1.7-3L13.7 3.9a2 2 0 0 0-3.4 0z" /><line x1="12" y1="9" x2="12" y2="13" /><line x1="12" y1="17" x2="12.01" y2="17" />
            </svg>
          )}
          <span className="flex-1">
            {result.ok === 'failed'
              ? (result.failedIPs.length > 0
                  ? t('host:addDevice.noResponse', { ips: result.failedIPs.join(', ') })
                  : t('host:toast.noDeviceFound'))
              : result.ok === 'success'
                ? t('host:addDevice.addedSuccess', { count: result.addedCount })
                : <>
                    {t('host:addDevice.someNoResponse', { count: result.failedIPs.length, ips: result.failedIPs.join(', ') })}
                    <button onClick={retryFailed} className="font-semibold underline underline-offset-2 ml-1.5">
                      {t('host:addDevice.retryFailed')}
                    </button>
                  </>}
          </span>
        </div>
      )}
    </div>
  )
}

export default AddDeviceManualTab
