import { Meter, Tooltip } from '@heroui/react'
import type { AccountQuota } from '@/api/types'
import { formatQuotaAmount, quotaTone, quotaUsedRatio } from '@/lib/account'

type Props = {
  quota: AccountQuota
  label: string
  usedLabel: string
  remainingLabel: string
  addOnLabel: string
  resourcePackageLabel: string
  exceededLabel: string
}

// Compact quota row: drops the standalone "额度" label so long unit strings
// (e.g. Trae's "entitlement_pack") do not squeeze the bar. The on-screen
// readout is a short mono `used / total`; the full breakdown (unit, add-on,
// resource package) lives in the tooltip.
export function QuotaMeter({ quota, label, usedLabel, remainingLabel, addOnLabel, resourcePackageLabel, exceededLabel }: Props) {
  const ratio = quotaUsedRatio(quota)
  const tone = quotaTone(quota)
  const color = tone === 'danger' ? 'danger' : tone === 'warn' ? 'warning' : 'success'
  const unit = quota.unit || 'credits'
  const used = `${formatQuotaAmount(quota.used)} / ${formatQuotaAmount(quota.total)}`
  const remaining = `${formatQuotaAmount(quota.remaining)} ${unit}`
  const addOn = quota.has_add_on && quota.add_on_available !== false
    ? `${addOnLabel} ${formatQuotaAmount(quota.add_on_used)} / ${formatQuotaAmount(quota.add_on_total)} ${quota.add_on_unit || 'credits'}`
    : ''
  const resourcePackage = quota.has_resource_package && quota.resource_package_available !== false
    ? `${resourcePackageLabel} ${remainingLabel} ${formatQuotaAmount(quota.resource_package_remaining)} ${quota.resource_package_unit || 'credits'}`
    : ''

  const tooltip = (
    <div className="space-y-0.5">
      <div>{usedLabel} {formatQuotaAmount(quota.used)} / {formatQuotaAmount(quota.total)} {unit}</div>
      <div>{remainingLabel} {remaining}</div>
      {addOn ? <div>{addOn}</div> : null}
      {resourcePackage ? <div>{resourcePackage}</div> : null}
    </div>
  )

  return (
    <Tooltip>
      <Tooltip.Trigger>
        <span className="block cursor-help">
          <Meter
            className="account-meter"
            color={color}
            size="md"
            minValue={0}
            maxValue={100}
            value={Math.round(ratio * 100)}
            aria-label={label}
            valueLabel={`${usedLabel} ${used} · ${remainingLabel} ${remaining}${addOn ? ` · ${addOn}` : ''}${resourcePackage ? ` · ${resourcePackage}` : ''}`}
          >
            <Meter.Output className="mono text-[10px] text-foreground/65">
              {quota.exceeded ? <span className="mr-1.5 text-danger">{exceededLabel}</span> : null}
              {usedLabel} {used}
            </Meter.Output>
            <Meter.Track>
              <Meter.Fill />
            </Meter.Track>
          </Meter>
        </span>
      </Tooltip.Trigger>
      <Tooltip.Content>{tooltip}</Tooltip.Content>
    </Tooltip>
  )
}
