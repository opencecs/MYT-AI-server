import { useState, useEffect, useRef } from 'react'
import * as AppService from '../../../bindings/github.com/moyunteng/myt/appservice'
import { useUpdateState } from '../../hooks/useUpdateCheck'
import { useTranslation } from 'react-i18next'

function formatSize(bytes: number): string {
  if (bytes < 1024) return bytes + ' B'
  if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(1) + ' KB'
  return (bytes / 1024 / 1024).toFixed(1) + ' MB'
}

function AboutSection() {
  const { t } = useTranslation()
  const [version, setVersion] = useState('')
  const [buildTime, setBuildTime] = useState('')
  const [autoCheck, setAutoCheck] = useState(true)
  const [autoDownload, setAutoDownload] = useState(false)
  const [menuOpen, setMenuOpen] = useState(false)
  const menuRef = useRef<HTMLDivElement>(null)

  const { updateStep, updateResult, checkUpdate } = useUpdateState()

  useEffect(() => {
    AppService.GetVersion().then((v: string) => setVersion(v || 'dev')).catch(() => setVersion('dev'))
    AppService.GetBuildTime().then((t: string) => setBuildTime(t || '-')).catch(() => setBuildTime('-'))
    AppService.GetAutoCheckUpdate().then((v: boolean) => setAutoCheck(v)).catch(() => {})
    AppService.GetAutoDownloadUpdate().then((v: boolean) => setAutoDownload(v)).catch(() => {})
  }, [])

  useEffect(() => {
    const handler = (e: MouseEvent) => {
      if (menuRef.current && !menuRef.current.contains(e.target as Node)) setMenuOpen(false)
    }
    if (menuOpen) document.addEventListener('mousedown', handler)
    return () => document.removeEventListener('mousedown', handler)
  }, [menuOpen])

  const toggleAutoCheck = async () => {
    const next = !autoCheck
    try { await AppService.SetAutoCheckUpdate(next); setAutoCheck(next) } catch {}
  }

  const toggleAutoDownload = async () => {
    const next = !autoDownload
    try { await AppService.SetAutoDownloadUpdate(next); setAutoDownload(next) } catch {}
  }

  const switchBtn = (on: boolean, onClick: () => void) => (
    <button onClick={onClick} className={`relative w-8 h-[18px] rounded-full transition-colors cursor-pointer ${on ? 'bg-blue-500' : 'bg-gray-300'}`}>
      <span className={`absolute top-[1px] left-[1px] w-4 h-4 rounded-full bg-white shadow transition-transform ${on ? 'translate-x-[14px]' : ''}`} />
    </button>
  )

  return (
    <div className="space-y-2">
      <h3 className="text-xs font-semibold text-gray-500 uppercase tracking-wider">{t('settings:about')}</h3>
      <div className="bg-gray-50 rounded-lg p-3">
        <div className="flex items-center gap-3 mb-2">
          <div className="w-10 h-10 rounded-lg bg-blue-500 flex items-center justify-center shrink-0">
            <img src="/logo.png" alt="logo" className="w-8 h-8 object-contain" />
          </div>
          <div>
            <div className="text-sm font-semibold text-gray-800">{t('settings:appName')}</div>
            <div className="text-[10px] text-gray-400">{t('settings:appDesc')}</div>
          </div>
        </div>
        <div className="space-y-1 border-t border-gray-200 pt-2">
          <div className="flex items-center justify-between text-xs">
            <span className="text-gray-400">{t('settings:version')}</span>
            <div className="flex items-center gap-2">
              <span className="text-gray-600">{version || 'dev'}</span>
              {updateResult?.hasUpdate && (
                <span className="text-[10px] text-green-600 bg-green-50 px-1.5 py-0.5 rounded">{t('settings:newVersion')}</span>
              )}
              {updateResult && !updateResult.hasUpdate && !updateResult.error && (
                <span className="text-gray-400 text-[10px]">{t('settings:upToDate')}</span>
              )}
              <div ref={menuRef} className="relative">
                <button onClick={() => setMenuOpen(!menuOpen)} className="relative w-5 h-5 flex items-center justify-center text-gray-400 hover:text-gray-600 hover:bg-gray-200 rounded cursor-pointer">
                  <svg width="12" height="12" viewBox="0 0 12 12" fill="currentColor"><circle cx="6" cy="2" r="1.2"/><circle cx="6" cy="6" r="1.2"/><circle cx="6" cy="10" r="1.2"/></svg>
                  {updateResult?.hasUpdate && (
                    <span className="absolute -top-1 -right-1 bg-red-500 text-white text-[7px] font-bold px-0.5 rounded animate-pulse">new</span>
                  )}
                </button>
                {menuOpen && (
                  <div className="absolute right-0 top-6 bg-white rounded-lg shadow-lg border border-gray-200 py-1 w-36 z-50">
                    <button onClick={() => { checkUpdate(); setMenuOpen(false) }} disabled={updateStep === 'checking' || updateStep === 'downloading'} className="w-full text-left px-3 py-1.5 text-xs text-gray-700 hover:bg-gray-50 disabled:opacity-50 cursor-pointer">
                      {updateStep === 'checking' ? t('settings:checking') : t('settings:checkUpdate')}
                    </button>
                    <div className="flex items-center justify-between px-3 py-1.5"><span className="text-xs text-gray-700">{t('settings:autoCheck')}</span>{switchBtn(autoCheck, toggleAutoCheck)}</div>
                    <div className="flex items-center justify-between px-3 py-1.5"><span className="text-xs text-gray-700">{t('settings:autoDownload')}</span>{switchBtn(autoDownload, toggleAutoDownload)}</div>
                  </div>
                )}
              </div>
            </div>
          </div>
          <div className="flex items-center justify-between text-xs">
            <span className="text-gray-400">{t('settings:build')}</span>
            <span className="text-gray-600">{buildTime || '-'}</span>
          </div>
          <div className="flex items-center justify-between text-xs">
            <span className="text-gray-400">{t('settings:techStack')}</span>
            <span className="text-gray-600">{t('settings:techStackValue')}</span>
          </div>
        </div>
        <div className="mt-2 pt-2 border-t border-gray-200 text-center">
          <span className="text-[10px] text-blue-500 cursor-pointer hover:text-blue-600">{t('settings:website')}</span>
        </div>
      </div>
    </div>
  )
}

export function UpdateModals() {
  const { t } = useTranslation()
  const { updateStep, updateResult, updateError, downloadProgress, downloadStep, startDownload, dismissUpdate } = useUpdateState()
  const [showNotes, setShowNotes] = useState(true)
  const [version, setVersion] = useState('')

  useEffect(() => {
    AppService.GetVersion().then((v: string) => setVersion(v || 'dev')).catch(() => setVersion('dev'))
  }, [])

  return (
    <>
      {/* 发现新版本弹窗 */}
      {updateStep === 'found' && updateResult?.hasUpdate && (
        <div className="fixed inset-0 bg-black/30 flex items-center justify-center z-50">
          <div className="bg-white rounded-lg shadow-xl w-80 overflow-hidden">
            <div className="px-5 pt-5 pb-4">
              <div className="flex items-center justify-between mb-3">
                <h3 className="text-sm font-semibold text-gray-800">{t('settings:updateFound')}</h3>
                <button onClick={dismissUpdate} className="text-gray-400 hover:text-gray-600 text-lg leading-none">&times;</button>
              </div>
              <div className="space-y-2 mb-3">
                <div className="flex items-center text-xs"><span className="text-gray-400 w-16 shrink-0">{t('settings:currentVersion')}</span><span className="text-gray-600">{version && version !== 'dev' && !version.startsWith('v') ? 'v' : ''}{version}</span></div>
                <div className="flex items-center text-xs"><span className="text-gray-400 w-16 shrink-0">{t('settings:latestVersion')}</span><span className="text-green-600 font-medium">v{updateResult.version}</span></div>
                {updateResult.fileSize ? <div className="flex items-center text-xs"><span className="text-gray-400 w-16 shrink-0">{t('settings:fileSize')}</span><span className="text-gray-600">{formatSize(updateResult.fileSize)}</span></div> : null}
              </div>
              <div className="border border-gray-200 rounded overflow-hidden mb-1">
                <button onClick={() => setShowNotes(!showNotes)} className="w-full flex items-center justify-between px-3 py-2 text-xs text-gray-700 hover:bg-gray-50 cursor-pointer">
                  <span>{t('settings:updateNotes')}</span>
                  <svg width="10" height="10" viewBox="0 0 10 10" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" className={`transition-transform ${showNotes ? 'rotate-180' : ''}`}><polyline points="2,4 5,7 8,4" /></svg>
                </button>
                {showNotes && (
                  <div className="max-h-36 overflow-y-auto px-3 py-2 text-[11px] text-gray-600 bg-gray-50 border-t border-gray-200 leading-relaxed" style={{ whiteSpace: 'pre-wrap' }}>
                    {updateResult.releaseNotes ? updateResult.releaseNotes.replace(/\\n/g, '\n') : t('settings:noUpdateNotes')}
                  </div>
                )}
              </div>
            </div>
            <div className="flex border-t border-gray-200">
              <button onClick={dismissUpdate} className="flex-1 py-2.5 text-xs text-gray-600 hover:bg-gray-50 cursor-pointer border-r border-gray-200">{t('settings:remindLater')}</button>
              <button onClick={() => startDownload(updateResult.downloadUrl || '', updateResult.checksum)} className="flex-1 py-2.5 text-xs text-blue-500 font-medium hover:bg-blue-50 cursor-pointer">{t('settings:updateNow')}</button>
            </div>
          </div>
        </div>
      )}

      {/* 下载中弹窗 */}
      {updateStep === 'downloading' && (
        <div className="fixed inset-0 bg-black/30 flex items-center justify-center z-50">
          <div className="bg-white rounded-lg shadow-xl p-5 w-72">
            <h3 className="text-sm font-semibold text-gray-800 mb-3">{t('settings:updating')}</h3>
            <div className="w-full bg-gray-200 rounded-full h-2 mb-2">
              <div className="bg-blue-500 h-2 rounded-full transition-all duration-300" style={{ width: `${downloadProgress}%` }} />
            </div>
            <p className="text-xs text-gray-500">{downloadStep || t('settings:preparing')} {downloadProgress}%</p>
          </div>
        </div>
      )}

      {/* 错误弹窗 */}
      {updateStep === 'error' && (
        <div className="fixed inset-0 bg-black/30 flex items-center justify-center z-50">
          <div className="bg-white rounded-lg shadow-xl p-5 w-72">
            <div className="flex items-center justify-between mb-3">
              <h3 className="text-sm font-semibold text-gray-800">{t('settings:updateFailed')}</h3>
              <button onClick={dismissUpdate} className="text-gray-400 hover:text-gray-600 text-lg leading-none">&times;</button>
            </div>
            <p className="text-xs text-red-500 mb-3">{updateError}</p>
            <div className="flex justify-end"><button onClick={dismissUpdate} className="px-3 py-1.5 text-xs bg-gray-100 rounded hover:bg-gray-200 text-gray-700 cursor-pointer">{t('button.close')}</button></div>
          </div>
        </div>
      )}
    </>
  )
}

export default AboutSection
