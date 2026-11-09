import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Pencil } from 'lucide-react'
import * as DiscoverService from '../../bindings/github.com/moyunteng/myt/discoverservicewrapper'
import { DeviceState } from '../types/index'
import { useModalEnter } from '../hooks/useModalAnimation'

interface Props {
  /** 目标设备（父组件条件挂载：device 存在才渲染，保证每次打开重置 state + 重播入场动画） */
  device: DeviceState
  /** 当前备注（父组件注入，挂载即初值——弹窗内部不异步加载，避免空框竞态） */
  note?: string
  onClose: () => void
}

/** 设备备注弹窗：右键菜单「设置备注」入口。保存经 devicenote:updated 事件同步全部页面。 */
function DeviceNoteModal({ device, note, onClose }: Props) {
  const { t } = useTranslation()
  const rootRef = useRef<HTMLDivElement>(null)
  const inputRef = useRef<HTMLInputElement>(null)
  const [value, setValue] = useState(note || '')
  const [saving, setSaving] = useState(false)

  useModalEnter(rootRef)

  // 挂载后全选（配合 autoFocus 便于直接覆盖输入）
  useEffect(() => {
    inputRef.current?.select()
  }, [])

  const save = async () => {
    const trimmed = value.trim()
    if (saving) return
    setSaving(true)
    try {
      await DiscoverService.SetDeviceNote(device.device_id, trimmed)
      onClose() // 保存成功：卸载弹窗（state 随之重置；备注经 devicenote:updated 事件同步）
    } catch {
      setSaving(false) // 失败留在弹窗内可重试
    }
  }

  return (
    <div
      ref={rootRef}
      className="fixed inset-0 bg-black/30 flex items-center justify-center z-50 modal-overlay"
      onClick={onClose}
      onKeyDown={e => { if (e.key === 'Escape') onClose() }}
    >
      <div className="bg-white rounded-lg shadow-xl w-[340px] modal-content" onClick={e => e.stopPropagation()}>
        <div className="flex items-center gap-2 px-4 py-3 border-b border-gray-100">
          <Pencil size={14} className="text-blue-500 shrink-0" />
          <h3 className="text-sm font-semibold text-gray-800">{t('groupcontrol:noteModal.title')}</h3>
          <button onClick={onClose} className="ml-auto text-gray-400 hover:text-gray-600 text-lg leading-none">&times;</button>
        </div>
        <div className="px-4 py-3">
          <div className="text-xs text-gray-400 bg-gray-50 rounded px-2 py-1.5 mb-3 font-mono truncate">{device.ip || device.device_id}</div>
          <input
            ref={inputRef}
            type="text"
            value={value}
            onChange={e => setValue(e.target.value)}
            onKeyDown={e => { if (e.key === 'Enter') save(); if (e.key === 'Escape') onClose() }}
            autoFocus
            maxLength={100}
            placeholder={t('groupcontrol:noteModal.placeholder')}
            className="w-full text-sm px-3 py-2 bg-gray-50 border border-gray-200 rounded-lg focus:border-blue-400 focus:outline-none focus:bg-white"
          />
        </div>
        <div className="px-4 py-3 border-t border-gray-100 flex justify-end gap-2">
          <button onClick={onClose} disabled={saving}
            className="px-4 py-1.5 text-xs text-gray-600 bg-gray-100 rounded-lg hover:bg-gray-200 transition-colors disabled:opacity-50">
            {t('button.cancel')}
          </button>
          <button onClick={save} disabled={saving}
            className="px-4 py-1.5 text-xs text-white bg-blue-500 rounded-lg hover:bg-blue-600 transition-colors disabled:opacity-50">
            {saving ? t('status.saving') : t('button.confirm')}
          </button>
        </div>
      </div>
    </div>
  )
}

export default DeviceNoteModal
