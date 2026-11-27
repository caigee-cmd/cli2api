import { useEffect, useRef, useState } from 'react'
import { openEventStream, type EventStreamStatus, type ServerEvent } from '@/api/events'

const MIN_BACKOFF_MS = 1000
const MAX_BACKOFF_MS = 15_000

/**
 * Console real-time channel: streams real runtime events from `GET /api/events`.
 * Reconnects with capped exponential backoff, replays from the last seen seq
 * (the bus ring buffers 1024 events), and tears the connection down while the
 * document is hidden so a background tab costs nothing. `status` lets a page
 * degrade to polling exactly when the stream is not live.
 */
export function useAccountEvents(
  onEvent: (event: ServerEvent) => void,
  opts?: { enabled?: boolean },
): { status: EventStreamStatus; lastSeq: number } {
  const handlerRef = useRef(onEvent)
  useEffect(() => {
    handlerRef.current = onEvent
  }, [onEvent])
  const enabled = opts?.enabled !== false
  const [status, setStatus] = useState<EventStreamStatus>('connecting')
  const [visible, setVisible] = useState(() => (typeof document === 'undefined' ? true : !document.hidden))
  const [online, setOnline] = useState(() => (typeof navigator === 'undefined' ? true : navigator.onLine))
  const [lastSeq, setLastSeq] = useState(0)
  const lastSeqRef = useRef(0)

  useEffect(() => {
    if (typeof document === 'undefined') return
    const onVisibility = () => setVisible(!document.hidden)
    document.addEventListener('visibilitychange', onVisibility)
    return () => document.removeEventListener('visibilitychange', onVisibility)
  }, [])

  useEffect(() => {
    if (typeof window === 'undefined') return
    const onOnline = () => {
      setOnline(true)
      setStatus((current) => (current === 'live' ? current : 'connecting'))
    }
    const onOffline = () => setOnline(false)
    window.addEventListener('online', onOnline)
    window.addEventListener('offline', onOffline)
    return () => {
      window.removeEventListener('online', onOnline)
      window.removeEventListener('offline', onOffline)
    }
  }, [])

  useEffect(() => {
    if (!enabled || !visible || !online) return
    let disposed = false
    const controller = new AbortController()
    let backoff = MIN_BACKOFF_MS
    let timer: number | null = null
    let wake: (() => void) | null = null

    const run = async () => {
      while (!disposed) {
        try {
          await openEventStream({
            lastSeq: lastSeqRef.current,
            signal: controller.signal,
            onOpen: () => {
              backoff = MIN_BACKOFF_MS
              if (!disposed) setStatus('live')
            },
            onEvent: (event) => {
              if (event.seq > lastSeqRef.current) {
                lastSeqRef.current = event.seq
                setLastSeq(event.seq)
              } else if (event.seq < lastSeqRef.current) {
                // The server restarted and its bus seq restarted lower. Accept
                // the new baseline so subsequent reconnect IDs replay correctly.
                lastSeqRef.current = event.seq
                setLastSeq(event.seq)
              }
              if (!disposed) handlerRef.current(event)
            },
          })
          if (disposed) break
          setStatus('offline')
        } catch {
          if (disposed || controller.signal.aborted) break
          setStatus('offline')
        }
        // Wait before reconnecting; the delay doubles until the cap so a
        // down console does not get hammered. A small positive jitter keeps
        // several open tabs from reconnecting in the same instant.
        await new Promise<void>((resolve) => {
          wake = resolve
          const delay = backoff + Math.floor(Math.random() * Math.max(250, backoff * 0.25))
          timer = window.setTimeout(() => {
            wake = null
            resolve()
          }, delay)
        })
        wake = null
        timer = null
        backoff = Math.min(MAX_BACKOFF_MS, backoff * 2)
      }
    }

    void run()
    return () => {
      disposed = true
      controller.abort()
      if (timer != null) window.clearTimeout(timer)
      wake?.()
    }
  }, [enabled, visible, online])

  return { status: online ? status : 'offline', lastSeq }
}
