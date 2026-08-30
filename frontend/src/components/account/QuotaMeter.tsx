import { Label, Meter } from '@heroui/react'
import type { AccountQuota } from '@/api/types'
import { formatQuotaAmount, quotaTone, quotaUsedRatio } from '@/lib/account'

type Props = {
  quota: AccountQuota
  label: string
  remainingLabel: string
  addOnLabel: string
  exceededLabel: string
}

export function QuotaMeter({ quota, label, remainingLabel, addOnLabel, exceededLabel }: Props) {
  const ratio = quotaUsedRatio(quota)
  const tone = quotaTone(quota)
  const color = tone === 'danger' ? 'danger' : tone === 'warn' ? 'warning' : 'success'
  const remaining = `${formatQuotaAmount(quota.remaining)} / ${formatQuotaAmount(quota.total)} ${quota.unit || 'credits'}`
  const addOn = quota.has_add_on
    ? `${addOnLabel} ${formatQuotaAmount(quota.add_on_used)} / ${formatQuotaAmount(quota.add_on_total)} ${quota.add_on_unit || 'credits'}`
    : ''

  return (
    <Meter
      className="account-meter"
      color={color}
      size="sm"
      minValue={0}
      maxValue={100}
      value={Math.round(ratio * 100)}
      aria-label={label}
      valueLabel={`${remainingLabel} ${remaining}${addOn ? ` · ${addOn}` : ''}`}
    >
      <Label className="text-[11px] font-medium">
        {label}
        {quota.exceeded ? <span className="ml-1.5 text-danger">{exceededLabel}</span> : null}
      </Label>
      <Meter.Output className="mono text-[10px] text-muted">
        {remaining}
        {addOn ? ` · ${addOn}` : ''}
      </Meter.Output>
      <Meter.Track>
        <Meter.Fill />
      </Meter.Track>
    </Meter>
  )
}
