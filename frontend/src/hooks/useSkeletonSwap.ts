import { useLayoutEffect, useRef } from 'react'
import { duration, ease, gsap, prefersReducedMotion } from '@/lib/motion'

// After arc's skeleton: when `loading` turns false, every element marked
// data-skeleton-swap="<name>" springs from the skeleton's height to the
// content's height (then back to auto) while the content fades in.
export function useSkeletonSwap(name: string, loading: boolean) {
  const heights = useRef(new WeakMap<HTMLElement, number>())
  const wasLoading = useRef(loading)

  useLayoutEffect(() => {
    const elements = Array.from(document.querySelectorAll<HTMLElement>(`[data-skeleton-swap~="${name}"]`))
    if (loading) {
      for (const el of elements) heights.current.set(el, el.offsetHeight)
      wasLoading.current = true
      return
    }
    if (!wasLoading.current) return
    wasLoading.current = false
    if (prefersReducedMotion()) return
    const tweens: gsap.core.Tween[] = []
    for (const el of elements) {
      const from = heights.current.get(el)
      const to = el.offsetHeight
      if (from != null && Math.abs(from - to) > 1) {
        tweens.push(gsap.fromTo(el, { height: from, overflow: 'hidden' }, {
          height: to,
          duration: duration.slow,
          ease: ease.out,
          clearProps: 'height,overflow',
        }))
      }
      const content = el.lastElementChild
      if (content) tweens.push(gsap.from(content, { autoAlpha: 0, duration: duration.base, ease: ease.out, clearProps: 'opacity,visibility' }))
    }
    return () => tweens.forEach((tween) => tween.progress(1).kill())
  }, [name, loading])
}
