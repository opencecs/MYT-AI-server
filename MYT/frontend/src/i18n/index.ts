import i18n from 'i18next'
import { initReactI18next } from 'react-i18next'

// zh-CN
import zhCNCommon from './locales/zh-CN/common.json'
import zhCNMirror from './locales/zh-CN/mirror.json'
import zhCNHost from './locales/zh-CN/host.json'
import zhCNAgent from './locales/zh-CN/agent.json'
import zhCNModel from './locales/zh-CN/model.json'
import zhCNVpc from './locales/zh-CN/vpc.json'
import zhCNDiskless from './locales/zh-CN/diskless.json'
import zhCNGroupcontrol from './locales/zh-CN/groupcontrol.json'
import zhCNMaintenance from './locales/zh-CN/maintenance.json'
import zhCNSettings from './locales/zh-CN/settings.json'
import zhCNPayment from './locales/zh-CN/payment.json'
import zhCNStream from './locales/zh-CN/stream.json'
import zhCNBackup from './locales/zh-CN/backup.json'
import zhCNRom from './locales/zh-CN/rom.json'

// en-US
import enUSCommon from './locales/en-US/common.json'
import enUSMirror from './locales/en-US/mirror.json'
import enUSHost from './locales/en-US/host.json'
import enUSAgent from './locales/en-US/agent.json'
import enUSModel from './locales/en-US/model.json'
import enUSVpc from './locales/en-US/vpc.json'
import enUSDiskless from './locales/en-US/diskless.json'
import enUSGroupcontrol from './locales/en-US/groupcontrol.json'
import enUSMaintenance from './locales/en-US/maintenance.json'
import enUSSettings from './locales/en-US/settings.json'
import enUSPayment from './locales/en-US/payment.json'
import enUSStream from './locales/en-US/stream.json'
import enUSBackup from './locales/en-US/backup.json'
import enUSRom from './locales/en-US/rom.json'

export const LANGUAGES = ['zh-CN', 'en-US'] as const
export type Language = typeof LANGUAGES[number]

export const NAMESPACES = [
  'common', 'mirror', 'host', 'agent', 'model', 'vpc',
  'diskless', 'groupcontrol', 'maintenance', 'settings',
  'payment', 'stream', 'backup', 'rom',
] as const

const savedLang = (localStorage.getItem('language') as Language) || 'zh-CN'

i18n.use(initReactI18next).init({
  resources: {
    'zh-CN': {
      common: zhCNCommon,
      mirror: zhCNMirror,
      host: zhCNHost,
      agent: zhCNAgent,
      model: zhCNModel,
      vpc: zhCNVpc,
      diskless: zhCNDiskless,
      groupcontrol: zhCNGroupcontrol,
      maintenance: zhCNMaintenance,
      settings: zhCNSettings,
      payment: zhCNPayment,
      stream: zhCNStream,
      backup: zhCNBackup,
      rom: zhCNRom,
    },
    'en-US': {
      common: enUSCommon,
      mirror: enUSMirror,
      host: enUSHost,
      agent: enUSAgent,
      model: enUSModel,
      vpc: enUSVpc,
      diskless: enUSDiskless,
      groupcontrol: enUSGroupcontrol,
      maintenance: enUSMaintenance,
      settings: enUSSettings,
      payment: enUSPayment,
      stream: enUSStream,
      backup: enUSBackup,
      rom: enUSRom,
    },
  },
  lng: savedLang,
  fallbackLng: 'zh-CN',
  defaultNS: 'common',
  ns: NAMESPACES,
  interpolation: {
    escapeValue: false,
  },
})

export function changeLanguage(lang: Language) {
  localStorage.setItem('language', lang)
  i18n.changeLanguage(lang)
}

export default i18n
