import * as MytAgentService from '../../../bindings/github.com/moyunteng/myt/mytagentservice'
import { AgentWsClient } from './agentWs'

/** RPC 调用（走 /ws JSON-RPC） */
export async function rpc<T = unknown>(client: AgentWsClient | null, method: string, params?: Record<string, unknown>): Promise<T> {
  if (!client) return Promise.reject(new Error('Not connected'))
  const res = await client.send(method, params)
  // 防御性处理：RPC 可能返回 {data:[...]} 或裸数组或 null
  return (res as T) ?? ([] as unknown as T)
}

/** 从 RPC 响应中提取数组（处理 {skills:[...]} / {tasks:[...]} / 裸数组 等多种格式） */
function extractArray<T>(res: unknown, ...keys: string[]): T[] {
  if (Array.isArray(res)) return res as T[]
  if (res && typeof res === 'object') {
    const obj = res as Record<string, unknown>
    for (const k of keys) {
      if (Array.isArray(obj[k])) return obj[k] as T[]
    }
    // 尝试常见的 data/list 字段
    if (Array.isArray(obj.data)) return obj.data as T[]
    if (Array.isArray(obj.list)) return obj.list as T[]
  }
  return []
}

/** HTTP 代理调用（走 Go HttpProxy，绕过 CORS） */
export async function agentGet<T = unknown>(path: string): Promise<T> {
  const res = await MytAgentService.HttpProxy('GET', path, '')
  try { return JSON.parse(res) as T } catch { return res as unknown as T }
}

export async function agentPost<T = unknown>(path: string, body?: Record<string, unknown>): Promise<T> {
  const res = await MytAgentService.HttpProxy('POST', path, body ? JSON.stringify(body) : '')
  try { return JSON.parse(res) as T } catch { return res as unknown as T }
}

export async function agentDelete<T = unknown>(path: string): Promise<T> {
  const res = await MytAgentService.HttpProxy('DELETE', path, '')
  try { return JSON.parse(res) as T } catch { return res as unknown as T }
}

/** 文件上传代理 */
export async function agentUpload(fileName: string, fileData: Uint8Array): Promise<{ ok: boolean; path: string }> {
  // Wails []byte 参数接收 base64 字符串
  let binary = ''
  for (let i = 0; i < fileData.length; i++) binary += String.fromCharCode(fileData[i])
  const res = await MytAgentService.HttpProxyUpload(fileName, btoa(binary))
  try { return JSON.parse(res) as { ok: boolean; path: string } } catch { return { ok: false, path: '' } }
}

// ── 技能管理 ──

export interface SkillItem {
  name: string
  description?: string
  content?: string
  scripts?: Record<string, string>
  form_schema?: FormFieldDef[]
  enabled?: boolean
  type?: string
  source?: string
  installed?: boolean
  update_available?: boolean
  auto_update?: boolean
  is_default?: boolean
  author?: string
  tags?: string[]
}

export interface FormFieldDef {
  name: string
  label: string
  type: 'text' | 'number' | 'select' | 'textarea' | 'password' | 'checkbox' | 'radio' | 'date' | 'slider' | 'multiselect'
  required?: boolean
  default?: string
  options?: string[]
  option_labels?: string[]
  placeholder?: string
  min?: string
  max?: string
  step?: string
  help?: string
}

export const skillsApi = {
  list: async (client: AgentWsClient | null) => extractArray<SkillItem>(await rpc(client, 'skills.list'), 'skills'),
  official: async (client: AgentWsClient | null) => extractArray<SkillItem>(await rpc(client, 'skills.official'), 'skills', 'official'),
  market: async (client: AgentWsClient | null, q: string) => extractArray<SkillItem>(await rpc(client, 'skills.market', { q }), 'skills', 'results'),
  create: (client: AgentWsClient | null, skill: SkillItem) => rpc(client, 'skills.create', skill as unknown as Record<string, unknown>),
  update: (client: AgentWsClient | null, name: string, skill: Partial<SkillItem>) => rpc(client, 'skills.update', { ...skill, name } as unknown as Record<string, unknown>),
  delete: (client: AgentWsClient | null, name: string) => rpc(client, 'skills.delete', { name }),
  install: (client: AgentWsClient | null, name: string) => rpc(client, 'skills.install', { name }),
  uninstall: (client: AgentWsClient | null, name: string) => rpc(client, 'skills.uninstall', { name }),
  setAutoUpdate: (client: AgentWsClient | null, name: string, val: boolean) => rpc(client, 'skills.setAutoUpdate', { name, auto_update: val }),
  publish: (client: AgentWsClient | null, skill: SkillItem) => rpc(client, 'skills.publish', skill as unknown as Record<string, unknown>),
}

// ── 定时任务 ──

export interface TaskItem {
  id: string
  description: string
  cron_expr?: string
  run_once_at?: string
  task_type: 'autonomous' | 'notification'
  status: 'active' | 'paused' | 'done' | 'failed' | 'deleted'
  last_fired?: string
  created_at: string
}

export interface AsyncTaskItem {
  id: string
  type: string
  status: 'pending' | 'running' | 'done' | 'failed'
  title: string
  detail?: string
  steps?: unknown[]
  error?: string
  created_at: string
  started_at?: string
  finished_at?: string
  duration_seconds?: number
}

export const tasksApi = {
  list: async (client: AgentWsClient | null) => extractArray<TaskItem>(await rpc(client, 'tasks.list'), 'tasks'),
  create: (client: AgentWsClient | null, task: { description: string; task_type: string; cron_expr?: string; run_once_at?: string; skill_content?: string }) => rpc(client, 'tasks.create', task as unknown as Record<string, unknown>),
  delete: (client: AgentWsClient | null, id: string) => rpc(client, 'tasks.delete', { id }),
  updateStatus: (client: AgentWsClient | null, id: string, status: string) => rpc(client, 'tasks.updateStatus', { id, status }),
  executions: async (client: AgentWsClient | null, title: string, page = 1, pageSize = 20) => extractArray<AsyncTaskItem>(await rpc(client, 'tasks.executions', { title, page, page_size: pageSize }), 'tasks', 'executions'),
  queue: async (client: AgentWsClient | null, page = 1, pageSize = 20, type = '', status = '') => {
    const res = await rpc<Record<string, unknown>>(client, 'tasks.queue', { page, page_size: pageSize, type, status })
    return { tasks: extractArray<AsyncTaskItem>(res, 'tasks'), total: (res?.total as number) || 0, stats: (res?.stats as Record<string, number>) || {} }
  },
}

// ── 消息平台（走 Go HTTP 代理） ──

export interface PlatformItem {
  platform: string
  config: Record<string, unknown>
  enabled: boolean
}

export const platformsApi = {
  list: async () => extractArray<PlatformItem>(await agentGet('/api/platforms'), 'platforms'),
  disconnect: (name: string) => agentDelete(`/api/platforms/${encodeURIComponent(name)}`),
  createQR: (platform: string) => agentGet<{ ok: boolean; qr_id: string; qr_url: string }>(`/api/platforms/qr/create/${encodeURIComponent(platform)}`),
  pollQR: (id: string) => agentGet<{ ok: boolean; status: string; platform?: string; config?: Record<string, unknown> }>(`/api/platforms/qr/status/${encodeURIComponent(id)}`),
}
