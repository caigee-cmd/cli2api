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
  const [lastSeq, setLastSeq] = useState(0)
  const lastSeqRef = useRef(0)

  useEffect(() => {
    if (typeof document === 'undefined') return
    const onVisibility = () => setVisible(!document.hidden)
    document.addEventListener('visibilitychange', onVisibility)
    return () => document.removeEventListener('visibilitychange', onVisibility)
  }, [])

  useEffect(() => {
    if (!enabled || !visible) return
    let disposed = false
    const controller = new AbortController()
    let backoff = MIN_BACKOFF_MS
    let timer: number | null = null
    setStatus((current) => (current === 'live' ? current : 'connecting'))

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
        // down console does not get hammered.
        await new Promise<void>((resolve) => {
          timer = window.setTimeout(resolve, backoff)
        })
        timer = null
        backoff = Math.min(MAX_BACKOFF_MS, backoff * 2)
      }
    }

    void run()
    return () => {
      disposed = true
      controller.abort()
      if (timer != null) window.clearTimeout(timer)
    }
  }, [enabled, visible])

  return { status, lastSeq }
}
