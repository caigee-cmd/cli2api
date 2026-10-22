import { FilterToggle } from '@/components/ui/FilterToggle'
import { TimeRangePicker, type DateRangeValue } from '@/components/ui/TimeRangePicker'
import { STATS_WINDOW_LABEL, type StatsWindow } from '@/lib/statsWindow'

/**
 * 1h / 24h / 7d / 30d / custom window toggle plus the custom from/to picker,
 * shared by the overview and usage pages. onBusy fires when a change will
 * trigger a new load (so the caller can show its skeleton).
 */
export function StatsWindowControls({
  value,
  onChange,
  custom,
  onCustomChange,
  onBusy,
  t,
}: {
  value: StatsWindow
  onChange: (next: StatsWindow) => void
  custom: DateRangeValue | null
  onCustomChange: (next: DateRangeValue | null) => void
  onBusy?: () => void
  t: (key: string) => string
}) {
  return (
    <>
      <FilterToggle
        value={value}
        onChange={(next) => {
          const window = next as StatsWindow
          if (value === window) return
          if (window !== 'custom' || custom) onBusy?.()
          onChange(window)
        }}
        ariaLabel={t('statsTraffic')}
        options={(Object.keys(STATS_WINDOW_LABEL) as StatsWindow[]).map((id) => ({ id, label: t(STATS_WINDOW_LABEL[id]) }))}
      />
      {value === 'custom' ? (
        <TimeRangePicker
          className="w-[min(100%,24rem)]"
          value={custom}
          onChange={(next) => {
            if (next) onBusy?.()
            onCustomChange(next)
          }}
          ariaLabel={t('statsWindowCustom')}
          fromLabel={t('logsTimeFrom')}
          toLabel={t('logsTimeTo')}
        />
      ) : null}
    </>
  )
}
