import { useState, useEffect, useRef } from 'react'
import * as SdkApiService from '../../bindings/github.com/moyunteng/myt/sdkapiservicewrapper'
import * as DeviceDetailService from '../../bindings/github.com/moyunteng/myt/devicedetailservicewrapper'
import { useTranslation } from 'react-i18next'
import { useModalEnter } from '../hooks/useModalAnimation'
import { getErrorMessage } from '../utils/error'

interface Props {
  deviceIP: string
  onClose: () => void
}

function formatMB(mb: number): string {
  if (mb >= 1024) return `${(mb / 1024).toFixed(1)} GB`
  return `${mb} MB`
}

/** 设备信息弹窗：右键菜单/维护页共用。打开/刷新时主动调 RefreshDeviceInfo 拉全量。 */
export default function DeviceInfoModal({ deviceIP, onClose }: Props) {
  const { t } = useTranslation()
  const rootRef = useRef<HTMLDivElement>(null)
  useModalEnter(rootRef)
  const [info, setInfo] = useState<Awaited<ReturnType<typeof SdkApiService.GetDeviceInfo>> | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')

  const loadInfo = async () => {
    setLoading(true)
    setError('')
    try {
      // 主动拉取：Go RefreshDeviceInfo——local 模式 REST /info/device 全量查询并落缓存，
      // 返回权威快照（含 hwaddr 等展示字段）；public/v2 模式 Go 侧已降级 SDK 网关查询。
      const result = await DeviceDetailService.RefreshDeviceInfo(deviceIP)
      setInfo(result)
    } catch (err) {
      // Wails 后端 error 的 message 是 JSON 字符串，String(err) 会带出
      // 'Error: {"message":...,"kind":"RuntimeError"}' 的原始外壳，用户看不懂。
      setError(getErrorMessage(err))
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    loadInfo()
  }, [deviceIP])

  const rows: { label: string; value: string }[] = []
  if (info) {
    // IP 优先取设备上报的真实 IP：v2 下传入的 deviceIP 是序列号，直接显示对用户无意义
    rows.push({ label: t('maintenance:info.deviceIP'), value: info.ip || deviceIP })
    rows.push({ label: t('maintenance:info.deviceId'), value: info.deviceId || '-' })
    rows.push({ label: t('maintenance:info.model'), value: info.model || '-' })
    rows.push({ label: t('maintenance:info.firmware'), value: info.version || '-' })
    rows.push({ label: t('maintenance:info.mac'), value: info.hwaddr || '-' })
    rows.push({ label: t('maintenance:info.cpuTemp'), value: info.cputemp ? `${info.cputemp}°C` : '-' })
    rows.push({ label: t('maintenance:info.cpuLoad'), value: info.cpuload || '-' })
    if (info.memtotal > 0) {
      rows.push({ label: t('maintenance:info.memTotal'), value: formatMB(info.memtotal) })
      rows.push({ label: t('maintenance:info.memUsed'), value: `${formatMB(info.memuse)} (${Math.round(info.memuse / info.memtotal * 100)}%)` })
    }
    if (info.mmctotal > 0) {
      rows.push({ label: t('maintenance:info.diskTotal'), value: formatMB(info.mmctotal) })
      rows.push({ label: t('maintenance:info.diskUsed'), value: `${formatMB(info.mmcuse)} (${Math.round(info.mmcuse / info.mmctotal * 100)}%)` })
    }
  }

  return (
    <div className="fixed inset-0 bg-black/30 flex items-center justify-center z-50 modal-overlay" ref={rootRef} onClick={onClose}>
      <div className="bg-white rounded-lg shadow-xl w-96 max-h-[80vh] overflow-y-auto modal-content" onClick={e => e.stopPropagation()}>
        <div className="flex items-center justify-between px-4 py-3 border-b border-gray-100">
          <h3 className="text-sm font-semibold text-gray-800">{t('maintenance:info.title', { ip: deviceIP })}</h3>
          <button onClick={onClose} className="text-gray-400 hover:text-gray-600 text-lg leading-none">&times;</button>
        </div>
        {loading && <div className="p-6 text-center text-xs text-gray-400">{t('status.loading')}</div>}
        {error && <div className="p-6 text-center text-xs text-red-500">{error}</div>}
        {info && !loading && (
          <div className="p-4 space-y-1.5">
            {rows.map((row, i) => (
              <div key={i} className="flex items-center justify-between text-xs px-2 py-1 rounded hover:bg-gray-50">
                <span className="text-gray-400">{row.label}</span>
                <span className="text-gray-700 font-mono">{row.value}</span>
              </div>
            ))}
          </div>
        )}
        <div className="px-4 py-3 border-t border-gray-100 flex justify-end">
          <button onClick={loadInfo} disabled={loading}
            className="px-3 py-1.5 text-xs text-gray-600 border border-gray-200 rounded-lg hover:bg-gray-50 disabled:opacity-40">
            {t('button.refresh')}
          </button>
        </div>
      </div>
    </div>
  )
}
