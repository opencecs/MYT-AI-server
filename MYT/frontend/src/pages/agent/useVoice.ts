import { useState, useRef, useCallback, useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import * as MytAgentService from '../../../bindings/github.com/moyunteng/myt/mytagentservice'
import type { AgentConnInfo, ChatMessage } from './agentTypes'

/**
 * TTS 语音播报：监听助手回复完成，调 Go HttpProxyBlob 合成并播放
 */
export function useTTS(connInfo: AgentConnInfo | null) {
  const [ttsEnabled, setTtsEnabled] = useState(false)
  const [ttsLoading, setTtsLoading] = useState(false)
  const audioRef = useRef<HTMLAudioElement | null>(null)
  const enabledRef = useRef(false)
  const startTimeRef = useRef(0)
  const lastMsgIdRef = useRef<string | null>(null)

  const stopTTS = useCallback(() => {
    if (audioRef.current) {
      audioRef.current.pause()
      audioRef.current.src = ''
      audioRef.current.onended = null
      audioRef.current.onerror = null
      audioRef.current = null
    }
    setTtsLoading(false)
  }, [])

  const playSentence = useCallback((text: string) => {
    if (!text.trim() || !connInfo) { setTtsLoading(false); return }
    setTtsLoading(true)
    MytAgentService.HttpProxyBlob('POST', `/api/tts/stream?token=${encodeURIComponent(connInfo.sessionToken)}`, JSON.stringify({ text, voice: '自然女声' }))
      .then(url => {
        const audio = new Audio(url)
        audioRef.current = audio
        audio.onplaying = () => { if (audioRef.current === audio) setTtsLoading(false) }
        audio.onended = () => { if (audioRef.current === audio) audioRef.current = null }
        audio.onerror = () => { if (audioRef.current === audio) { audioRef.current = null; setTtsLoading(false) } }
        audio.play().catch(() => setTtsLoading(false))
      })
      .catch(() => setTtsLoading(false))
  }, [connInfo])

  const toggleTTS = useCallback(() => {
    const next = !enabledRef.current
    enabledRef.current = next
    if (!next) stopTTS()
    if (next) startTimeRef.current = Date.now()
    setTtsEnabled(next)
  }, [stopTTS])

  const watchMessages = useCallback((messages: ChatMessage[]) => {
    if (!enabledRef.current || messages.length === 0) return
    const last = messages[messages.length - 1]
    if (last.role !== 'assistant' || !last.text || last.timestamp < startTimeRef.current) return
    if (last.id === lastMsgIdRef.current) return
    lastMsgIdRef.current = last.id
    stopTTS()
    playSentence(last.text.trim())
  }, [playSentence, stopTTS])

  useEffect(() => () => stopTTS(), [stopTTS])

  return { ttsEnabled, ttsLoading, toggleTTS, stopTTS, watchMessages }
}

/**
 * ASR 语音识别：按住录音 → Go 端 AsrRecognize → 识别结果回填输入框
 * onSend 可选：识别成功后自动发送并清空输入框（按住说话、松开发送）
 */
export function useASR(connInfo: AgentConnInfo | null, onSend?: (text: string) => void) {
  const { t } = useTranslation()
  const [recording, setRecording] = useState(false)
  const recorderRef = useRef<MediaRecorder | null>(null)
  const chunksRef = useRef<Blob[]>([])

  const stopRecording = useCallback(() => {
    if (recorderRef.current && recorderRef.current.state !== 'inactive') {
      recorderRef.current.stop()
    }
    setRecording(false)
  }, [])

  const startRecording = useCallback(async (onResult: (text: string) => void) => {
    if (!connInfo) return
    try {
      console.log('[ASR] requesting microphone...')
      const stream = await navigator.mediaDevices.getUserMedia({ audio: true })
      console.log('[ASR] mic acquired, tracks:', stream.getTracks().map(t => ({ label: t.label, settings: t.getSettings() })))
      const mimeType = MediaRecorder.isTypeSupported('audio/webm') ? 'audio/webm' : ''
      console.log('[ASR] mimeType:', mimeType || 'default')
      const recorder = mimeType ? new MediaRecorder(stream, { mimeType }) : new MediaRecorder(stream)
      chunksRef.current = []
      recorder.ondataavailable = (e) => { if (e.data.size > 0) chunksRef.current.push(e.data) }
      recorder.onstop = async () => {
        stream.getTracks().forEach(t => t.stop())
        const blob = new Blob(chunksRef.current, { type: 'audio/webm' })
        console.log('[ASR] recording stopped, blob size:', blob.size)
        if (blob.size < 1000) { console.log('[ASR] blob too small, abort'); return }
        let audioCtx: AudioContext | null = null
        try {
          const arrayBuf = await blob.arrayBuffer()
          // 不强制 16kHz:Wails WebView2 在强制低采样率下 decodeAudioData 会返回静音。
          // 用默认采样率解码(常见 48kHz),由下方重采样逻辑降到 16kHz。
          audioCtx = new AudioContext()
          try { await audioCtx.resume() } catch { /* */ }
          const decoded = await audioCtx.decodeAudioData(arrayBuf)
          console.log('[ASR] decoded: sr=' + decoded.sampleRate + ' ch=' + decoded.numberOfChannels + ' len=' + decoded.length)
          const pcm = decoded.getChannelData(0)
          // 重采样到 16kHz
          const targetSr = 16000
          const srcSr = decoded.sampleRate
          let pcmResampled: Float32Array
          if (srcSr !== targetSr) {
            console.log('[ASR] resampling', srcSr, '->', targetSr)
            const ratio = srcSr / targetSr
            const newLen = Math.round(pcm.length / ratio)
            pcmResampled = new Float32Array(newLen)
            for (let i = 0; i < newLen; i++) {
              const srcIdx = i * ratio
              const idx0 = Math.floor(srcIdx)
              const idx1 = Math.min(idx0 + 1, pcm.length - 1)
              const frac = srcIdx - idx0
              pcmResampled[i] = pcm[idx0] * (1 - frac) + pcm[idx1] * frac
            }
          } else {
            pcmResampled = pcm
          }
          const pcm16 = new Int16Array(pcmResampled.length)
          for (let i = 0; i < pcmResampled.length; i++) {
            pcm16[i] = Math.max(-32768, Math.min(32767, pcmResampled[i] * 32768))
          }
          console.log('[ASR] PCM16:', pcm16.byteLength, 'bytes')
          // 用 Uint8Array 字节视图做 base64（每采样 2 字节 LE），避免 fromCharCode 栈溢出
          const bytes = new Uint8Array(pcm16.buffer)
          let binary = ''
          for (let i = 0; i < bytes.length; i++) binary += String.fromCharCode(bytes[i])
          const b64 = btoa(binary)
          console.log('[ASR] base64 length:', b64.length)
          const text = await MytAgentService.AsrRecognize(b64)
          console.log('[ASR] result:', text)
          if (text) {
            onResult(text)
            if (onSend) setTimeout(() => { onSend(text); onResult('') }, 0)
          }
        } catch (e) {
          console.error('[ASR] recognition failed:', e)
          alert(t('agent:toast.asrFailed', { error: e instanceof Error ? e.message : String(e) }))
        } finally {
          audioCtx?.close()
        }
      }
      recorder.start()
      recorderRef.current = recorder
      setRecording(true)
    } catch (e) {
      console.error('[ASR] mic access failed:', e)
      alert(t('agent:toast.micDenied'))
    }
  }, [connInfo, t])

  return { recording, startRecording, stopRecording }
}
