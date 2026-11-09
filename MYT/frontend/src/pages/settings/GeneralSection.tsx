import { useState, useEffect } from 'react'
import * as AppService from '../../../bindings/github.com/moyunteng/myt/appservice'
import { useTranslation } from 'react-i18next'
import { changeLanguage, LANGUAGES } from '../../i18n'
import Switch from '../../components/Switch'
import { useShowOfflineAndroids } from '../../hooks/useShowOfflineAndroids'
import { useSlotMemory } from '../../hooks/useSlotMemory'
import { useDeviceGroupMode } from '../../hooks/useDeviceGroupMode'

function GeneralSection() {
  const { t, i18n } = useTranslation()
  const [autoStart, setAutoStart] = useState(false)
  const [closeBehavior, setCloseBehavior] = useState('exit')
  const [logLevel, setLogLevel] = useState('info')
  const [autoUpgradeSDK, setAutoUpgradeSDK] = useState(false)
  // 「云机展示：运行中 / 全部」——localStorage 持久化（useSyncExternalStore 模块级 store，
  // 云机/主机 Tab 订阅同一份状态，切换后立即生效）
  const [showOfflineAndroids, setShowOfflineAndroids] = useShowOfflineAndroids()
  // 「坑位记忆」——localStorage 持久化（同 useSyncExternalStore 模块级 store 模式），
  // 开启后新建云机表单默认回填上一次创建选择的坑位（默认关闭）
  const [slotMemory, setSlotMemory] = useSlotMemory()
  // 「设备单分组」——localStorage 持久化（同 useSyncExternalStore 模块级 store 模式），
  // 开启后设备只属一个分组（右键菜单/拖拽保持切换语义），关闭允许多分组（默认开启）
  const [deviceSingleGroup, setDeviceSingleGroup] = useDeviceGroupMode()

  useEffect(() => {
    AppService.GetAutoStart().then((v: boolean) => setAutoStart(v)).catch(() => {})
    AppService.GetCloseBehavior().then((v: string) => setCloseBehavior(v || 'exit')).catch(() => {})
    AppService.GetLogLevel().then((v: string) => setLogLevel(v || 'info')).catch(() => {})
    AppService.GetAutoUpgradeSDK().then((v: boolean) => setAutoUpgradeSDK(v)).catch(() => {})
  }, [])

  const toggleAutoStart = async () => {
    const next = !autoStart
    try {
      await AppService.SetAutoStart(next)
      setAutoStart(next)
    } catch (err) { console.error('Failed to set auto start:', err) }
  }

  const changeCloseBehavior = async (v: string) => {
    try {
      await AppService.SetCloseBehavior(v)
      setCloseBehavior(v)
    } catch (err) { console.error('Failed to set close behavior:', err) }
  }

  const changeLogLevel = async (v: string) => {
    try {
      await AppService.SetLogLevel(v)
      setLogLevel(v)
    } catch (err) { console.error('Failed to set log level:', err) }
  }

  const toggleAutoUpgradeSDK = async () => {
    const next = !autoUpgradeSDK
    try { await AppService.SetAutoUpgradeSDK(next); setAutoUpgradeSDK(next) } catch {}
  }

  const radio = 'w-3 h-3 text-blue-500 border-gray-300 focus:ring-blue-400'

  return (
    <div className="space-y-2 flex flex-col">
      <h3 className="text-xs font-semibold text-gray-500 uppercase tracking-wider">{t('settings:general')}</h3>
      <div className="bg-gray-50 rounded-lg p-3 space-y-3 flex-1">
        <div className="flex items-center justify-between">
          <div>
            <div className="text-xs text-gray-600">{t('settings:autoStart')}</div>
            <div className="text-[10px] text-gray-400">{t('settings:autoStartDesc')}</div>
          </div>
          <Switch checked={autoStart} onChange={toggleAutoStart} />
        </div>
        <div className="flex items-center justify-between">
          <div>
            <div className="text-xs text-gray-600">{t('settings:closeBehavior')}</div>
            <div className="text-[10px] text-gray-400">{t('settings:closeBehaviorDesc')}</div>
          </div>
          <div className="flex items-center gap-2">
            <label className="flex items-center gap-1 text-xs text-gray-600 cursor-pointer">
              <input type="radio" name="closeBehavior" checked={closeBehavior === 'exit'} onChange={() => changeCloseBehavior('exit')} className={radio} />
              {t('settings:exit')}
            </label>
            <label className="flex items-center gap-1 text-xs text-gray-600 cursor-pointer">
              <input type="radio" name="closeBehavior" checked={closeBehavior === 'tray'} onChange={() => changeCloseBehavior('tray')} className={radio} />
              {t('settings:minimize')}
            </label>
          </div>
        </div>
        <div className="flex items-center justify-between">
          <div>
            <div className="text-xs text-gray-600">{t('settings:logLevel')}</div>
            <div className="text-[10px] text-gray-400">{t('settings:logLevelDesc')}</div>
          </div>
          <div className="flex items-center gap-2">
            {(['error', 'info', 'debug'] as const).map(lv => (
              <label key={lv} className="flex items-center gap-1 text-xs text-gray-600 cursor-pointer">
                <input type="radio" name="logLevel" checked={logLevel === lv} onChange={() => changeLogLevel(lv)} className={radio} />
                {lv.charAt(0).toUpperCase() + lv.slice(1)}
              </label>
            ))}
          </div>
        </div>
        <div className="flex items-center justify-between">
          <div>
            <div className="text-xs text-gray-600">{t('settings:autoUpgradeSDK')}</div>
            <div className="text-[10px] text-gray-400">{t('settings:autoUpgradeSDKDesc')}</div>
          </div>
          <Switch checked={autoUpgradeSDK} onChange={toggleAutoUpgradeSDK} />
        </div>
        <div className="flex items-center justify-between">
          <div>
            <div className="text-xs text-gray-600">{t('settings:showOfflineAndroids')}</div>
            <div className="text-[10px] text-gray-400">{t('settings:showOfflineAndroidsDesc')}</div>
          </div>
          <Switch checked={showOfflineAndroids} onChange={() => setShowOfflineAndroids(!showOfflineAndroids)} />
        </div>
        <div className="flex items-center justify-between">
          <div>
            <div className="text-xs text-gray-600">{t('settings:slotMemory')}</div>
            <div className="text-[10px] text-gray-400">{t('settings:slotMemoryDesc')}</div>
          </div>
          <Switch checked={slotMemory} onChange={() => setSlotMemory(!slotMemory)} />
        </div>
        <div className="flex items-center justify-between">
          <div>
            <div className="text-xs text-gray-600">{t('settings:deviceSingleGroup')}</div>
            <div className="text-[10px] text-gray-400">{t('settings:deviceSingleGroupDesc')}</div>
          </div>
          <Switch checked={deviceSingleGroup} onChange={() => setDeviceSingleGroup(!deviceSingleGroup)} />
        </div>
        <div className="flex items-center justify-between">
          <div>
            <div className="text-xs text-gray-600">{t('settings:language')}</div>
            <div className="text-[10px] text-gray-400">{t('settings:languageDesc')}</div>
          </div>
          <select
            value={i18n.language}
            onChange={e => changeLanguage(e.target.value as typeof LANGUAGES[number])}
            className="text-xs px-2 py-1 border border-gray-200 rounded-lg bg-white text-gray-600 focus:outline-none focus:border-blue-400"
          >
            {LANGUAGES.map(lang => (
              <option key={lang} value={lang}>
                {lang === 'zh-CN' ? '中文' : 'English'}
              </option>
            ))}
          </select>
        </div>
      </div>
    </div>
  )
}

export default GeneralSection
