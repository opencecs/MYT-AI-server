import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { AndroidInfo } from '../../../types/index'
import * as SdkApiService from '../../../../bindings/github.com/moyunteng/myt/sdkapiservicewrapper'
import { useModalEnter } from '../../../hooks/useModalAnimation'
import { useAndroidNotes } from '../../../hooks/useNoteSync'
import { formatSize } from '../../../utils/format'

interface Props {
  android: AndroidInfo
  deviceIP: string
  onClose: () => void
}

/** 端口定义 */
const PORTS = [
  { containerPort: 5555, nameKey: 'host:diagnose.apiDetail.ports.adb', descKey: 'host:diagnose.apiDetail.ports.adbDesc', proto: 'TCP' },
  { containerPort: 9082, nameKey: 'host:diagnose.apiDetail.ports.mytos', descKey: 'host:diagnose.apiDetail.ports.mytosDesc', proto: 'TCP' },
  { containerPort: 9083, nameKey: 'host:diagnose.apiDetail.ports.rpa', descKey: 'host:diagnose.apiDetail.ports.rpaDesc', proto: 'TCP' },
  { containerPort: 10000, nameKey: 'host:diagnose.apiDetail.ports.videoStream', descKey: 'host:diagnose.apiDetail.ports.videoStreamDesc', proto: 'TCP' },
  { containerPort: 10001, nameKey: 'host:diagnose.apiDetail.ports.controlStream', descKey: 'host:diagnose.apiDetail.ports.controlStreamDesc', proto: 'TCP' },
  { containerPort: 10006, nameKey: 'host:diagnose.apiDetail.ports.cameraTcp', descKey: 'host:diagnose.apiDetail.ports.cameraTcpDesc', proto: 'TCP' },
  { containerPort: 10007, nameKey: 'host:diagnose.apiDetail.ports.cameraUdp', descKey: 'host:diagnose.apiDetail.ports.cameraUdpDesc', proto: 'UDP' },
  { containerPort: 10008, nameKey: 'host:diagnose.apiDetail.ports.webrtc', descKey: 'host:diagnose.apiDetail.ports.webrtcDesc', proto: 'TCP/UDP' },
]

/** 判断桥接模式：SDK 端已对 myt 网络容器统一做端口映射，只看网络名 */
function isBridged(android: AndroidInfo): boolean {
  const name = android.networkName?.toLowerCase() || ''
  return name.includes('myt')
}

/** 从 portBindings 查映射端口 */
function getMappedPort(android: AndroidInfo, containerPort: number): number | null {
  if (!android.portBindings) return null
  for (const proto of ['tcp', 'udp']) {
    const key = `${containerPort}/${proto}`
    const bindings = android.portBindings[key]
    if (bindings && bindings.length > 0) {
      const p = parseInt(bindings[0].HostPort)
      if (!isNaN(p)) return p
    }
  }
  return null
}

function ApiDetailModal({ android, deviceIP, onClose }: Props) {
  const rootRef = useRef<HTMLDivElement>(null)
  useModalEnter(rootRef)
  const { t } = useTranslation()
  const { notes } = useAndroidNotes()
  const bridged = isBridged(android)

  // 打开时主动拉取设备容器列表，按 id 匹配覆盖 CPU/内存/磁盘展示值。
  // 局部 state：不落缓存、不推事件——避免与 WS snapshot/stats 的权威覆盖打架
  //（WS 合并层以设备端推送为准，这里只是弹窗打开瞬间的展示刷新）。
  // 失败静默保持缓存值（设备离线/公网模式查询失败时展示值仍可用）。
  // 局限：diskUsage 是设备端 ~20min 缓存值，本弹窗的磁盘数字不会即时变化；
  // cpu/mem 拉取即新鲜，改善明显。
  const [usage, setUsage] = useState<Pick<AndroidInfo, 'cpuUsage' | 'memUsage' | 'diskUsage'> | null>(null)
  useEffect(() => {
    let cancelled = false
    SdkApiService.ListAndroids(deviceIP)
      .then(list => {
        if (cancelled) return
        const fresh = list?.find(a => a.id === android.id)
        if (fresh) {
          setUsage({ cpuUsage: fresh.cpuUsage, memUsage: fresh.memUsage, diskUsage: fresh.diskUsage })
        }
      })
      .catch(() => {}) // 静默：保持缓存展示值
    return () => { cancelled = true }
  }, [deviceIP, android.id])

  const cpuUsage = usage?.cpuUsage ?? android.cpuUsage
  const memUsage = usage?.memUsage ?? android.memUsage
  const diskUsage = usage?.diskUsage ?? android.diskUsage

  return (
    <div className="fixed inset-0 bg-black/30 flex items-center justify-center z-50 modal-overlay" ref={rootRef}>
      <div className="bg-white rounded-lg shadow-xl w-[680px] max-h-[80vh] flex flex-col modal-content" onClick={e => e.stopPropagation()}>
        <div className="px-5 py-3 border-b border-gray-100 flex justify-between">
          <div>
            <h3 className="text-sm font-medium text-gray-700">{t('host:diagnose.apiDetail.title', { name: android.containerIP || android.name })}</h3>
            <div className="flex items-center gap-2 mt-0.5">
              <span className={`text-[10px] px-1.5 py-0.5 rounded ${bridged ? 'bg-blue-50 text-blue-500' : 'bg-gray-50 text-gray-400'}`}>
                {bridged ? t('host:diagnose.apiDetail.bridgeMode') : t('host:diagnose.apiDetail.natMode')}
              </span>
              <span className="text-xs text-gray-400">{t('host:diagnose.apiDetail.slot', { index: android.indexNum })}</span>
              <span className="text-xs text-gray-400">·</span>
              <span className="text-xs text-gray-500">{notes[android.id] || android.name}</span>
            </div>
          </div>
          <button onClick={onClose} className="text-gray-400 hover:text-gray-600 text-lg leading-none">&times;</button>
        </div>

        <div className="flex-1 overflow-y-auto scrollbar-thin p-4">
          {/* 资源占用（running 容器才有数据）*/}
          <div className="mb-3 p-3 bg-gray-50 rounded-md">
            <div className="text-[11px] font-medium text-gray-500 mb-2">{t('host:diagnose.apiDetail.resourceUsage')}</div>
            {android.status === 'running' ? (
              <div className="grid grid-cols-3 gap-3">
                <ResourceItem label={t('host:diagnose.apiDetail.cpuUsage')} value={`${(cpuUsage ?? 0).toFixed(1)}%`} />
                <ResourceItem label={t('host:diagnose.apiDetail.memUsage')} value={formatSize(memUsage ?? 0)} />
                <ResourceItem label={t('host:diagnose.apiDetail.diskUsage')} value={formatSize(diskUsage ?? 0)} />
              </div>
            ) : (
              <div className="text-xs text-gray-400">{t('host:diagnose.apiDetail.notRunning')}</div>
            )}
          </div>

          <table className="w-full text-xs">
            <thead>
              <tr className="text-gray-400 border-b border-gray-100">
                <th className="text-left py-1.5 font-medium">{t('host:diagnose.apiDetail.colService')}</th>
                <th className="text-left py-1.5 font-medium">{t('host:diagnose.apiDetail.colDesc')}</th>
                <th className="text-left py-1.5 font-medium">{t('host:diagnose.apiDetail.colContainerPort')}</th>
                <th className="text-left py-1.5 font-medium">{t('host:diagnose.apiDetail.colAccessAddr')}</th>
              </tr>
            </thead>
            <tbody>
              {PORTS.map(p => {
                // 映射优先：SDK 对 myt 容器也做了端口映射，连接实际走设备 IP + 映射端口；
                // 容器 IP 仅作标识（弹窗标题已展示）。
                const mapped = getMappedPort(android, p.containerPort)
                let host: string
                let port: number | null
                let badge: 'mapped' | 'direct' | 'notMapped'
                if (mapped != null) {
                  host = deviceIP
                  port = mapped
                  badge = 'mapped'
                } else if (bridged) {
                  // 无映射的桥接兜底：容器有独立 IP，直接容器 IP + 容器端口访问
                  host = android.containerIP || android.ip
                  port = p.containerPort
                  badge = 'direct'
                } else {
                  // NAT 无映射：无法直连，显示占位
                  host = deviceIP
                  port = null
                  badge = 'notMapped'
                }
                const source = t(`host:diagnose.apiDetail.${badge}`)
                return (
                  <tr key={p.containerPort} className="border-b border-gray-50 hover:bg-gray-50/50">
                    <td className="py-1.5 font-medium text-gray-700">{t(p.nameKey)}</td>
                    <td className="py-1.5 text-gray-500">{t(p.descKey)}</td>
                    <td className="py-1.5 text-gray-400 font-mono">{p.containerPort}/{p.proto}</td>
                    <td className="py-1.5">
                      {port != null ? (
                        <span className="font-mono text-gray-700">{host}:{port}</span>
                      ) : (
                        <span className="text-gray-300">—</span>
                      )}
                      <span className={`ml-1 text-[9px] px-1 py-px rounded ${
                        badge === 'direct' ? 'bg-blue-50 text-blue-400' : badge === 'mapped' ? 'bg-green-50 text-green-400' : 'bg-gray-50 text-gray-400'
                      }`}>{source}</span>
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        </div>

        <div className="px-5 py-3 border-t border-gray-100 flex justify-between items-center">
          <div className="text-[10px] text-gray-400 space-y-0.5">
            <p>{t('host:diagnose.apiDetail.bridgeDesc')}</p>
            <p>{t('host:diagnose.apiDetail.natDesc')}</p>
          </div>
          <button onClick={onClose} className="px-4 py-1.5 text-sm bg-gray-100 text-gray-700 rounded-lg hover:bg-gray-200 shrink-0">{t('host:diagnose.apiDetail.close')}</button>
        </div>
      </div>
    </div>
  )
}

export default ApiDetailModal

function ResourceItem({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <div className="text-[10px] text-gray-400 mb-0.5">{label}</div>
      <div className="text-sm font-medium text-gray-700 font-mono">{value}</div>
    </div>
  )
}
