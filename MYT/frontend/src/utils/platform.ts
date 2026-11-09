const platform = navigator.platform || ''

export const isMac = platform.includes('Mac')
export const isWindows = platform.includes('Win')
