import { useLayoutEffect, useRef } from 'react'
import { duration, ease, gsap } from '@/lib/motion'
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
        gsap.to(empty, { scaleY: 0.32, autoAlpha: 0.45, duration: duration.fast, ease: ease.out, overwrite: true })
        gsap.to(filled, {
          scaleY: 1,
          autoAlpha: 1,
          duration: duration.base,
          ease: ease.out,
          stagger: { amount: 0.08, from: 'start' },
          overwrite: true,
        })
      })
    }, root)

    return () => context.revert()
  }, [count, state])

  // The state Chip in the card header already names the state, so the strip
  // carries no label of its own. A healthy account renders it at low contrast;
  // warning and danger tones stay at full colour so problems stand out.
  return (
    <div ref={rootRef} className="flex items-center">
      <div
        className="runtime-meter min-w-[80px] flex-1"
        data-quiet={tone === 'ok' ? 'true' : undefined}
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
    </div>
  )
}
