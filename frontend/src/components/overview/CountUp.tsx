import { useLayoutEffect, useRef } from 'react'
import gsap from 'gsap'
import { formatCountKind } from '@/lib/format'

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

  useLayoutEffect(() => {
    const el = ref.current
    if (!el) return
    const state = { value: 0 }
    const context = gsap.context(() => {
      const media = gsap.matchMedia()
      media.add('(prefers-reduced-motion: reduce)', () => {
        el.textContent = formatCountKind(value, kind)
      })
      media.add('(prefers-reduced-motion: no-preference)', () => {
        gsap.to(state, {
          value,
          duration: 0.5,
          ease: 'power3.out',
          onUpdate: () => {
            el.textContent = formatCountKind(state.value, kind)
          },
        })
      })
    }, el)
    return () => context.revert()
  }, [kind, value])

  return <span ref={ref} className={className}>{formatCountKind(0, kind)}</span>
}
