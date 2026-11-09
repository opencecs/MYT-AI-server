import { useState, useEffect, useCallback } from 'react'

export type ToastType = 'success' | 'error' | 'warning'

interface ToastState {
  message: string
  type: ToastType
}

let showToastFn: (message: string, type: ToastType) => void = () => {}

export function showToast(message: string, type: ToastType = 'success') {
  showToastFn(message, type)
}

export default function Toast() {
  const [toast, setToast] = useState<ToastState | null>(null)

  showToastFn = useCallback((message: string, type: ToastType) => {
    setToast({ message, type })
  }, [])

  useEffect(() => {
    if (!toast) return
    const t = setTimeout(() => setToast(null), toast.type === 'warning' ? 4000 : 2000)
    return () => clearTimeout(t)
  }, [toast])

  if (!toast) return null

  const bgMap: Record<ToastType, string> = {
    success: 'bg-green-500',
    error: 'bg-red-500',
    warning: 'bg-amber-500',
  }
  const bg = bgMap[toast.type]

  return (
    <div className="fixed top-6 left-1/2 -translate-x-1/2 z-[100]" role="status" aria-live="polite">
      <div className={`${bg} text-white text-xs px-4 py-2 rounded-lg shadow-lg animate-fade-in`}>
        {toast.message}
      </div>
    </div>
  )
}
