import { useEffect, useState } from 'react'
import { fetchLive, type LiveSnapshot } from '@/api/live'

export const LIVE_POLL_MS = 2500
/** Samples kept client-side for the ~60s concurrency sparkline. */
export const LIVE_SAMPLES = Math.round(60_000 / LIVE_POLL_MS)

export type LiveSample = { at: number; value: number }

/** Faster poll while InflightLiveCard is expanded — selecting dwell is ~1.6s. */
export const LIVE_POLL_EXPANDED_MS = 400

/**
 * Polls /api/live while the document is visible. Keeps the last good snapshot
 * on error so the strip never flashes back to a placeholder.
 * Pass a shorter `intervalMs` while the inflight card is open so selecting /
 * queue→slot handoffs are not missed between the default 2.5s ticks.
 */
export function useLive(opts?: { intervalMs?: number }) {
  const intervalMs = opts?.intervalMs ?? LIVE_POLL_MS
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
          // Keep ~60s of sparkline history even when expanded poll is faster.
          const keep = Math.max(LIVE_SAMPLES, Math.round(60_000 / intervalMs))
          setSamples((prev) => [...prev, { at: Date.now(), value: data.in_flight }].slice(-keep))
        })
        .catch(() => { /* keep last good snapshot */ })
    }
    const start = () => {
      if (timer || document.hidden) return
      tick()
      timer = setInterval(tick, intervalMs)
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
  }, [intervalMs])

  return { live, samples }
}
