import { useState, useRef, useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import type { IscsiManager } from './useIscsiManager'
import { Spinner } from './TargetPanel'
import { useModalEnter } from '../../hooks/useModalAnimation'
import { useDraggable } from '../../hooks/useDraggable'

const STEPS = [
  { key: 'nic', label: 'steps.nic' },
  { key: 'mount', label: 'steps.mount' },
]

interface Props {
  mgr: IscsiManager
  occupiedDisks: Record<string, string>
  onClose: () => void
}

export function IscsiWizardModal({ mgr, occupiedDisks, onClose }: Props) {
  const { t } = useTranslation()
  const [step, setStep] = useState(0)
  const [mounting, setMounting] = useState(false)
  const [selectedIQN, setSelectedIQN] = useState('')
  const [selectedPortal, setSelectedPortal] = useState('')
  const [selectedLUN, setSelectedLUN] = useState(-1)
  const [autoInit, setAutoInit] = useState(true)

  // 进入发现与挂载步骤时自动触发发现
  useEffect(() => {
    if (step === 1 && mgr.targetIP) {
      mgr.discoverTargets()
    }
  }, [step])

  // 拖动逻辑抽到通用 useDraggable hook（与原实现行为一致：transform 偏移 + 手柄内交互元素守卫）
  const panelRef = useRef<HTMLDivElement>(null)
  const headerRef = useRef<HTMLDivElement>(null)
  const { startDrag } = useDraggable({ panelRef, handleRef: headerRef })

  const canNext = () => {
    switch (step) {
      case 0: return !!mgr.selectedLocalIP
      case 1: return !!selectedIQN && selectedLUN >= 0
      default: return true
    }
  }

  const handleNext = async () => {
    if (step === 1) {
      setMounting(true)
      try {
        // 提交挂载任务：成功后关闭向导（任务面板已打开，进度在那里看；页面状态由后台轮询自动更新）
        const ok = await mgr.mountIscsi(selectedIQN, selectedPortal || `${mgr.targetIP}:3260`, autoInit)
        if (ok) onClose()
      } finally {
        setMounting(false)
      }
      return
    }
    setStep(step + 1)
  }

  const rootRef = useRef<HTMLDivElement>(null)
  useModalEnter(rootRef)
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/30 modal-overlay" ref={rootRef}>
      <div ref={panelRef} className="bg-white rounded-xl shadow-xl w-[720px] max-w-[calc(100vw-32px)] max-h-[85vh] flex flex-col overflow-hidden modal-content">
        {/* 头部 - 可拖拽 */}
        <div ref={headerRef} className="flex items-center justify-between px-6 py-4 border-b border-gray-200 cursor-move select-none" onMouseDown={startDrag}>
          <div className="flex items-center gap-3">
            <h3 className="text-base font-semibold text-gray-800">{t('diskless:wizard.title')}</h3>
            <StorageModeBadge mode={mgr.storageMode} />
          </div>
          <button onClick={onClose} disabled={mounting} className="w-7 h-7 flex items-center justify-center rounded-md text-gray-400 hover:text-gray-600 hover:bg-gray-100 disabled:opacity-30 disabled:cursor-not-allowed cursor-pointer text-sm">x</button>
        </div>

        {/* 步骤条 */}
        <div className="px-6 py-3 border-b border-gray-100 flex items-center gap-1">
          {STEPS.map((s, i) => (
            <div key={s.key} className="flex items-center">
              <div className={`flex items-center gap-2 px-3 py-1.5 rounded-md text-xs transition-colors ${
                i === step ? 'bg-blue-50 text-blue-700 font-medium' : i < step ? 'text-blue-500' : 'text-gray-400'
              }`}>
                <span className={`w-5 h-5 rounded-full flex items-center justify-center text-[11px] ${
                  i < step ? 'bg-blue-500 text-white' : i === step ? 'bg-blue-500 text-white' : 'bg-gray-200 text-gray-500'
                }`}>
                  {i < step ? '✓' : i + 1}
                </span>
                <span>{t('diskless:wizard.' + s.label)}</span>
              </div>
              {i < STEPS.length - 1 && <div className={`w-6 h-px mx-1 ${i < step ? 'bg-blue-300' : 'bg-gray-200'}`} />}
            </div>
          ))}
        </div>

        {/* 内容区 */}
        <div className="flex-1 overflow-y-auto px-6 py-5">
          {step === 0 && <StepNic mgr={mgr} />}
          {step === 1 && (
            <StepDiscoverSelect
              mgr={mgr}
              occupiedDisks={occupiedDisks}
              selectedIQN={selectedIQN}
              selectedLUN={selectedLUN}
              onSelect={(iqn, portal, lun) => { setSelectedIQN(iqn); setSelectedPortal(portal); setSelectedLUN(lun) }}
              autoInit={autoInit}
              setAutoInit={setAutoInit}
            />
          )}
        </div>

        {/* 底部按钮 */}
        <div className="flex items-center justify-between px-6 py-4 border-t border-gray-200">
          <button onClick={() => setStep(Math.max(0, step - 1))} disabled={step === 0 || mounting} className="px-4 py-2 text-xs rounded-lg border border-gray-200 text-gray-600 hover:bg-gray-50 disabled:opacity-30 cursor-pointer">
            {t('button.back')}
          </button>
          <div className="flex items-center gap-2">
            <button onClick={onClose} className="px-4 py-2 text-xs rounded-lg border border-gray-200 text-gray-600 hover:bg-gray-50 cursor-pointer">
              {t('button.close')}
            </button>
            <button onClick={handleNext} disabled={!canNext() || mounting} className="px-5 py-2 text-xs rounded-lg bg-blue-500 text-white hover:bg-blue-600 disabled:opacity-50 cursor-pointer flex items-center gap-2">
              {mounting && <Spinner />}
              {step === 0 ? t('button.next') : mounting ? t('diskless:wizard.mounting') : t('diskless:wizard.mount')}
            </button>
          </div>
        </div>
      </div>
    </div>
  )
}

function StorageModeBadge({ mode }: { mode: string }) {
  const { t } = useTranslation()
  const isIscsi = mode === 'iscsi'
  return (
    <span className={`px-2 py-0.5 rounded-md text-[10px] font-medium ${isIscsi ? 'bg-green-100 text-green-700' : 'bg-gray-100 text-gray-500'}`}>
      {isIscsi ? t('diskless:wizard.disklessMode') : t('diskless:wizard.localStorage')}
    </span>
  )
}

// Step 0: 选择网卡
function StepNic({ mgr }: { mgr: IscsiManager }) {
  const { t } = useTranslation()
  return (
    <div className="space-y-4">
      <p className="text-xs text-gray-500">{t('diskless:wizard.stepNic.desc')}</p>
      <div className="grid grid-cols-2 gap-3">
        {mgr.localNics.map(n => {
          const selected = mgr.selectedLocalIP === n.ip
          return (
            <div key={`${n.name}-${n.ip}`} onClick={() => mgr.setSelectedLocalIP(n.ip)} className={`rounded-lg border-2 p-3 cursor-pointer transition-all ${selected ? 'border-blue-500 bg-blue-50/50' : 'border-gray-200 hover:border-gray-300 bg-white'}`}>
              <div className="flex items-center gap-2 mb-2">
                <div className={`w-4 h-4 rounded-full border-2 flex items-center justify-center ${selected ? 'border-blue-500' : 'border-gray-300'}`}>
                  {selected && <div className="w-2 h-2 rounded-full bg-blue-500" />}
                </div>
                <span className="text-sm font-medium text-gray-700">{n.name}</span>
              </div>
              <div className="ml-6 space-y-1">
                <div className="text-xs text-gray-600 font-mono">{n.ip}</div>
                <div className="text-[10px] text-gray-400 font-mono">{n.mac}</div>
              </div>
            </div>
          )
        })}
      </div>
      {mgr.selectedLocalIP && (
        <div className="text-xs text-gray-500 bg-blue-50 rounded-lg px-4 py-2.5">
          {t('diskless:wizard.stepNic.serviceAddr')} <span className="font-mono text-blue-700 font-medium">{mgr.selectedLocalIP}</span>
        </div>
      )}
    </div>
  )
}

// Step 2: 发现与挂载
function StepDiscoverSelect({ mgr, occupiedDisks, selectedIQN, selectedLUN, onSelect, autoInit, setAutoInit }: {
  mgr: IscsiManager
  occupiedDisks: Record<string, string>
  selectedIQN: string
  selectedLUN: number
  onSelect: (iqn: string, portal: string, lun: number) => void
  autoInit: boolean
  setAutoInit: (v: boolean) => void
}) {
  const { t } = useTranslation()
  return (
    <div className="space-y-5">
      <p className="text-xs text-gray-500">{t('diskless:wizard.stepDiscover.desc')}</p>

      <div className="flex items-center gap-3">
        <label className="text-xs text-gray-500 w-20 shrink-0">{t('diskless:wizard.stepDiscover.serviceAddr')}</label>
        <input value={mgr.targetIP} onChange={e => mgr.setTargetIP(e.target.value)} className="flex-1 text-xs border border-gray-200 rounded-lg px-3 py-2 focus:outline-none focus:border-blue-400" />
      </div>

      <label className="flex items-center gap-2 cursor-pointer">
        <input type="checkbox" checked={autoInit} onChange={e => setAutoInit(e.target.checked)} className="w-3.5 h-3.5 rounded border-gray-300 text-blue-500" />
        <span className="text-xs text-gray-600">{t('diskless:wizard.stepDiscover.autoInit')}</span>
      </label>

      {/* 磁盘列表 - 卡片式，per-disk IQN 判断占用 */}
      {mgr.diskImages.filter(d => d.iqn).length > 0 && (
        <div className="space-y-2">
          <div className="text-xs font-medium text-gray-500">{t('diskless:wizard.stepDiscover.availableDisks')}</div>
          <div className="space-y-2">
            {mgr.diskImages.filter(d => d.iqn).map(d => {
              const selected = selectedLUN === d.lun
              const occupiedBy = occupiedDisks[d.iqn]
              // 当前设备已挂载此磁盘
              const isSelfMounted = mgr.sessions.some(s => s.iqn === d.iqn)
              // 其他设备占用
              const isOtherOccupied = !isSelfMounted && !!occupiedBy
              const isOccupied = isSelfMounted || isOtherOccupied
              return (
                <div key={`${d.name}-${d.lun}`} onClick={() => !isOccupied && onSelect(d.iqn, `${mgr.targetIP}:3260`, d.lun)} className={`rounded-lg border-2 p-3 transition-all flex items-center justify-between ${isOccupied ? 'border-gray-100 bg-gray-50 cursor-not-allowed opacity-50' : selected ? 'border-blue-500 bg-blue-50/50 cursor-pointer' : 'border-gray-200 hover:border-gray-300 cursor-pointer'}`}>
                  <div className="flex items-center gap-3">
                    <div className={`w-8 h-8 rounded-lg flex items-center justify-center text-xs font-bold ${isOccupied ? 'bg-gray-100 text-gray-300' : selected ? 'bg-blue-500 text-white' : 'bg-gray-100 text-gray-500'}`}>{d.lun}</div>
                    <div>
                      <div className={`text-sm font-medium ${isOccupied ? 'text-gray-400' : 'text-gray-700'}`}>{d.name}</div>
                      <div className="text-xs text-gray-400">{d.size_gb % 1 ? d.size_gb.toFixed(1) : d.size_gb} GB{isSelfMounted ? ' · ' + t('diskless:wizard.stepDiscover.mounted') : isOtherOccupied ? ' · ' + t('diskless:wizard.stepDiscover.inUseBy', { device: occupiedBy }) : ''}</div>
                    </div>
                  </div>
                  <div className={`w-5 h-5 rounded-full border-2 flex items-center justify-center ${isOccupied ? 'border-gray-200' : selected ? 'border-blue-500' : 'border-gray-300'}`}>
                    {selected && !isOccupied && <div className="w-2.5 h-2.5 rounded-full bg-blue-500" />}
                  </div>
                </div>
              )
            })}
          </div>
        </div>
      )}

      {/* Target 列表（无磁盘时） */}
      {mgr.targets.length > 0 && mgr.diskImages.filter(d => d.iqn).length === 0 && (
        <div className="space-y-2">
          <div className="text-xs font-medium text-gray-500">{t('diskless:wizard.stepDiscover.discoveredTargets')}</div>
          <div className="space-y-2">
            {mgr.targets.map(tgt => {
              const selected = selectedIQN === tgt.iqn
              const occupiedBy = occupiedDisks[tgt.iqn]
              const isSelfMounted = mgr.sessions.some(s => s.iqn === tgt.iqn)
              const isOtherOccupied = !isSelfMounted && !!occupiedBy
              const isOccupied = isSelfMounted || isOtherOccupied
              return (
                <div key={tgt.iqn} onClick={() => !isOccupied && onSelect(tgt.iqn, tgt.portal, -1)} className={`rounded-lg border-2 p-3 cursor-pointer transition-all flex items-center justify-between ${isOccupied ? 'border-gray-100 opacity-50 cursor-not-allowed' : selected ? 'border-blue-500 bg-blue-50/50' : 'border-gray-200 hover:border-gray-300'}`}>
                  <div>
                    <div className="text-xs text-gray-700 font-mono">{tgt.iqn}</div>
                    <div className="text-xs text-gray-400 mt-0.5">{tgt.portal}{isSelfMounted ? ' · ' + t('diskless:wizard.stepDiscover.mounted') : isOtherOccupied ? ' · ' + t('diskless:wizard.stepDiscover.inUseBy', { device: occupiedBy }) : ''}</div>
                  </div>
                  <div className={`w-5 h-5 rounded-full border-2 flex items-center justify-center ${isOccupied ? 'border-gray-200' : selected ? 'border-blue-500' : 'border-gray-300'}`}>
                    {selected && !isOccupied && <div className="w-2.5 h-2.5 rounded-full bg-blue-500" />}
                  </div>
                </div>
              )
            })}
          </div>
        </div>
      )}
    </div>
  )
}
