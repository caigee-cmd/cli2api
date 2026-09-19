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

// Trae-style quota block: the remaining count is the headline number; the
// used / total pair plus unit sits underneath as a secondary mono line.
// Long unit strings (Trae's "entitlement_pack") stay on the sub-line so
// they never squeeze the bar. Add-on and resource-package detail lives in
// the tooltip.
export function QuotaMeter({ quota, label, usedLabel, remainingLabel, addOnLabel, resourcePackageLabel, exceededLabel }: Props) {
  const ratio = quotaUsedRatio(quota)
  const tone = quotaTone(quota)
  const color = tone === 'danger' ? 'danger' : tone === 'warn' ? 'warning' : 'success'
  const unit = quota.unit || 'credits'
  const used = `${formatQuotaAmount(quota.used)} / ${formatQuotaAmount(quota.total)}`
  const remaining = `${formatQuotaAmount(quota.remaining)}`
  const addOn = quota.has_add_on && quota.add_on_available !== false
    ? `${addOnLabel} ${formatQuotaAmount(quota.add_on_used)} / ${formatQuotaAmount(quota.add_on_total)} ${quota.add_on_unit || 'credits'}`
    : ''
  const resourcePackage = quota.has_resource_package && quota.resource_package_available !== false
    ? `${resourcePackageLabel} ${remainingLabel} ${formatQuotaAmount(quota.resource_package_remaining)} ${quota.resource_package_unit || 'credits'}`
    : ''

  const tooltip = (
    <div className="space-y-0.5">
      <div>{usedLabel} {formatQuotaAmount(quota.used)} / {formatQuotaAmount(quota.total)} {unit}</div>
      <div>{remainingLabel} {formatQuotaAmount(quota.remaining)} {unit}</div>
      {addOn ? <div>{addOn}</div> : null}
      {resourcePackage ? <div>{resourcePackage}</div> : null}
    </div>
  )

  return (
    <Tooltip>
      <Tooltip.Trigger>
        <div className="cursor-help">
          <Meter
            className="account-meter"
            color={color}
            size="md"
            minValue={0}
            maxValue={100}
            value={Math.round(ratio * 100)}
            aria-label={label}
            valueLabel={`${usedLabel} ${used} ${unit} · ${remainingLabel} ${remaining} ${unit}${addOn ? ` · ${addOn}` : ''}${resourcePackage ? ` · ${resourcePackage}` : ''}`}
          >
            <Meter.Output className="flex w-full items-baseline justify-between text-[10px]">
              <span className="text-[13px] font-semibold leading-5 text-foreground tabular-nums">
                {quota.exceeded ? <span className="mr-1.5 text-danger">{exceededLabel}</span> : null}
                {remaining}
                <span className="ml-1 text-[10px] font-normal text-foreground/60">{unit}</span>
              </span>
              <span className="mono text-foreground/55">
                {usedLabel} {used}
              </span>
            </Meter.Output>
            <Meter.Track>
              <Meter.Fill />
            </Meter.Track>
          </Meter>
        </div>
      </Tooltip.Trigger>
      <Tooltip.Content>{tooltip}</Tooltip.Content>
    </Tooltip>
  )
}
