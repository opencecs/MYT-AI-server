import { AdbShortcut } from './groupTypes'

/** ADB 快捷命令列表 */
export const ADB_SHORTCUTS: AdbShortcut[] = [
  { label: 'groupcontrol:adbShortcut.launchApp', command: 'am start -n package_name/Activity' },
  { label: 'groupcontrol:adbShortcut.forceStop', command: 'am force-stop package_name' },
  { label: 'groupcontrol:adbShortcut.clearData', command: 'pm clear package_name' },
  { label: 'groupcontrol:adbShortcut.screenshot', command: 'screencap -p /sdcard/screenshot.png' },
  { label: 'groupcontrol:adbShortcut.inputText', command: 'input text "text_content"' },
  { label: 'groupcontrol:adbShortcut.simulateTap', command: 'input tap x y' },
  { label: 'groupcontrol:adbShortcut.simulateSwipe', command: 'input swipe x1 y1 x2 y2' },
  { label: 'groupcontrol:adbShortcut.viewProps', command: 'getprop ro.product.model' },
  { label: 'groupcontrol:adbShortcut.systemSettings', command: 'settings put system key value' },
  { label: 'groupcontrol:adbShortcut.viewMemory', command: 'cat /proc/meminfo | head -3' },
]
