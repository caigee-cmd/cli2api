import { useEffect, useState } from 'react'
import { fetchLive, type LiveSnapshot } from '@/api/live'

export const LIVE_POLL_MS = 2500
/** Samples kept client-side for the ~60s concurrency sparkline. */
export const LIVE_SAMPLES = Math.round(60_000 / LIVE_POLL_MS)

export type LiveSample = { at: number; value: number }

/**
 * Polls /api/live every 2.5s while the document is visible. Keeps the last
 * good snapshot on error so the strip never flashes back to a placeholder.
 */
export function useLive() {
  const [live, setLive] = useState<LiveSnapshot | null>(null)
  const [samples, setSamples] = useState<LiveSample[]>([])

  useEffect(() => {
    let cancelled = false
    let timer: ReturnType<typeof setInterval> | null = null
    const tick = () => {
      if (document.hidden) return
      void fetchLive()
        .then((data) => {
          if (cancelled) return
          setLive(data)
          setSamples((prev) => [...prev, { at: Date.now(), value: data.in_flight }].slice(-LIVE_SAMPLES))
        })
        .catch(() => { /* keep last good snapshot */ })
    }
    const start = () => {
      if (timer || document.hidden) return
      tick()
      timer = setInterval(tick, LIVE_POLL_MS)
    }
    const stop = () => {
      if (timer) clearInterval(timer)
      timer = null
    }
    const onVisibility = () => (document.hidden ? stop() : start())
    start()
    document.addEventListener('visibilitychange', onVisibility)
    return () => {
      cancelled = true
      stop()
      document.removeEventListener('visibilitychange', onVisibility)
    }
  }, [])

  return { live, samples }
}
