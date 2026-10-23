import { dateValueToISO, type DateRangeValue } from '@/components/ui/TimeRangePicker'

export type StatsWindow = 'today' | '1' | '24' | '168' | '720' | 'custom'

export const STATS_WINDOW_LABEL: Record<StatsWindow, string> = {
  today: 'statsWindowToday',
  '1': 'statsWindow1h',
  '24': 'statsWindow24h',
  '168': 'statsWindow7d',
  '720': 'statsWindow30d',
  custom: 'statsWindowCustom',
}

/**
 * Stats query for a window. 30d, today and custom go through from/to: the
 * backend only accepts hours=1|24|168. "today" is a calendar range — local
 * midnight -> now — so it is sent as an explicit `from` (the backend defaults
 * `to` to now and derives the window from the pair).
 */
export function statsQuery(window: StatsWindow, custom: DateRangeValue | null) {
  if (window === 'today') {
    const start = new Date()
    start.setHours(0, 0, 0, 0)
    return { from: start.toISOString() }
  }
  if (window === 'custom') {
    const from = dateValueToISO(custom?.start)
    const to = dateValueToISO(custom?.end, true)
    return from && to ? { from, to } : null
  }
  if (window === '720') {
    const to = new Date()
    return { from: new Date(to.getTime() - 30 * 24 * 3600 * 1000).toISOString(), to: to.toISOString() }
  }
  return { hours: Number(window) }
}
