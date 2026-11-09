// Wails custom.js - 桌面模式扩展
// 全局未捕获异常上报
window.addEventListener('error', (e) => {
  console.error('[global]', e.message, e.filename + ':' + e.lineno)
})
window.addEventListener('unhandledrejection', (e) => {
  console.error('[global] unhandled rejection:', e.reason)
})
