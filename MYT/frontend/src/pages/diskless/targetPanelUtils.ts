import { useRef, useCallback } from 'react'

export function formatMB(mb: number): string {
  if (mb >= 1024) return `${(mb / 1024).toFixed(1)} GB`
  return `${mb} MB`
}

export function formatBytes(bytes: number): string {
  if (bytes >= 1024 * 1024 * 1024 * 1024) return `${(bytes / (1024 * 1024 * 1024 * 1024)).toFixed(1)} TB`
  if (bytes >= 1024 * 1024 * 1024) return `${(bytes / (1024 * 1024 * 1024)).toFixed(1)} GB`
  return `${(bytes / (1024 * 1024)).toFixed(0)} MB`
}

export function nextDiskName(disks: { name: string }[]): string {
  const nums = disks.map(d => { const m = d.name.match(/^disk(\d+)$/); return m ? parseInt(m[1]) : 0 })
  return `disk${nums.length > 0 ? Math.max(...nums) + 1 : 1}`
}

export function storageLabel(path: string): string {
  if (!path) return ''
  const match = path.match(/^([A-Z]:)\\/i)
  return match ? match[1] : path
}

// 可拖拽弹窗 hook
export function useDraggable() {
  const posRef = useRef({ x: 0, y: 0 })
  const dragRef = useRef<{ startX: number; startY: number; origX: number; origY: number } | null>(null)
  const panelRef = useRef<HTMLDivElement>(null)

  const onMouseDown = useCallback((e: React.MouseEvent) => {
    if ((e.target as HTMLElement).closest('button, input, select, textarea')) return
    dragRef.current = { startX: e.clientX, startY: e.clientY, origX: posRef.current.x, origY: posRef.current.y }
    const onMouseMove = (ev: MouseEvent) => {
      if (!dragRef.current) return
      posRef.current = {
        x: dragRef.current.origX + ev.clientX - dragRef.current.startX,
        y: dragRef.current.origY + ev.clientY - dragRef.current.startY,
      }
      if (panelRef.current) {
        panelRef.current.style.transform = `translate(${posRef.current.x}px, ${posRef.current.y}px)`
      }
    }
    const onMouseUp = () => {
      dragRef.current = null
      document.removeEventListener('mousemove', onMouseMove)
      document.removeEventListener('mouseup', onMouseUp)
    }
    document.addEventListener('mousemove', onMouseMove)
    document.addEventListener('mouseup', onMouseUp)
  }, [])

  return { panelRef, onMouseDown }
}
