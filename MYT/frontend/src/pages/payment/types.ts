export interface PackageItem {
  id: string
  name: string
  price: string
  day: string
}

export interface OrderInfo {
  orderId: string
  qrcode: string
}

export interface BindInfo {
  device_id: string
  bind_status: number
}

export interface SlotExpireInfo {
  slot_index: number
  expire_ts: string
  state: number
}

export interface DeviceSlotInfo {
  device_id: string
  slots: SlotExpireInfo[]
}

export type PayMethod = 'zfb' | 'score'

export const PAY_METHOD_LABELS: Record<PayMethod, string> = {
  zfb: 'pkg.alipay',
  score: 'pkg.score',
}
