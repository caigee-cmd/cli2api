import { useLayoutEffect, useRef } from 'react'
import { duration, prefersReducedMotion } from '@/lib/motion'

// CSS equivalent of ease.out (power3.out).
const EASE_OUT = 'cubic-bezier(0.215, 0.61, 0.355, 1)'

// Animations run through WAAPI rather than GSAP on purpose: content that
// replaces a skeleton (a 50-row table, a whole page) usually costs one long
// frame to mount. GSAP's ticker measures that frame as elapsed time and jumps
// a 280ms tween straight to its end, so the content showed at opacity 0 for a
// frame and then popped in — a visible flash instead of a fade. WAAPI starts
// the timeline on the first frame it actually paints, so the fade plays in full.
function fadeIn(el: Element, ms: number) {
  return el.animate([{ opacity: 0 }, { opacity: 1 }], { duration: ms, easing: EASE_OUT })
}

// After arc's skeleton: when `loading` turns false, every element marked
// data-skeleton-swap="<name>" that showed a skeleton springs from the
// skeleton's height to the content's height while the content fades in.
// Elements that were not on screen during loading (e.g. the whole page was a
// skeleton) are skipped — useSkeletonExit fades those in once at page level.
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
    const measured = heights.current
    heights.current = new WeakMap()
    if (prefersReducedMotion()) return
    const animations: Animation[] = []
    for (const el of elements) {
      const from = measured.get(el)
      if (from == null) continue
      const to = el.offsetHeight
      if (Math.abs(from - to) > 1) {
        const overflow = el.style.overflow
        el.style.overflow = 'hidden'
        const anim = el.animate([{ height: `${from}px` }, { height: `${to}px` }], { duration: duration.slow * 1000, easing: EASE_OUT })
        anim.finished.catch(() => {}).finally(() => { el.style.overflow = overflow })
        animations.push(anim)
      }
      const content = el.lastElementChild
      if (content) animations.push(fadeIn(content, duration.base * 1000))
    }
    return () => animations.forEach((anim) => anim.finish())
  }, [name, loading])
}

// Whole-page skeleton -> content: pass `skeleton` (true while the page renders
// its full-page skeleton) and put the returned ref on the content root. The
// root fades in exactly once, the first time the skeleton gives way.
export function useSkeletonExit<T extends HTMLElement = HTMLDivElement>(skeleton: boolean) {
  const ref = useRef<T>(null)
  const shown = useRef(skeleton)

  useLayoutEffect(() => {
    if (skeleton) {
      shown.current = true
      return
    }
    if (!shown.current) return
    shown.current = false
    const el = ref.current
    if (!el || prefersReducedMotion()) return
    const anim = fadeIn(el, duration.base * 1000)
    return () => anim.finish()
  }, [skeleton])

  return ref
}
