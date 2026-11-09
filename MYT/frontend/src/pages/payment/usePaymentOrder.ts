// 支付/订单相关状态与逻辑
import { useState, useRef } from 'react'
import { useTranslation } from 'react-i18next'
import * as PaymentService from '../../../bindings/github.com/moyunteng/myt/paymentservicewrapper'
import { PackageItem, OrderInfo, PayMethod } from './types'
import { getErrorMessage } from '../../utils/error'

interface UsePaymentOrderParams {
  selectedSlots: Map<string, Set<number>>
  totalSelectedCount: number
  setLoading: (v: boolean) => void
  setError: (v: string) => void
  onPaymentSuccess: () => void
}

export function usePaymentOrder({ selectedSlots, totalSelectedCount, setLoading, setError, onPaymentSuccess }: UsePaymentOrderParams) {
  const { t } = useTranslation()
  const [packages, setPackages] = useState<PackageItem[]>([])
  const [showPackageModal, setShowPackageModal] = useState(false)
  const [payMethod, setPayMethod] = useState<PayMethod>('zfb')
  const [orderInfo, setOrderInfo] = useState<OrderInfo | null>(null)
  const [polling, setPolling] = useState(false)
  const pollingRef = useRef<ReturnType<typeof setInterval> | null>(null)

  const pollPaymentStatus = (orderId: string) => {
    setPolling(true)
    let attempts = 0
    pollingRef.current = setInterval(async () => {
      attempts++
      try {
        const status = await PaymentService.QueryOrderStatus(orderId) as { state: number }
        console.log(`[payment] poll #${attempts} orderId=${orderId} state=${status?.state} raw=`, status)
        if (status?.state === 2) {
          if (pollingRef.current) clearInterval(pollingRef.current)
          pollingRef.current = null
          setPolling(false)
          setOrderInfo(null)
          onPaymentSuccess()
          return
        }
        // state=1 表示支付中，继续轮询；其他值也继续
      } catch (err) {
        console.error(`[payment] poll #${attempts} error:`, err)
        attempts--
      }
      // 超时上限：200 次有效轮询（约 10 分钟）
      if (attempts >= 200) {
        if (pollingRef.current) clearInterval(pollingRef.current)
        pollingRef.current = null
        setPolling(false)
      }
    }, 3000)
  }

  const handleRenew = async () => {
    if (totalSelectedCount === 0) return
    setLoading(true)
    setError('')
    try {
      const pkgs = await PaymentService.GetPackages()
      setPackages(pkgs as PackageItem[])
      setShowPackageModal(true)
    } catch (err) {
      setError(getErrorMessage(err))
    } finally {
      setLoading(false)
    }
  }

  const handleBuy = async (pkg: PackageItem) => {
    const rabbetObj: Record<string, string[]> = {}
    selectedSlots.forEach((slots, deviceID) => {
      rabbetObj[deviceID] = Array.from(slots).map(String)
    })
    const totalPrice = (parseFloat(pkg.price) * totalSelectedCount).toFixed(2)
    setLoading(true)
    setError('')
    try {
      if (payMethod === 'score') {
        // 积分支付
        await PaymentService.ScoreExchangeTermTime(JSON.stringify(rabbetObj), pkg.id, totalPrice)
        setShowPackageModal(false)
        onPaymentSuccess()
      } else {
        // 支付宝/微信支付
        const result = await PaymentService.CreateOrder(JSON.stringify(rabbetObj), pkg.id, pkg.price, payMethod)
        if (result?.qrcode) {
          const orderId = result.order_id ?? ''
          setOrderInfo({ orderId, qrcode: result.qrcode })
          setShowPackageModal(false)
          pollPaymentStatus(orderId)
        } else {
          setError(result?.msg || result?.code ? t('payment:order.createFailed', { code: result?.code || '', msg: result?.msg || t('payment:order.noQrcode') }) : t('payment:order.noQrcodeRetry'))
        }
      }
    } catch (err) {
      setError(getErrorMessage(err))
    } finally {
      setLoading(false)
    }
  }

  const resetOrder = () => {
    if (pollingRef.current) { clearInterval(pollingRef.current); pollingRef.current = null }
    setPackages([])
    setShowPackageModal(false)
    setOrderInfo(null)
    setPolling(false)
  }

  return {
    packages,
    showPackageModal, setShowPackageModal,
    payMethod, setPayMethod,
    orderInfo, setOrderInfo,
    polling,
    handleRenew,
    handleBuy,
    resetOrder,
  }
}
