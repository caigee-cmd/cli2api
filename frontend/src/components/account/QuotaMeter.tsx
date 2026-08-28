import { useLayoutEffect, useRef } from 'react'
import gsap from 'gsap'
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
  const rootRef = useRef<HTMLDivElement>(null)
  const fillRef = useRef<HTMLSpanElement>(null)
  const remainingRef = useRef<HTMLSpanElement>(null)
  const remainingValueRef = useRef({ remaining: quota.remaining ?? 0, total: quota.total ?? 0 })
  const primedRef = useRef(false)
  const animateRef = useRef<((ratio: number, remaining: number, total: number) => void) | null>(null)
  const ratio = quotaUsedRatio(quota)
  const tone = quotaTone(quota)
  const remaining = `${formatQuotaAmount(quota.remaining)} / ${formatQuotaAmount(quota.total)} ${quota.unit || 'credits'}`

  useLayoutEffect(() => {
    const root = rootRef.current
    const fill = fillRef.current
    if (!root || !fill) return

    const context = gsap.context(() => {
      const media = gsap.matchMedia()
      media.add({ all: 'all', reduceMotion: '(prefers-reduced-motion: reduce)' }, (match) => {
        const reduceMotion = Boolean(match.conditions?.reduceMotion)
        gsap.set(fill, { transformOrigin: 'left center' })
        animateRef.current = (nextRatio, remainingValue, totalValue) => {
          const duration = reduceMotion ? 0 : primedRef.current ? 0.28 : 0.32
          if (!primedRef.current) {
            gsap.fromTo(fill, { scaleX: 0, autoAlpha: 0.72 }, { scaleX: nextRatio, autoAlpha: 1, duration, ease: 'power2.out', overwrite: true })
          } else {
            gsap.to(fill, { scaleX: nextRatio, autoAlpha: 1, duration, ease: 'power2.out', overwrite: true })
          }
          if (remainingRef.current) {
            if (reduceMotion) {
              remainingValueRef.current = { remaining: remainingValue, total: totalValue }
              remainingRef.current.textContent = `${formatQuotaAmount(remainingValue)} / ${formatQuotaAmount(totalValue)}`
            } else {
              gsap.to(remainingValueRef.current, {
                remaining: remainingValue,
                total: totalValue,
                duration,
                ease: 'power2.out',
                overwrite: true,
                onUpdate: () => {
                  if (!remainingRef.current) return
                  remainingRef.current.textContent = `${formatQuotaAmount(remainingValueRef.current.remaining)} / ${formatQuotaAmount(remainingValueRef.current.total)}`
                },
              })
            }
          }
          primedRef.current = true
        }
        return () => {
          animateRef.current = null
          gsap.killTweensOf(fill)
          gsap.killTweensOf(remainingValueRef.current)
        }
      })
    }, root)

    return () => {
      animateRef.current = null
      context.revert()
    }
  }, [])

  useLayoutEffect(() => {
    animateRef.current?.(ratio, quota.remaining ?? 0, quota.total ?? 0)
  }, [quota.remaining, quota.total, ratio, tone])

  return (
    <div ref={rootRef} className="account-meter">
      <div className="flex items-center justify-between gap-3 text-[11px]">
        <div className="flex min-w-0 items-center gap-1.5 font-medium">
          <span>{label}</span>
          {quota.exceeded ? <span className="text-[var(--app-danger)]">{exceededLabel}</span> : null}
        </div>
        <span className="mono truncate text-[10px] leading-4 text-[var(--app-faint)]">
          <span ref={remainingRef}>{formatQuotaAmount(quota.remaining)} / {formatQuotaAmount(quota.total)}</span>
          {` ${quota.unit || 'credits'}`}
          {quota.has_add_on ? (
            <span>
              {' · '}{addOnLabel} {formatQuotaAmount(quota.add_on_used)} / {formatQuotaAmount(quota.add_on_total)} {quota.add_on_unit || 'credits'}
            </span>
          ) : null}
        </span>
      </div>
      <div
        className="quota-meter"
        role="progressbar"
        aria-label={label}
        aria-valuenow={quota.percentage ?? 0}
        aria-valuemin={0}
        aria-valuemax={100}
        aria-valuetext={`${remainingLabel} ${remaining}`}
      >
        <span ref={fillRef} className="quota-meter__fill" data-tone={tone} />
      </div>
    </div>
  )
}
