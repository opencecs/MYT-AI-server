import { useMemo, useRef } from 'react'
import { useTranslation } from 'react-i18next'
import { X, Info } from 'lucide-react'
import { useModalEnter } from '../../../../hooks/useModalAnimation'
import type { AndroidInfo, MirrorItem } from '../../../../types/index'
import { isContainerImage } from './modelSwitchUtils'
import { ContainerSection } from './BatchSwitchModelContainer'
import { EmulatorSection } from './BatchSwitchModelEmulator'

interface Props {
  selectedAndroids: AndroidInfo[]
  mirrors: MirrorItem[]
  onClose: () => void
  /** 单个云机切换成功后的回调（用于清截图等） */
  onSwitched?: (android: AndroidInfo) => void
}

function BatchSwitchModelModal({ selectedAndroids, mirrors, onClose, onSwitched }: Props) {
  const rootRef = useRef<HTMLDivElement>(null)
  useModalEnter(rootRef)
  const { t } = useTranslation()
  // 按镜像类型分组：容器 vs 模拟器（未知 sys_ver 按模拟器处理，走原 /android/switchModel）
  const { containerGroup, emulatorGroup } = useMemo(() => {
    const container: AndroidInfo[] = []
    const emulator: AndroidInfo[] = []
    for (const a of selectedAndroids) {
      if (isContainerImage(a.image, mirrors)) container.push(a)
      else emulator.push(a)
    }
    return { containerGroup: container, emulatorGroup: emulator }
  }, [selectedAndroids, mirrors])

  const hasContainer = containerGroup.length > 0
  const hasEmulator = emulatorGroup.length > 0
  const mixed = hasContainer && hasEmulator

  return (
    <div className="fixed inset-0 bg-black/30 flex items-center justify-center z-50 modal-overlay" ref={rootRef}>
      <div className="bg-white rounded-xl shadow-2xl w-[640px] max-h-[90vh] overflow-auto flex flex-col modal-content" onClick={e => e.stopPropagation()}>
        <div className="px-6 py-3 border-b border-gray-100 flex justify-between items-start">
          <div>
            <h2 className="text-base font-semibold text-gray-800">
              {selectedAndroids.length > 1 ? t('host:batchSwitchModel.batchTitle') : t('host:batchSwitchModel.singleTitle')}
            </h2>
            <p className="text-xs text-gray-400">
              {t('host:batchSwitchModel.selectedCount', { count: selectedAndroids.length })}
              {hasContainer && ` · ${t('host:batchSwitchModel.containerCount', { count: containerGroup.length })}`}
              {hasEmulator && ` · ${t('host:batchSwitchModel.emulatorCount', { count: emulatorGroup.length })}`}
            </p>
          </div>
          <button onClick={onClose} className="text-gray-400 hover:text-gray-600 hover:bg-gray-100 rounded-md p-1 transition-colors"><X size={18} /></button>
        </div>

        <div className="px-6 py-4 space-y-4">
          {mixed && (
            <div className="flex items-start gap-2 text-xs text-gray-500 bg-gray-50 border border-gray-100 rounded-md p-2.5">
              <Info size={14} className="text-gray-400 shrink-0 mt-0.5" />
              <span>{t('host:batchSwitchModel.mixedWarning')}</span>
            </div>
          )}
          {hasContainer && <ContainerSection androids={containerGroup} onSwitched={onSwitched} onClose={onClose} />}
          {hasEmulator && <EmulatorSection androids={emulatorGroup} mirrors={mirrors} onSwitched={onSwitched} onClose={onClose} />}
        </div>

        <div className="flex justify-end gap-2 px-6 py-3 border-t border-gray-100">
          <button onClick={onClose} className="h-9 px-4 text-sm rounded-md border border-gray-200 bg-white text-gray-600 hover:bg-gray-50 hover:border-gray-300 transition-colors">{t('host:batchSwitchModel.close')}</button>
        </div>
      </div>
    </div>
  )
}

export default BatchSwitchModelModal
