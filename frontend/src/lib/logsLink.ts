/**
 * /logs link with filters for a window. The logs picker is minute-granular and
 * rounds the end up to :59.999, so the end is passed as the minute before its
 * ceiling to keep the round trip exact.
 */
export function logsHref(filters: Record<string, string | undefined>, from: string, to: string) {
  const params = new URLSearchParams()
  for (const [key, value] of Object.entries(filters)) if (value) params.set(key, value)
  const fromMs = Date.parse(from)
  const toMs = Date.parse(to)
  if (Number.isFinite(fromMs) && Number.isFinite(toMs)) {
    params.set('from', new Date(Math.floor(fromMs / 60_000) * 60_000).toISOString())
    params.set('to', new Date(Math.ceil(toMs / 60_000) * 60_000 - 60_000).toISOString())
  }
  return `/logs?${params}`
}
