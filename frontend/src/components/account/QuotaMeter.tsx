import { Meter, Tooltip } from '@heroui/react'
import type { AccountQuota, AccountQuotaWindow } from '@/api/types'
import {
  formatQuotaAmount,
  quotaExpiryLabel,
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
  provider?: string
}

function extraQuotaLines(quota: AccountQuota, remainingLabel: string, addOnLabel: string, resourcePackageLabel: string, t: Translate, provider?: string) {
  const addOn = quota.has_add_on && quota.add_on_available !== false
    ? `${addOnLabel} ${formatQuotaAmount(quota.add_on_used)} / ${formatQuotaAmount(quota.add_on_total)} ${quota.add_on_unit || 'credits'}`
    : ''
  const resourcePackage = quota.has_resource_package && quota.resource_package_available !== false
    ? `${resourcePackageLabel} ${remainingLabel} ${formatQuotaAmount(quota.resource_package_remaining)} ${quota.resource_package_unit || 'credits'}`
    : ''
  // Package-expiry is a WorkBuddy-only surface; never render it for other
  // providers even if a stale snapshot happens to carry the fields.
  const expiry = provider === 'workbuddy' ? quotaExpiryLabel(quota, t) : ''
  return [addOn, resourcePackage, expiry].filter(Boolean)
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
  const ratio = quotaUsedRatio(window)
  const tone = quotaTone(window)
  const color = tone === 'danger' ? 'danger' : tone === 'warn' ? 'warning' : 'success'

  return (
    <Tooltip>
      <Tooltip.Trigger className="block w-full min-w-0">
        <div className="w-full min-w-0 cursor-help">
          <Meter
            className="account-meter account-meter--window w-full"
            color={color}
            size="md"
            minValue={0}
            maxValue={100}
            value={Math.round(ratio * 100)}
            aria-label={label}
            valueLabel={valueLabel}
          >
            <Meter.Output className="flex w-full items-baseline justify-between gap-3 text-[11px]">
              <span className="min-w-0 truncate font-medium text-foreground">{label}</span>
              <span className="mono shrink-0 text-foreground/70">{usedText}</span>
            </Meter.Output>
            <Meter.Track>
              <Meter.Fill />
            </Meter.Track>
          </Meter>
          {reset ? <div className="mt-1 text-[10px] leading-4 text-muted">{reset}</div> : null}
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

// Trae-style quota block: the remaining count is the headline number; the
// used / total pair plus unit sits underneath as a secondary mono line.
// Devin daily / weekly / monthly windows each occupy a full-width row with
// a titled bar, used percent, and reset copy.
export function QuotaMeter({ quota, t, label, usedLabel, remainingLabel, addOnLabel, resourcePackageLabel, exceededLabel, provider }: Props) {
  const unit = quota.unit || 'credits'
  const used = `${formatQuotaAmount(quota.used)} / ${formatQuotaAmount(quota.total)}`
  const remaining = `${formatQuotaAmount(quota.remaining)}`
  const extra = extraQuotaLines(quota, remainingLabel, addOnLabel, resourcePackageLabel, t, provider)
  const windows = quotaWindows(quota)
  const ratio = quotaUsedRatio(quota)
  const tone = quotaTone(quota)
  const color = tone === 'danger' ? 'danger' : tone === 'warn' ? 'warning' : 'success'
  const valueLabel = `${usedLabel} ${used} ${unit} · ${remainingLabel} ${remaining} ${unit}${extra.length ? ` · ${extra.join(' · ')}` : ''}`

  if (windows.length > 0) {
    return (
      <div className="account-quota-windows flex w-full min-w-0 flex-col gap-3">
        {windows.map((window, index) => (
          <div key={window.id || String(index)} className="w-full min-w-0">
            <WindowMeter
              window={window}
              t={t}
              extra={index === 0 ? extra : undefined}
            />
          </div>
        ))}
      </div>
    )
  }

  return (
    <Tooltip>
      <Tooltip.Trigger>
        <div className="w-full cursor-help">
          <Meter
            className="account-meter w-full"
            color={color}
            size="md"
            minValue={0}
            maxValue={100}
            value={Math.round(ratio * 100)}
            aria-label={label}
            valueLabel={valueLabel}
          >
            <Meter.Output className="flex w-full items-baseline justify-between text-[10px]">
              <span className="text-[13px] font-semibold leading-5 text-foreground tabular-nums">
                {quota.exceeded ? <span className="mr-1.5 text-danger">{exceededLabel}</span> : null}
                {remaining}
                <span className="ml-1 text-[10px] font-normal text-foreground/60">{unit}</span>
              </span>
              <span className="mono text-foreground/55">{usedLabel} {used}</span>
            </Meter.Output>
            <Meter.Track>
              <Meter.Fill />
            </Meter.Track>
          </Meter>
        </div>
      </Tooltip.Trigger>
      <Tooltip.Content>
        <div className="space-y-0.5">
          <div>{usedLabel} {used} {unit}</div>
          <div>{remainingLabel} {remaining} {unit}</div>
          {extra.map((line) => <div key={line}>{line}</div>)}
        </div>
      </Tooltip.Content>
    </Tooltip>
  )
}
