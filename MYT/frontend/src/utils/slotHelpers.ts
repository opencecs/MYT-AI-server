import i18n from '../i18n'

export const expireColor = (state?: number) => {
  switch (state) {
    case 0: return 'text-blue-500'
    case 1: return 'text-yellow-500'
    case 2: return 'text-red-500'
    default: return 'text-gray-400'
  }
}

export const expireLabel = (state?: number) => {
  switch (state) {
    case 0: return i18n.t('payment:slotStatus.normal')
    case 1: return i18n.t('payment:slotStatus.expiring')
    case 2: return i18n.t('payment:slotStatus.expired')
    default: return ''
  }
}

/** 坑位状态二次确认提示文案，返回空字符串表示无需提示 */
export const slotWarningMessage = (state?: number): string => {
  switch (state) {
    case 1: return i18n.t('payment:slotWarning.expiring')
    case 2: return i18n.t('payment:slotWarning.expired')
    default: return i18n.t('payment:slotWarning.unsubscribed')
  }
}

/** 是否需要弹出坑位状态确认（state 为 1/2 或无 state 时需要） */
export const needSlotConfirm = (state?: number): boolean => {
  return state !== 0
}
