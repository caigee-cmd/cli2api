import type { ReactNode } from 'react'
import { Meter, Tooltip } from '@heroui/react'
import type { AccountQuota, AccountQuotaWindow } from '@/api/types'
import {
  formatQuotaAmount,
  quotaResetLabel,
  quotaTone,
  quotaUsedRatio,
  quotaWindowLabel,
  quotaWindows,
} from '@/lib/account'

type Translate = (key: string, vars?: Record<string, string | number>) => string

type Props = {
  quota: AccountQuota
  t: Translate
  label: string
  usedLabel: string
  remainingLabel: string
  addOnLabel: string
  resourcePackageLabel: string
  exceededLabel: string
}

function extraQuotaLines(quota: AccountQuota, remainingLabel: string, addOnLabel: string, resourcePackageLabel: string) {
  const addOn = quota.has_add_on && quota.add_on_available !== false
    ? `${addOnLabel} ${formatQuotaAmount(quota.add_on_used)} / ${formatQuotaAmount(quota.add_on_total)} ${quota.add_on_unit || 'credits'}`
    : ''
  const resourcePackage = quota.has_resource_package && quota.resource_package_available !== false
    ? `${resourcePackageLabel} ${remainingLabel} ${formatQuotaAmount(quota.resource_package_remaining)} ${quota.resource_package_unit || 'credits'}`
    : ''
  return [addOn, resourcePackage].filter(Boolean)
}

function QuotaBar({
  quota,
  label,
  left,
  right,
  valueLabel,
  extra,
  compact,
}: {
  quota: Pick<AccountQuota, 'percentage' | 'exceeded'>
  label: string
  left: ReactNode
  right: ReactNode
  valueLabel: string
  extra?: string[]
  compact?: boolean
}) {
  const ratio = quotaUsedRatio(quota)
  const tone = quotaTone(quota)
  const color = tone === 'danger' ? 'danger' : tone === 'warn' ? 'warning' : 'success'

  return (
    <Tooltip>
      <Tooltip.Trigger>
        <div className="cursor-help">
          <Meter
            className={compact ? 'account-meter account-meter--window' : 'account-meter'}
            color={color}
            size="md"
            minValue={0}
            maxValue={100}
            value={Math.round(ratio * 100)}
            aria-label={label}
            valueLabel={valueLabel}
          >
            <Meter.Output className="flex w-full items-baseline justify-between text-[10px]">
              {left}
              {right}
            </Meter.Output>
            <Meter.Track>
              <Meter.Fill />
            </Meter.Track>
          </Meter>
        </div>
      </Tooltip.Trigger>
      <Tooltip.Content>
        <div className="space-y-0.5">
          <div>{valueLabel}</div>
          {extra?.map((line) => <div key={line}>{line}</div>)}
        </div>
      </Tooltip.Content>
    </Tooltip>
  )
}

function WindowMeter({
  window,
  t,
  extra,
}: {
  window: AccountQuotaWindow
  t: Translate
  extra?: string[]
}) {
  const percent = Math.round(window.percentage ?? 0)
  const label = quotaWindowLabel(window, t)
  const usedText = t('quotaUsedPercent', { n: percent })
  const reset = quotaResetLabel(window.reset_at, t)
  const valueLabel = `${label} · ${usedText}${reset ? ` · ${reset}` : ''}`

  return (
    <div className="space-y-1">
      <QuotaBar
        quota={window}
        label={label}
        left={<span className="text-[11px] font-medium text-foreground">{label}</span>}
        right={<span className="mono text-foreground/55">{usedText}</span>}
        valueLabel={valueLabel}
        extra={extra}
        compact
      />
      {reset ? <div className="text-[10px] text-muted">{reset}</div> : null}
    </div>
  )
}

// Trae-style quota block: the remaining count is the headline number; the
// used / total pair plus unit sits underneath as a secondary mono line.
// Devin daily/weekly windows render as stacked compact bars with reset copy.
export function QuotaMeter({ quota, t, label, usedLabel, remainingLabel, addOnLabel, resourcePackageLabel, exceededLabel }: Props) {
  const unit = quota.unit || 'credits'
  const used = `${formatQuotaAmount(quota.used)} / ${formatQuotaAmount(quota.total)}`
  const remaining = `${formatQuotaAmount(quota.remaining)}`
  const extra = extraQuotaLines(quota, remainingLabel, addOnLabel, resourcePackageLabel)
  const windows = quotaWindows(quota)

  if (windows.length > 0) {
    return (
      <div className="space-y-2.5">
        {windows.map((window, index) => (
          <WindowMeter
            key={window.id || String(index)}
            window={window}
            t={t}
            extra={index === 0 ? extra : undefined}
          />
        ))}
      </div>
    )
  }

  return (
    <QuotaBar
      quota={quota}
      label={label}
      left={(
        <span className="text-[13px] font-semibold leading-5 text-foreground tabular-nums">
          {quota.exceeded ? <span className="mr-1.5 text-danger">{exceededLabel}</span> : null}
          {remaining}
          <span className="ml-1 text-[10px] font-normal text-foreground/60">{unit}</span>
        </span>
      )}
      right={<span className="mono text-foreground/55">{usedLabel} {used}</span>}
      valueLabel={`${usedLabel} ${used} ${unit} · ${remainingLabel} ${remaining} ${unit}${extra.length ? ` · ${extra.join(' · ')}` : ''}`}
      extra={extra}
    />
  )
}
