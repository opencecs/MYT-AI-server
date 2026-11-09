import { useState, useEffect, useRef } from 'react'
import * as PaymentService from '../../../bindings/github.com/moyunteng/myt/paymentservicewrapper'
import { PackageItem, PayMethod } from './types'
import { useModalEnter } from '../../hooks/useModalAnimation'
import { useTranslation } from 'react-i18next'

interface SelectedSlot {
  deviceID: string
  slotIndex: number
}

interface Props {
  packages: PackageItem[]
  selectedSlots: SelectedSlot[]
  payMethod: PayMethod
  loading: boolean
  onPayMethodChange: (m: PayMethod) => void
  onBuy: (pkg: PackageItem) => void
  onRemoveSlot: (idx: number) => void
  onClose: () => void
}

function PackageModal({ packages, selectedSlots, payMethod, loading, onPayMethodChange, onBuy, onRemoveSlot, onClose }: Props) {
  const { t } = useTranslation()
  const rootRef = useRef<HTMLDivElement>(null)
  useModalEnter(rootRef)
  const [userScore, setUserScore] = useState(0)
  const [scoreLoading, setScoreLoading] = useState(false)

  useEffect(() => {
    const fetchScore = async () => {
      setScoreLoading(true)
      try {
        const score = await PaymentService.GetUserAtt() as number
        setUserScore(score || 0)
      } catch {}
      setScoreLoading(false)
    }
    fetchScore()
  }, [])

  return (
    <div className="fixed inset-0 bg-black/30 flex items-center justify-center z-50 modal-overlay" ref={rootRef}>
      <div className="bg-white rounded-lg shadow-xl w-[560px] max-h-[80vh] flex flex-col modal-content" onClick={e => e.stopPropagation()}>
        <div className="px-5 py-3 border-b border-gray-100 flex items-center justify-between">
          <h3 className="text-sm font-semibold text-gray-800">{t('payment:pkg.title')}</h3>
          <button onClick={onClose} className="text-gray-400 hover:text-gray-600 text-lg leading-none">&times;</button>
        </div>

        <div className="flex-1 p-5 space-y-4 overflow-y-auto">
          {/* 已选实例 */}
          {selectedSlots.length > 0 && (
            <div>
              <div className="text-xs text-gray-500 mb-2">{t('payment:pkg.selectedInstances', { count: selectedSlots.length })}</div>
              <div className="flex flex-wrap gap-1.5">
                {selectedSlots.map((s, i) => (
                  <span key={`${s.deviceID}-${s.slotIndex}`}
                    className="inline-flex items-center gap-1 px-2 py-0.5 text-xs bg-blue-50 text-blue-600 rounded-full">
                    {t('payment:pkg.slotLabel', { index: s.slotIndex })}
                    <button onClick={() => onRemoveSlot(i)} className="text-blue-400 hover:text-red-500">&times;</button>
                  </span>
                ))}
              </div>
            </div>
          )}

          {/* 套餐列表 */}
          {packages.length === 0 ? (
            <div className="text-center text-xs text-gray-300 py-6">{t('payment:pkg.noPackages')}</div>
          ) : (
            <div className="space-y-2">
              <div className="text-xs text-gray-500 mb-1">{t('payment:pkg.selectPackage')}</div>
              {packages.map(pkg => (
                <button key={pkg.id} onClick={() => onBuy(pkg)} disabled={loading}
                  className="w-full flex items-center justify-between px-4 py-3 border border-gray-200 rounded-lg hover:border-blue-300 hover:bg-blue-50 disabled:opacity-50 transition-colors">
                  <div className="text-left">
                    <div className="text-sm font-medium text-gray-700">{pkg.name}</div>
                    {pkg.day && <div className="text-xs text-gray-400">{t('payment:pkg.days', { days: pkg.day })}</div>}
                  </div>
                  <div className="text-sm font-bold text-blue-500">&yen;{pkg.price}</div>
                </button>
              ))}
            </div>
          )}

          {/* 小计 + 支付方式 */}
          {selectedSlots.length > 0 && (
            <div className="bg-gray-50 rounded-lg p-3 space-y-2">
              <div className="flex items-center justify-between text-xs">
                <span className="text-gray-500">{t('payment:pkg.subtotal')}</span>
                <span className="text-gray-700 font-medium">{t('payment:pkg.instanceCount', { count: selectedSlots.length })}</span>
              </div>
              <div className="flex items-center gap-3">
                <span className="text-xs text-gray-500">{t('payment:pkg.payMethod')}</span>
                <div className="flex gap-3">
                  <label className={`flex items-center gap-1 text-xs cursor-pointer ${payMethod === 'zfb' ? 'text-blue-500 font-medium' : 'text-gray-500'}`}>
                    <input type="radio" name="payMethod" checked={payMethod === 'zfb'} onChange={() => onPayMethodChange('zfb')} className="w-3.5 h-3.5 text-blue-500" />
                    {t('payment:pkg.alipay')}
                  </label>
                  <label className={`flex items-center gap-1 text-xs cursor-pointer ${payMethod === 'score' ? 'text-blue-500 font-medium' : 'text-gray-500'}`}>
                    <input type="radio" name="payMethod" checked={payMethod === 'score'} onChange={() => onPayMethodChange('score')} className="w-3.5 h-3.5 text-blue-500" />
                    {t('payment:pkg.score')}
                    {scoreLoading ? (
                      <span className="text-gray-300">{t('status.loading')}</span>
                    ) : (
                      <span className="text-gray-400">{t('payment:pkg.availableScore', { score: userScore })}</span>
                    )}
                  </label>
                </div>
              </div>
            </div>
          )}
        </div>

        <div className="px-5 py-3 border-t border-gray-100">
          <button onClick={onClose} className="w-full px-4 py-2 text-sm bg-gray-100 text-gray-600 rounded-lg hover:bg-gray-200">{t('button.cancel')}</button>
        </div>
      </div>
    </div>
  )
}

export { PackageModal }
