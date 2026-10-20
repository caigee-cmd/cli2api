import { useLayoutEffect, useRef } from 'react'
import { formatCountKind } from '@/lib/format'
import { duration, ease, gsap } from '@/lib/motion'

// Rolls from the previously shown value to the new one (from 0 on first mount),
// and nudges in from the side it moved toward, after arc's animated-counter /
// metric-card: a larger value rises from below, a smaller one drops from above.
export function CountUp({
  value,
  kind = 'int',
  className,
}: {
  value: number
  kind?: 'int' | 'compact' | 'percent' | 'ms'
  className?: string
}) {
  const ref = useRef<HTMLSpanElement>(null)
  const shown = useRef(0)
  const mounted = useRef(false)

  useLayoutEffect(() => {
    const el = ref.current
    if (!el) return
    const from = shown.current
    const isUpdate = mounted.current
    mounted.current = true
    const state = { value: from }
    const context = gsap.context(() => {
      const media = gsap.matchMedia()
      media.add('(prefers-reduced-motion: reduce)', () => {
        shown.current = value
        el.textContent = formatCountKind(value, kind)
      })
      media.add('(prefers-reduced-motion: no-preference)', () => {
        gsap.to(state, {
          value,
          duration: isUpdate ? duration.calm : duration.slow,
          ease: ease.out,
          onUpdate: () => {
            shown.current = state.value
            el.textContent = formatCountKind(state.value, kind)
          },
        })
        if (isUpdate && value !== from) {
          gsap.fromTo(el, { y: value > from ? 6 : -6, autoAlpha: 0.4 }, { y: 0, autoAlpha: 1, duration: duration.base, ease: ease.out })
        }
      })
    }, el)
    return () => context.revert()
  }, [kind, value])

  return <span ref={ref} className={`inline-block ${className ?? ''}`}>{formatCountKind(0, kind)}</span>
}
