import { useEffect, useState } from 'react'
import { fetchLive, type LiveSnapshot } from '@/api/live'

export const LIVE_POLL_MS = 2500
/** Samples kept client-side for the ~60s concurrency sparkline. */
export const LIVE_SAMPLES = Math.round(60_000 / LIVE_POLL_MS)

export type LiveSample = { at: number; value: number }

/** Faster poll while InflightLiveCard is expanded. 800ms still gives two
 *  samples across the ~1.6s select-dwell window without paying for 150
 *  account-list reads per minute. */
export const LIVE_POLL_EXPANDED_MS = 800

/**
 * Polls /api/live while the document is visible. Keeps the last good snapshot
 * on error so the strip never flashes back to a placeholder.
 * Pass a shorter `intervalMs` while the inflight card is open so selecting /
 * queue→slot handoffs are not missed between the default 2.5s ticks.
 */
export function useLive(opts?: { intervalMs?: number }) {
  const intervalMs = opts?.intervalMs ?? LIVE_POLL_MS
  const [live, setLive] = useState<LiveSnapshot | null>(null)
  const [error, setError] = useState<string | null>(null)
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
          setError(null)
          // Keep ~60s of sparkline history even when expanded poll is faster.
          const keep = Math.max(LIVE_SAMPLES, Math.round(60_000 / intervalMs))
          setSamples((prev) => [...prev, { at: Date.now(), value: data.in_flight }].slice(-keep))
        })
        .catch((err: unknown) => {
          // Keep the last good snapshot, but surface the real failure instead of
          // pretending the pool is idle.
          if (cancelled) return
          setError(err instanceof Error ? err.message : String(err))
        })
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

  return { live, samples, error }
}
