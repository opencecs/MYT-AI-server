import { useState, useRef, useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import * as SdkApiService from '../../../../../bindings/github.com/moyunteng/myt/sdkapiservicewrapper'
import { AndroidInfo } from '../../../../types/index'
import { getErrorMessage } from '../../../../utils/error'
import { showToast } from '../../../../components/Toast'
import ConfirmDialog from '../../../../components/ConfirmDialog'
import { useDeployMode } from '../../../../hooks/useDeployMode'
import { useAndroidNotes } from '../../../../hooks/useNoteSync'
import { getDeviceId } from '../../../../utils/deviceId'
import { useModalEnter } from '../../../../hooks/useModalAnimation'
import { cast } from '../../../../utils/cast'
import Select from '../../../../components/Select'

interface Props {
  android: AndroidInfo
  onClose: () => void
}

/**
 * 设备端 `GET /proxy` 的 data 段（MYT/docs/MYTOS-API接口文档.md「查询 S5 代理状态」）：
 * 已启动 `{ status:1, statusText:"已启动", addr:"socks5://user:pass@ip:port", type:2 }`；
 * 未启动 `{ status:0, statusText:"未启动" }`。老固件无此接口时按「状态未知」降级。
 */
interface ProxyStatusData {
  status?: number
  statusText?: string
  addr?: string
  type?: number
}

interface ProxyQueryResult {
  code?: number
  msg?: string
  data?: ProxyStatusData
}

/**
 * 解析代理整串（粘贴后填充下方字段）。支持四种写法：
 * - `socks5://user:pass@host:port`（`socks://` 亦可）
 * - `user:pass@host:port`
 * - `host:port`（无认证）
 * - `host:port:user:pass`（代理商常见格式，账号密码本身不得含冒号）
 * 不匹配时返回 null：静默忽略、不覆盖已填内容（老行为）。
 */
function parseProxyUrl(raw: string): { ip: string; port: string; usr: string; pwd: string } | null {
  const s = raw.trim()
  const m = s.match(/^(?:socks5?:\/\/)?(?:([^:@/]+):([^:@/]+)@)?([^:/@]+):(\d+)$/)
  if (m) return { usr: m[1] ?? '', pwd: m[2] ?? '', ip: m[3], port: m[4] }
  // host:port:user:pass（不含 @ 的四段写法；前一条正则的 host 段不容冒号，故两者互斥）
  const p = s.match(/^([^:@/]+):(\d+):([^:@/]+):([^:@/]+)$/)
  if (p) return { ip: p[1], port: p[2], usr: p[3], pwd: p[4] }
  return null
}

const inputCls = 'w-full bg-gray-50 text-sm px-3 py-1.5 rounded-lg border border-gray-200 focus:border-blue-400 focus:outline-none'
const labelCls = 'text-xs text-gray-500 mb-1 block'

/**
 * S5 代理设置：打开即查询设备端当前状态（`GET /proxy`），已启动则回显地址并预填表单；
 * 关闭代理也收在本弹窗内（二次确认），右键菜单不再单独出「关闭S5代理」入口。
 */
function S5ProxyModal({ android, onClose }: Props) {
  const { t } = useTranslation()
  const { mode } = useDeployMode()
  const { notes } = useAndroidNotes()
  const deviceIP = getDeviceId(android, mode)
  const [ip, setIp] = useState('')
  const [port, setPort] = useState('')
  const [usr, setUsr] = useState('')
  const [pwd, setPwd] = useState('')
  const [proxyType, setProxyType] = useState(1)
  const [loading, setLoading] = useState(false)
  // 查询结果：null = 未知/查询失败（与 querying 配合区分「查询中」与「状态未知」）
  const [status, setStatus] = useState<ProxyStatusData | null>(null)
  const [querying, setQuerying] = useState(true)
  const [askClose, setAskClose] = useState(false)
  const [closing, setClosing] = useState(false)

  const running = status?.status === 1

  // 打开即查一次状态：查询失败或老固件不支持时降级为「状态未知」，不阻塞设置
  useEffect(() => {
    let alive = true
    SdkApiService.QueryProxy(deviceIP, android.name)
      .then((raw) => {
        if (!alive) return
        const data = cast<ProxyQueryResult | null>(raw)?.data
        if (!data || typeof data.status !== 'number') return
        setStatus(data)
        // 已启动：回显当前代理配置（表单预填，便于在原参数上改）
        if (data.status === 1) {
          const parsed = data.addr ? parseProxyUrl(data.addr) : null
          if (parsed) { setIp(parsed.ip); setPort(parsed.port); setUsr(parsed.usr); setPwd(parsed.pwd) }
          if (data.type) setProxyType(data.type)
        }
      })
      .catch(() => {})
      .finally(() => { if (alive) setQuerying(false) })
    return () => { alive = false }
  }, [deviceIP, android.name])

  const handleApply = async () => {
    if (!ip || !port) { showToast(t('host:toast.s5ProxyFillFields'), 'error'); return }
    setLoading(true)
    try {
      await SdkApiService.StartProxy(deviceIP, android.name, ip, Number(port), usr, pwd, proxyType)
      showToast(t('host:toast.s5ProxySuccess'), 'success')
      onClose()
    } catch (err) {
      showToast(getErrorMessage(err), 'error')
    } finally {
      setLoading(false)
    }
  }

  // 关闭代理：成功后本弹窗不关闭，状态就地翻为「未启动」（用户可立刻改参数重设）
  const handleCloseProxy = async () => {
    setAskClose(false)
    setClosing(true)
    try {
      await SdkApiService.StopProxy(deviceIP, android.name)
      showToast(t('host:toast.s5ProxyCloseSuccess'), 'success')
      setStatus({ status: 0 })
    } catch (err) {
      showToast(getErrorMessage(err), 'error')
    } finally {
      setClosing(false)
    }
  }

  const rootRef = useRef<HTMLDivElement>(null)
  useModalEnter(rootRef)
  return (
    <>
      {/* 不做「点击弹窗外部关闭」：S5 表单填一半被误点遮罩关掉会丢状态，关闭只走 ×/取消按钮 */}
      <div className="fixed inset-0 bg-black/30 flex items-center justify-center z-50 modal-overlay" ref={rootRef}>
        <div className="bg-white rounded-lg shadow-xl w-[440px] flex flex-col modal-content">
          <div className="px-6 py-3 border-b border-gray-100 flex items-center justify-between">
            <div>
              <h2 className="text-base font-semibold text-gray-800">{t('host:s5Proxy.title')}</h2>
              <p className="text-xs text-gray-400">{notes[android.id] || android.name}</p>
            </div>
            <button onClick={onClose} className="text-gray-400 hover:text-gray-600 text-lg leading-none">&times;</button>
          </div>

          {/* 当前状态：查询中 / 已启动（含地址） / 未启动 / 状态未知 */}
          <div className="px-6 py-2 bg-gray-50 border-b border-gray-100 flex items-center gap-2 text-xs min-h-[34px]">
            {querying ? (
              <span className="text-gray-400">{t('host:s5Proxy.querying')}</span>
            ) : status === null ? (
              <span className="text-amber-600">{t('host:s5Proxy.statusUnknown')}</span>
            ) : (
              <>
                <span className={`w-1.5 h-1.5 rounded-full shrink-0 ${running ? 'bg-green-500' : 'bg-gray-300'}`} />
                <span className={`shrink-0 ${running ? 'text-green-700 font-medium' : 'text-gray-500'}`}>
                  {status.statusText || (running ? t('host:s5Proxy.statusRunning') : t('host:s5Proxy.statusStopped'))}
                </span>
                {running && status.addr && (
                  <span className="text-gray-500 font-mono truncate" title={status.addr}>{status.addr}</span>
                )}
              </>
            )}
          </div>

          <div className="px-6 py-4 space-y-3">
            <div>
              <label className={labelCls}>{t('host:s5Proxy.urlParse')} <span className="text-gray-300">{t('host:s5Proxy.urlParseHint')}</span></label>
              <input type="text" onChange={e => {
                const parsed = parseProxyUrl(e.target.value)
                if (parsed) { setIp(parsed.ip); setPort(parsed.port); setUsr(parsed.usr); setPwd(parsed.pwd) }
              }} placeholder={t('host:s5Proxy.urlParsePlaceholder')} className={inputCls} />
              {/* 支持的整串格式（写清楚边界：不匹配的写法不会填充下方字段，也不会报错） */}
              <p className="text-[11px] text-gray-400 mt-1">{t('host:s5Proxy.urlParseFormats')}</p>
            </div>
            <div className="flex gap-3">
              <div className="flex-1">
                <label className={labelCls}>{t('host:s5Proxy.serverIp')}</label>
                <input type="text" value={ip} onChange={e => setIp(e.target.value)} className={inputCls} />
              </div>
              <div className="w-24">
                <label className={labelCls}>{t('host:s5Proxy.port')}</label>
                <input type="number" value={port} onChange={e => setPort(e.target.value)} className={inputCls} />
              </div>
            </div>
            <div className="flex gap-3">
              <div className="flex-1">
                <label className={labelCls}>{t('host:s5Proxy.username')}</label>
                <input type="text" value={usr} onChange={e => setUsr(e.target.value)} className={inputCls} />
              </div>
              <div className="flex-1">
                <label className={labelCls}>{t('host:s5Proxy.password')}</label>
                <input type="text" value={pwd} onChange={e => setPwd(e.target.value)} className={inputCls} />
              </div>
            </div>
            <div>
              <label className={labelCls}>{t('host:s5Proxy.dnsMode')}</label>
              <Select value={String(proxyType)} onChange={v => setProxyType(Number(v))} options={[{ value: '1', label: t('host:s5Proxy.localDns') }, { value: '2', label: t('host:s5Proxy.remoteDns') }]} />
            </div>
          </div>

          <div className="flex items-center gap-2 px-6 py-3 border-t border-gray-100">
            {running && (
              <button onClick={() => setAskClose(true)} disabled={closing || loading}
                className="px-4 py-1.5 text-sm bg-white text-red-600 border border-red-200 rounded-lg hover:bg-red-50 disabled:opacity-50">
                {closing ? t('host:s5Proxy.closing') : t('host:s5Proxy.close')}
              </button>
            )}
            <div className="flex-1" />
            <button onClick={onClose} className="px-4 py-1.5 text-sm bg-gray-100 text-gray-700 rounded-lg hover:bg-gray-200">{t('button.cancel')}</button>
            <button onClick={handleApply} disabled={loading || !ip || !port}
              className="px-4 py-1.5 text-sm bg-blue-500 text-white rounded-lg hover:bg-blue-600 disabled:opacity-50">{loading ? t('host:s5Proxy.setting') : t('host:s5Proxy.apply')}</button>
          </div>
        </div>
      </div>

      {/* 与弹窗平级（不在其子树内）：ConfirmDialog 走 portal 渲染，同级可避免点击事件
          冒泡回弹窗外壳的 onClick 而把本弹窗一起关掉 */}
      {askClose && (
        <ConfirmDialog
          title={t('host:confirm.closeS5Proxy.title')}
          message={t('host:confirm.closeS5Proxy.message', { name: android.name })}
          confirmLabel={t('host:s5Proxy.close')}
          onConfirm={handleCloseProxy}
          onCancel={() => setAskClose(false)}
        />
      )}
    </>
  )
}

export default S5ProxyModal
