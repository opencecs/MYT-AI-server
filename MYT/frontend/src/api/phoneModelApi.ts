import * as PhoneModelService from '../../bindings/github.com/moyunteng/myt/phonemodelservice'
import * as AIService from '../../bindings/github.com/moyunteng/myt/aiservicewrapper'

// --- 线上机型 ---

const TEMPLATE_API = 'https://newapi.moyunteng.com/api/v1/template/list'

export interface OnlineModel {
  id: number
  name: string
  android_version: string
}

export async function ListOnlineModels(androidVersion = '14'): Promise<OnlineModel[]> {
  const resp = await fetch(`${TEMPLATE_API}?page=0&android_version=${androidVersion}`)
  const json = await resp.json()
  if (json.code_id !== 200) throw new Error(json.message || '获取失败')
  const data = json.data
  return Array.isArray(data) ? data : data?.list || []
}

// --- 本地编辑机型 ---

export interface EditedModel {
  name: string
  size: number
  modTime: string
}

export async function ListEditedModels(): Promise<EditedModel[]> {
  return (await PhoneModelService.ListEditedModels() as EditedModel[] || [])
}

export async function DeleteEditedModel(name: string): Promise<void> {
  await PhoneModelService.DeleteEditedModel(name)
}

// --- Wails 绑定：下载/推送/目录 ---

export async function DownloadPhoneModel(modelID: string): Promise<void> {
  await PhoneModelService.DownloadPhoneModel(modelID)
}

export async function GetLocalModelNames(): Promise<string[]> {
  return (await PhoneModelService.GetLocalModelNames() as string[] || [])
}

export async function PushPhoneModel(deviceIP: string, modelName: string): Promise<void> {
  await PhoneModelService.PushPhoneModel(deviceIP, modelName)
}

export async function OpenModelDir(): Promise<void> {
  await PhoneModelService.OpenModelDir()
}

export async function OpenEditedDir(): Promise<void> {
  await PhoneModelService.OpenEditedDir()
}

export async function EditPhoneModel(sourceName: string, paramsJSON: string): Promise<void> {
  await PhoneModelService.EditPhoneModel(sourceName, paramsJSON)
}

export async function GetModelCfg(modelName: string): Promise<string> {
  return await PhoneModelService.GetModelCfg(modelName) as string
}

export async function GetModelDeviceInfo(modelName: string): Promise<string> {
  return await PhoneModelService.GetModelDeviceInfo(modelName) as string
}

export interface AIChatResponse {
  content: string
  paramsJSON: string
}

export async function AIChat(modelName: string, message: string): Promise<AIChatResponse> {
  const req = JSON.stringify({ modelName, message })
  const result = await AIService.AIChat(req)
  return result as AIChatResponse
}
