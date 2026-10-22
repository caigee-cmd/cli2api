import { getLocalTimeZone, parseAbsoluteToLocal, toCalendarDateTime, type DateValue } from '@internationalized/date'
import {
  DateField,
  DateRangePicker,
  Label,
  RangeCalendar,
  TimeField,
  type TimeValue,
} from '@heroui/react'

export type DateRangeValue = { start: DateValue; end: DateValue }

/** DateValue (local wall time) → ISO instant; endOfMinute rounds the end up to :59.999. */
export function dateValueToISO(value: DateValue | null | undefined, endOfMinute = false) {
  if (!value) return undefined
  const date = value.toDate(getLocalTimeZone())
  if (!Number.isFinite(date.getTime())) return undefined
  if (endOfMinute) date.setSeconds(59, 999)
  return date.toISOString()
}

/** ISO instant → local CalendarDateTime for the picker; null when unparsable. */
export function isoToDateValue(iso: string | null | undefined): DateValue | null {
  if (!iso) return null
  const ms = Date.parse(iso)
  if (!Number.isFinite(ms)) return null
  try {
    return toCalendarDateTime(parseAbsoluteToLocal(new Date(ms).toISOString()))
  } catch {
    return null
  }
}

/** Minute-granularity from/to range picker shared by the logs and overview pages. */
export function TimeRangePicker({
  value,
  onChange,
  ariaLabel,
  fromLabel,
  toLabel,
  className,
}: {
  value: DateRangeValue | null
  onChange: (next: DateRangeValue | null) => void
  ariaLabel: string
  fromLabel: string
  toLabel: string
  className?: string
}) {
  return (
    <DateRangePicker
      className={className}
      granularity="minute"
      hourCycle={24}
      shouldForceLeadingZeros
      value={value}
      onChange={(next) => onChange(next)}
      aria-label={ariaLabel}
    >
      {({ state }) => (
        <>
          <DateField.Group className="w-full min-w-0" variant="secondary">
            <DateField.Input className="min-w-0" slot="start" aria-label={fromLabel}>
              {(segment) => <DateField.Segment segment={segment} />}
            </DateField.Input>
            <DateRangePicker.RangeSeparator />
            <DateField.Input className="min-w-0" slot="end" aria-label={toLabel}>
              {(segment) => <DateField.Segment segment={segment} />}
            </DateField.Input>
            <DateField.Suffix>
              <DateRangePicker.Trigger>
                <DateRangePicker.TriggerIndicator />
              </DateRangePicker.Trigger>
            </DateField.Suffix>
          </DateField.Group>
          <DateRangePicker.Popover className="flex w-[20.5rem] max-w-[calc(100vw-2rem)] flex-col gap-3 p-3">
            <RangeCalendar aria-label={ariaLabel}>
              <RangeCalendar.Header>
                <RangeCalendar.YearPickerTrigger>
                  <RangeCalendar.YearPickerTriggerHeading />
                  <RangeCalendar.YearPickerTriggerIndicator />
                </RangeCalendar.YearPickerTrigger>
                <RangeCalendar.NavButton slot="previous" />
                <RangeCalendar.NavButton slot="next" />
              </RangeCalendar.Header>
              <RangeCalendar.Grid>
                <RangeCalendar.GridHeader>
                  {(day) => (
                    <RangeCalendar.HeaderCell>{day}</RangeCalendar.HeaderCell>
                  )}
                </RangeCalendar.GridHeader>
                <RangeCalendar.GridBody>
                  {(date) => <RangeCalendar.Cell date={date} />}
                </RangeCalendar.GridBody>
              </RangeCalendar.Grid>
              <RangeCalendar.YearPickerGrid>
                <RangeCalendar.YearPickerGridBody>
                  {({ year }) => <RangeCalendar.YearPickerCell year={year} />}
                </RangeCalendar.YearPickerGridBody>
              </RangeCalendar.YearPickerGrid>
            </RangeCalendar>
            <div className="flex flex-col gap-3">
              <div className="flex items-center justify-between gap-3">
                <Label className="text-xs">{fromLabel}</Label>
                <TimeField
                  aria-label={fromLabel}
                  granularity="minute"
                  hourCycle={24}
                  value={state.timeRange?.start ?? null}
                  onChange={(value) =>
                    state.setTimeRange({
                      start: value as TimeValue,
                      end: state.timeRange?.end as TimeValue,
                    })
                  }
                >
                  <TimeField.Group className="w-[7.5rem]" variant="secondary">
                    <TimeField.Input>
                      {(segment) => <TimeField.Segment segment={segment} />}
                    </TimeField.Input>
                  </TimeField.Group>
                </TimeField>
              </div>
              <div className="flex items-center justify-between gap-3">
                <Label className="text-xs">{toLabel}</Label>
                <TimeField
                  aria-label={toLabel}
                  granularity="minute"
                  hourCycle={24}
                  value={state.timeRange?.end ?? null}
                  onChange={(value) =>
                    state.setTimeRange({
                      start: state.timeRange?.start as TimeValue,
                      end: value as TimeValue,
                    })
                  }
                >
                  <TimeField.Group className="w-[7.5rem]" variant="secondary">
                    <TimeField.Input>
                      {(segment) => <TimeField.Segment segment={segment} />}
                    </TimeField.Input>
                  </TimeField.Group>
                </TimeField>
              </div>
            </div>
          </DateRangePicker.Popover>
        </>
      )}
    </DateRangePicker>
  )
}
