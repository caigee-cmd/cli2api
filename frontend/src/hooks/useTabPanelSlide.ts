import { useLayoutEffect, useRef } from 'react'
import { duration, ease, gsap, prefersReducedMotion } from '@/lib/motion'

// After arc's tabs: the incoming panel (marked data-tab-panel="<id>") slides in
// from the side of the tab it came after. Reduced motion: no movement.
export function useTabPanelSlide(tab: string, order: readonly string[]) {
  const previous = useRef(tab)

  useLayoutEffect(() => {
    const from = previous.current
    previous.current = tab
    if (from === tab || prefersReducedMotion()) return
    const panel = document.querySelector<HTMLElement>(`[data-tab-panel="${tab}"]`)
    if (!panel) return
    const direction = order.indexOf(tab) > order.indexOf(from) ? 1 : -1
    const tween = gsap.fromTo(panel, { x: 16 * direction, autoAlpha: 0 }, {
      x: 0,
      autoAlpha: 1,
      duration: duration.base,
      ease: ease.out,
      clearProps: 'transform,opacity,visibility',
    })
    return () => { tween.progress(1).kill() }
  }, [tab, order])
}
