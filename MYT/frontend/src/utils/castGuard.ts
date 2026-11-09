/**
 * 投屏关闭后的杂散点击守卫。
 *
 * 背景：截图卡片是「单击即投屏」（clickToCast）。用户关闭叠在云机页面上方的
 * 投屏窗口（标题栏 X）后，紧随的第二次点击（双击习惯/关窗后的快速连点）会落到
 * 刚暴露出来的下层截图卡片上，误开那个云机的投屏——用户看到的是"点了关闭却
 * 又开了一个"。本地模式不中招：本地播放器关窗要清理 RTSP/D3D11，窗口滞留
 * 数百 ms 把杂散点击吸收掉了；公网 WebView2 模式关窗瞬间完成。
 *
 * 机制：Go 端投屏退出时发射 screencast:exited（screencast_service.go 的
 * SetOnProjectionExit），ScreenshotView 订阅并 markCastExit 记录时刻；
 * ScreenshotCard 的 clickToCast 点击在 GUARD_MS 内被丢弃。守卫窗口极短
 * （500ms），对正常操作无感（关窗后刻意点卡片的间隔远大于此）。
 */
let lastExitAt = 0

/** GUARD_MS 关窗后忽略卡片点击的窗口时长 */
const GUARD_MS = 500

/** screencast:exited 到达时调用：记录投屏退出时刻 */
export function markCastExit(): void {
  lastExitAt = Date.now()
}

/** 距最近一次投屏退出不足 GUARD_MS 时返回 true——此时的卡片点击视为杂散，丢弃 */
export function castClickBlocked(): boolean {
  return Date.now() - lastExitAt < GUARD_MS
}
