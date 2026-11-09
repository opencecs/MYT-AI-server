import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useDeployMode } from '../../../../../hooks/useDeployMode'
import { useAndroidNotes } from '../../../../../hooks/useNoteSync'
import { getDeviceId } from '../../../../../utils/deviceId'
import { SdkApiService, OpBlock, inputCls, btnCls, btnSecondaryCls, preCls, showToast, getErrorMessage, isContainerImage, AndroidInfo, MirrorItem } from '../shared'

interface Props {
  androids: AndroidInfo[]
  mirrors: MirrorItem[]
}

interface AndroidInfoData {
  buildTime?: string
  hostIp?: string
  instance?: string
  ipVerion?: string
  modelVersion?: string
  name?: string
}

const fieldLabels: Record<string, string> = {
  buildTime: '编译时间',
  hostIp: '宿主机 IP',
  instance: '实例号',
  ipVerion: 'IP 版本',
  modelVersion: '机型版本',
  name: '实例名称',
}

function formatBuildTime(ts: string): string {
  const n = Number(ts)
  if (!n) return ts
  return new Date(n * 1000).toLocaleString('zh-CN', { hour12: false })
}

export default function InfoTab({ androids, mirrors }: Props) {
  const { t } = useTranslation()
  const { notes } = useAndroidNotes()
  const { mode } = useDeployMode()
  const primary = androids.find(a => isContainerImage(a.image, mirrors)) ?? androids[0]
  const isBatch = androids.length > 1
  const [allInfo, setAllInfo] = useState<Array<{ name: string; data: AndroidInfoData | null; error?: string }> | null>(null)
  const [version, setVersion] = useState('')
  const [loading, setLoading] = useState(false)

  const fetchInfo = async () => {
    setLoading(true)
    setVersion('')
    try {
      if (isBatch) {
        const results = await Promise.all(androids.map(async a => {
          try {
            const resp = await SdkApiService.GetAndroidInfo(getDeviceId(a, mode), a.name) as Record<string, unknown>
            const data = (resp?.data as AndroidInfoData) || null
            return { name: a.name, data }
          } catch (err) {
            return { name: a.name, data: null, error: getErrorMessage(err) }
          }
        }))
        setAllInfo(results)
      } else {
        const resp = await SdkApiService.GetAndroidInfo(getDeviceId(primary, mode), primary.name) as Record<string, unknown>
        const data = (resp?.data as AndroidInfoData) || null
        setAllInfo([{ name: primary.name, data }])
      }
    } catch (err) {
      showToast(getErrorMessage(err), 'error')
    } finally { setLoading(false) }
  }

  const fetchVersion = async () => {
    setLoading(true)
    setAllInfo(null)
    try {
      const v = await SdkApiService.GetAndroidVersion(getDeviceId(primary, mode), primary.name)
      setVersion(v)
    } catch (err) {
      showToast(getErrorMessage(err), 'error')
    } finally { setLoading(false) }
  }

  return (
    <div className="space-y-3">
      {isBatch && (
        <div className="text-xs text-gray-500 bg-gray-50 px-3 py-2 rounded">
          {t('host:moreOpsTab.batchFirstOnly')} <span className="font-medium text-gray-700">{notes[primary.id] || primary.name}</span>
        </div>
      )}
      <OpBlock label={t('host:moreOpsTab.info.machineInfo')} hint="/info · /queryversion">
        <div className="flex gap-2">
          <button onClick={fetchInfo} disabled={loading} className={btnCls}>{t('host:moreOpsTab.info.getInfo')}</button>
          <button onClick={fetchVersion} disabled={loading} className={btnSecondaryCls}>{t('host:moreOpsTab.info.getVersion')}</button>
        </div>
        {version && <div className={`${inputCls} bg-white`}>{version}</div>}
        {allInfo && allInfo.map((item, idx) => (
          <div key={idx} className="rounded-lg border border-gray-100 p-2.5 space-y-1">
            <div className="text-xs font-medium text-gray-700">{item.name}</div>
            {item.error ? (
              <div className="text-[11px] text-red-500">{item.error}</div>
            ) : item.data ? (
              <div className="grid grid-cols-2 gap-x-3 gap-y-1">
                {Object.entries(fieldLabels).map(([key, label]) => {
                  const raw = (item.data as Record<string, unknown>)[key]
                  if (raw == null || raw === '') return null
                  const val = key === 'buildTime' ? formatBuildTime(String(raw)) : String(raw)
                  return (
                    <div key={key} className="flex items-baseline gap-1">
                      <span className="text-[11px] text-gray-400 shrink-0">{label}</span>
                      <span className="text-[11px] text-gray-700 truncate">{val}</span>
                    </div>
                  )
                })}
              </div>
            ) : (
              <div className="text-[11px] text-gray-400">无数据</div>
            )}
          </div>
        ))}
      </OpBlock>
    </div>
  )
}
