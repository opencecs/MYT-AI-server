import { useCallback, useEffect, useMemo, useState } from 'react'
import * as MCPService from '../../../bindings/github.com/moyunteng/myt/mcpservice'
import { MCPStatus } from '../../../bindings/github.com/moyunteng/myt/models'
import { useTranslation } from 'react-i18next'
import Switch from '../../components/Switch'
import ConfirmDialog from '../../components/ConfirmDialog'

/** token 打码显示：保留首尾各 4 位，中间用 • 遮盖 */
function maskToken(token: string): string {
  if (token.length <= 8) return '••••••••'
  return `${token.slice(0, 4)}••••${token.slice(-4)}`
}

/** 合法监听地址：空（恢复默认）、localhost、0.0.0.0 或合法 IPv4/IPv6 */
function isValidListenHost(host: string): boolean {
  if (host === '') return true
  if (host === 'localhost' || host === '0.0.0.0') return true
  // IPv4
  const v4 = /^(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})$/.exec(host)
  if (v4) return v4.slice(1).every(seg => Number(seg) <= 255)
  return false // IPv6 裸地址冒号易与端口混淆，暂不支持
}

/**
 * McpSection 「MCP 服务」区块：开关 + 运行状态卡 + 使用说明。
 * 接自建 MCPService（热启停，无需重启客户端）：
 * - enabled = 配置开关；running = server 实际监听中
 * - url 为实际地址（端口被占时非默认端口）；token 用于客户端鉴权
 * - cdpReady=false 表示界面控制类工具（截图/点击/读界面）需重启客户端后可用
 */
function McpSection() {
  const { t } = useTranslation()
  const [status, setStatus] = useState<MCPStatus | null>(null)
  const [busy, setBusy] = useState(false) // 切换/重新生成进行中，防重复提交
  const [showToken, setShowToken] = useState(false)
  const [confirmRegen, setConfirmRegen] = useState(false)
  const [copiedKey, setCopiedKey] = useState<string | null>(null)
  // 监听地址编辑：草稿值 + 保存反馈（null=无反馈，'ok'=已保存，'invalid'=校验失败，'error'=接口失败）
  const [hostDraft, setHostDraft] = useState('')
  const [hostFeedback, setHostFeedback] = useState<'ok' | 'invalid' | 'error' | null>(null)

  const refresh = useCallback(async () => {
    try {
      setStatus(await MCPService.GetMCPStatus())
    } catch (err) {
      console.error('Failed to get MCP status:', err)
    }
  }, [])

  useEffect(() => {
    void refresh()
  }, [refresh])

  // 状态到达后同步草稿值（仅当用户未在编辑中途时覆盖，简化处理：每次刷新同步）
  useEffect(() => {
    if (status) setHostDraft(status.host ?? '')
  }, [status])

  // 保存监听地址：后端运行中会自动重启服务使新地址生效
  const saveHost = async () => {
    if (busy || !status) return
    const host = hostDraft.trim()
    if (!isValidListenHost(host)) {
      setHostFeedback('invalid')
      return
    }
    setBusy(true)
    try {
      await MCPService.SetMCPHost(host)
      await refresh()
      setHostFeedback('ok')
    } catch (err) {
      console.error('Failed to set MCP host:', err)
      setHostFeedback('error')
    } finally {
      setBusy(false)
    }
  }

  // 复刻 autoStart 模式：先等接口返回，成功后再刷新本地状态；失败仅记录
  const toggleMCP = async () => {
    if (busy || !status) return
    const next = !status.enabled
    setBusy(true)
    try {
      await MCPService.SetMCPEnabled(next)
      await refresh()
    } catch (err) {
      console.error('Failed to set MCP enabled:', err)
    } finally {
      setBusy(false)
    }
  }

  const regenerateToken = async () => {
    setConfirmRegen(false)
    setBusy(true)
    try {
      await MCPService.RegenerateMCPToken()
      await refresh()
    } catch (err) {
      console.error('Failed to regenerate MCP token:', err)
    } finally {
      setBusy(false)
    }
  }

  const copyText = async (text: string, key: string) => {
    try {
      await navigator.clipboard.writeText(text)
      setCopiedKey(key)
      setTimeout(() => setCopiedKey(null), 1500)
    } catch (err) {
      console.error('Failed to copy:', err)
    }
  }

  // 示例 JSON 配置：用实际 url + token 拼接（Claude Desktop / Cursor 的 mcpServers 格式）
  const exampleConfig = useMemo(() => {
    const url = status ? `${status.url}?token=${status.token}` : ''
    return JSON.stringify({ mcpServers: { 'myt-client': { url } } }, null, 2)
  }, [status])

  const enabled = status?.enabled ?? false
  const copyBtn = (key: string, text: string) => (
    <button
      onClick={() => void copyText(text, key)}
      className="text-[10px] px-2 py-0.5 border border-gray-200 rounded bg-white text-gray-500 hover:text-blue-500 hover:border-blue-300 transition-colors shrink-0"
    >
      {copiedKey === key ? t('settings:copied') : t('settings:copy')}
    </button>
  )

  return (
    <div className="space-y-2 flex flex-col">
      <h3 className="text-xs font-semibold text-gray-500 uppercase tracking-wider">{t('settings:mcpDebug')}</h3>
      <div className="bg-gray-50 rounded-lg p-3 space-y-3 flex-1">
        <div className="flex items-center justify-between">
          <div>
            <div className="text-xs text-gray-600">{t('settings:mcpDebug')}</div>
            <div className="text-[10px] text-gray-400">{t('settings:mcpDebugDesc')}</div>
          </div>
          <Switch checked={enabled} onChange={toggleMCP} disabled={busy} />
        </div>
        {enabled && status && (
          <>
            {/* 状态卡：运行徽标 / URL / token / 工具数 */}
            <div className="bg-white border border-gray-200 rounded-lg p-2.5 space-y-2">
              <div className="flex items-center gap-1.5">
                <span
                  className={`inline-block size-1.5 rounded-full ${status.running ? 'bg-green-500' : 'bg-gray-300'}`}
                />
                <span className={`text-[10px] ${status.running ? 'text-green-600' : 'text-gray-400'}`}>
                  {status.running ? t('settings:mcpRunning') : t('settings:mcpStopped')}
                </span>
                <span className="text-[10px] text-gray-400 ml-auto">
                  {t('settings:mcpToolsCount')}：{status.tools}
                </span>
              </div>
              <div className="flex items-center justify-between gap-2">
                <div className="min-w-0">
                  <div className="text-[10px] text-gray-400">{t('settings:mcpConnUrl')}</div>
                  <div className="text-[10px] font-mono text-gray-600 truncate">{status.url}</div>
                </div>
                {copyBtn('url', status.url)}
              </div>
              <div className="flex items-center justify-between gap-2">
                <div className="min-w-0">
                  <div className="text-[10px] text-gray-400">{t('settings:mcpToken')}</div>
                  <div
                    className="text-[10px] font-mono text-gray-600 truncate cursor-pointer select-none"
                    onClick={() => setShowToken(v => !v)}
                    title={showToken ? t('settings:mcpHideToken') : t('settings:mcpShowToken')}
                  >
                    {showToken ? status.token : maskToken(status.token)}
                  </div>
                </div>
                <div className="flex items-center gap-1.5 shrink-0">
                  <button
                    onClick={() => setShowToken(v => !v)}
                    className="text-[10px] px-2 py-0.5 border border-gray-200 rounded bg-white text-gray-500 hover:text-blue-500 hover:border-blue-300 transition-colors"
                  >
                    {showToken ? t('settings:mcpHideToken') : t('settings:mcpShowToken')}
                  </button>
                  {copyBtn('token', status.token)}
                </div>
              </div>
              <div className="flex justify-end">
                <button
                  onClick={() => setConfirmRegen(true)}
                  disabled={busy}
                  className="text-[10px] px-2 py-0.5 border border-gray-200 rounded bg-white text-gray-500 hover:text-red-500 hover:border-red-300 transition-colors disabled:opacity-40"
                >
                  {t('settings:mcpRegenToken')}
                </button>
              </div>
            </div>
            {/* 监听地址：输入 + 保存（后端运行中自动重启生效） */}
            <div className="flex items-center justify-between gap-2">
              <div className="min-w-0 flex-1">
                <div className="text-[10px] text-gray-400">{t('settings:mcpListenHost')}</div>
                <input
                  type="text"
                  value={hostDraft}
                  onChange={e => {
                    setHostDraft(e.target.value)
                    setHostFeedback(null)
                  }}
                  onKeyDown={e => {
                    if (e.key === 'Enter') void saveHost()
                  }}
                  placeholder={t('settings:mcpListenHostPlaceholder')}
                  disabled={busy}
                  className="mt-0.5 w-full text-[10px] font-mono text-gray-600 bg-white border border-gray-200 rounded px-2 py-1 focus:outline-none focus:border-blue-300 disabled:opacity-50"
                />
                <div className="text-[9px] text-gray-400 mt-0.5">{t('settings:mcpListenHostDesc')}</div>
              </div>
              <button
                onClick={() => void saveHost()}
                disabled={busy}
                className="text-[10px] px-2 py-1 border border-gray-200 rounded bg-white text-gray-500 hover:text-blue-500 hover:border-blue-300 transition-colors disabled:opacity-40 shrink-0 self-end mb-4"
              >
                {t('settings:set')}
              </button>
            </div>
            {hostFeedback === 'ok' && (
              <div className="text-[10px] text-green-600">{t('settings:mcpHostSaved')}</div>
            )}
            {hostFeedback === 'invalid' && (
              <div className="text-[10px] text-red-500">{t('settings:mcpHostInvalid')}</div>
            )}
            {hostFeedback === 'error' && (
              <div className="text-[10px] text-red-500">{t('settings:mcpHostSaveFailed')}</div>
            )}
            {/* CDP 未就绪：界面控制类工具需重启客户端 */}
            {!status.cdpReady && (
              <div className="text-[10px] text-amber-600 bg-amber-50 border border-amber-200 rounded p-2">
                {t('settings:mcpCdpHint')}
              </div>
            )}
            {/* 安全警示 */}
            <div className="text-[10px] text-gray-400">{t('settings:mcpSecurityHint')}</div>
            {/* 使用说明 + 示例配置 */}
            <div>
              <div className="flex items-center justify-between mb-1">
                <span className="text-[10px] text-gray-400">{t('settings:mcpExampleConfig')}</span>
                {copyBtn('config', exampleConfig)}
              </div>
              <pre className="text-[10px] font-mono bg-white border border-gray-200 rounded p-2 overflow-x-auto text-gray-600">
{exampleConfig}
              </pre>
            </div>
          </>
        )}
        {!enabled && <div className="text-[10px] text-gray-400">{t('settings:mcpDisabledDesc')}</div>}
      </div>
      {confirmRegen && (
        <ConfirmDialog
          title={t('settings:mcpRegenToken')}
          message={t('settings:mcpRegenConfirmMsg')}
          onConfirm={() => void regenerateToken()}
          onCancel={() => setConfirmRegen(false)}
        />
      )}
    </div>
  )
}

export default McpSection
