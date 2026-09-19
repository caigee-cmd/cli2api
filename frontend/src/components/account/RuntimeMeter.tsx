import { useLayoutEffect, useRef } from 'react'
import gsap from 'gsap'
import { runtimeSegments, runtimeTone, type AccountState } from '@/lib/account'

type Props = {
  state: AccountState
  stateCopy: string
  t: (key: string) => string
}

// Compact horizontal runtime strip. Replaces the previous boxed meter plus
// the separate inFlight / priority / restarts grid: the same signal (state)
// fits in a single row, leaving vertical room for the quota block.
export function RuntimeMeter({ state, stateCopy, t }: Props) {
  const rootRef = useRef<HTMLDivElement>(null)
  const count = runtimeSegments(state)
  const tone = runtimeTone(state)

  useLayoutEffect(() => {
    const root = rootRef.current
    if (!root) return

    const context = gsap.context(() => {
      const segments = gsap.utils.toArray<HTMLElement>('[data-runtime-seg]')
      const filled = segments.filter((_, index) => index < count)
      const empty = segments.filter((_, index) => index >= count)
      const media = gsap.matchMedia()

      media.add('(prefers-reduced-motion: reduce)', () => {
        gsap.set(filled, { scaleY: 1, autoAlpha: 1 })
        gsap.set(empty, { scaleY: 0.32, autoAlpha: 0.45 })
      })

      media.add('(prefers-reduced-motion: no-preference)', () => {
        gsap.set(segments, { transformOrigin: 'center bottom' })
        gsap.to(empty, { scaleY: 0.32, autoAlpha: 0.45, duration: 0.18, ease: 'power2.out', overwrite: true })
        gsap.to(filled, {
          scaleY: 1,
          autoAlpha: 1,
          duration: 0.22,
          ease: 'power2.out',
          stagger: { amount: 0.08, from: 'start' },
          overwrite: true,
        })
      })
    }, root)

    return () => context.revert()
  }, [count, state])

  return (
    <div ref={rootRef} className="flex items-center gap-2">
      <div className="flex items-center gap-1.5 text-[11px] font-medium text-foreground/75">
        <span className="status-dot" data-state={tone === 'muted' ? undefined : tone === 'warn' ? 'warn' : tone} />
        <span>{t('runtimeState')}</span>
      </div>
      <div
        className="runtime-meter flex-1"
        role="meter"
        aria-label={t('runtimeState')}
        aria-valuemin={0}
        aria-valuemax={12}
        aria-valuenow={count}
        aria-valuetext={stateCopy}
      >
        {Array.from({ length: 12 }, (_, index) => (
          <span
            key={index}
            data-runtime-seg
            data-on={index < count ? 'true' : undefined}
            data-tone={index < count ? tone : undefined}
            className="runtime-meter__seg"
          />
        ))}
      </div>
      <span className="mono shrink-0 text-[10px] text-foreground/65">{stateCopy}</span>
    </div>
  )
}
