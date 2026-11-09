import { registerCancelHandler } from './useTaskQueue'
import * as SdkApiService from '../../../bindings/github.com/moyunteng/myt/sdkapiservicewrapper'
import * as ImageService from '../../../bindings/github.com/moyunteng/myt/imageservicewrapper'
import type { TaskQueueItem } from './types'

// 注册安卓长任务（copy/import/export/move）的取消处理器，调后端 CancelTaskOp 真正中断执行
// 后端这些方法接收 taskID，内部用可取消的 context，CancelTaskOp 调 cancelFn 中断
registerCancelHandler('copy', (taskId) => {
  SdkApiService.CancelTaskOp(taskId).catch(() => {})
})
registerCancelHandler('importAndroid', (taskId) => {
  SdkApiService.CancelTaskOp(taskId).catch(() => {})
})
registerCancelHandler('exportAndroid', (taskId) => {
  SdkApiService.CancelTaskOp(taskId).catch(() => {})
})
registerCancelHandler('moveAndroid', (taskId) => {
  SdkApiService.CancelTaskOp(taskId).catch(() => {})
})

// 创建云机取消：双通道中断
// 1) CancelTaskOp 断 deviceFetch / PullImageCancelable 在途 HTTP
// 2) 若推本地镜像阶段，CancelAllUploads 断上传 HTTP
registerCancelHandler('create', (taskId, task: TaskQueueItem) => {
  SdkApiService.CancelTaskOp(taskId).catch(() => {})
  const mirrorId = task.mirrorId
  if (mirrorId) ImageService.CancelAllUploads(mirrorId).catch(() => {})
})
