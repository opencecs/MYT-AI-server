import { useTranslation } from 'react-i18next'
import { isRunnableStatus } from '../../../../utils/containerStatus'
import { AndroidInfo } from '../../../../types/index'

/**
 * 批量勾选闸门（语义降级，2026-11 回滚「勾选源头拦截」）：
 *
 * 关机云机恢复可选/可全选——上传/重启/删除等批量操作有 Go 侧逐台护栏兜底
 * （死容器进 failed 明细，不毒整组），安全性不靠前端勾选闸门。
 * 唯一例外是 **ADB 批量**（BatchDockerExec）：后端无逐台兜底，关机容器混入
 * 会整组报错回退——ActionBar.executeAdb 在执行前用 filterAdbTargets 把被选
 * 集合中的关机容器过滤掉并提示跳过，其余操作一律不过滤。
 */

/** 执行 ADB 批量前过滤被选集合：只保留可操作容器（isRunnableStatus）。 */
export function filterAdbTargets(targets: AndroidInfo[]): AndroidInfo[] {
  return targets.filter(a => isRunnableStatus(a.status))
}

/** 计算被过滤掉的（不可操作）数量：0 表示无跳过。 */
export function countAdbSkipped(targets: AndroidInfo[]): number {
  return targets.length - filterAdbTargets(targets).length
}

/**
 * 跳过提示文案（N 台已关机已跳过）。
 */
export function adbSkippedMessage(
  count: number,
  t: ReturnType<typeof useTranslation>['t'],
): string {
  return t('host:batchSelect.batchAdbSkipped', { count })
}
