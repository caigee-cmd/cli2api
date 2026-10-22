import { useLayoutEffect, useRef, useState } from 'react'
import { duration, ease, gsap, motionDuration } from '@/lib/motion'

/**
 * Height expand/collapse for a section that mounts on `open` and unmounts
 * after collapsing. Only the wrapper height and the inner content's
 * opacity/y are tweened; children are never animated individually. Height is
 * cleared to `auto` when expanded so later content changes reflow naturally.
 */
export function useCollapse<T extends HTMLElement = HTMLDivElement, I extends HTMLElement = HTMLDivElement>(open: boolean) {
  const [mounted, setMounted] = useState(open)
  if (open && !mounted) setMounted(true)
  const outerRef = useRef<T>(null)
  const innerRef = useRef<I>(null)
  const skipFirst = useRef(open)

  useLayoutEffect(() => {
    const outer = outerRef.current
    const inner = innerRef.current
    if (!outer || !inner) return
    if (skipFirst.current) {
      // Already open when the modal mounted: no entrance animation.
      skipFirst.current = false
      return
    }
    gsap.killTweensOf([outer, inner])
    const from = outer.getBoundingClientRect().height
    if (open) {
      gsap.set(outer, { overflow: 'hidden' })
      const to = outer.scrollHeight
      gsap.fromTo(outer, { height: from === to ? 0 : from }, {
        height: to,
        duration: motionDuration(duration.base),
        ease: ease.out,
        onComplete: () => gsap.set(outer, { clearProps: 'height,overflow' }),
      })
      gsap.fromTo(inner, { autoAlpha: 0, y: -6 }, { autoAlpha: 1, y: 0, duration: motionDuration(duration.base), ease: ease.out })
    } else {
      gsap.set(outer, { overflow: 'hidden', height: from })
      gsap.to(inner, { autoAlpha: 0, y: -4, duration: motionDuration(duration.fast), ease: ease.inOut })
      gsap.to(outer, {
        height: 0,
        duration: motionDuration(duration.fast + 0.04),
        ease: ease.inOut,
        onComplete: () => setMounted(false),
      })
    }
    return () => { gsap.killTweensOf([outer, inner]) }
  }, [open, mounted])

  return { mounted, outerRef, innerRef }
}
