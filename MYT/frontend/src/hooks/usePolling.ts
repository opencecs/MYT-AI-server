import { useEffect, useRef, useCallback } from 'react'

export function usePolling(callback: () => void, intervalMs: number) {
  const savedCallback = useRef(callback)

  useEffect(() => {
    savedCallback.current = callback
  }, [callback])

  useEffect(() => {
    const tick = () => savedCallback.current()
    const id = setInterval(tick, intervalMs)
    return () => clearInterval(id)
  }, [intervalMs])
}

export function usePollingWithInit(callback: () => void, intervalMs: number) {
  const initRef = useRef(false)

  usePolling(callback, intervalMs)

  const runInit = useCallback(() => {
    if (!initRef.current) {
      initRef.current = true
      callback()
    }
  }, [callback])

  useEffect(() => {
    runInit()
  }, [runInit])
}
