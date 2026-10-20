import { useLayoutEffect, type RefObject } from 'react'
import { duration, ease, gsap, stagger } from '@/lib/motion'

// First-paint reveal for HeroUI meter fills inside `rootRef`, after arc's
// usage-meter: each fill grows from the left in turn. Later value changes keep
// HeroUI's own width transition, so segments re-flow instead of re-rendering.
export function useFillReveal(rootRef: RefObject<HTMLElement | null>) {
  useLayoutEffect(() => {
    const root = rootRef.current
    if (!root) return
    const context = gsap.context(() => {
      const media = gsap.matchMedia()
      media.add('(prefers-reduced-motion: no-preference)', () => {
        const fills = gsap.utils.toArray<HTMLElement>('.meter__fill')
        if (!fills.length) return
        gsap.from(fills, {
          scaleX: 0,
          transformOrigin: 'left center',
          duration: duration.calm,
          ease: ease.emphasized,
          stagger: stagger.base,
          clearProps: 'transform,transformOrigin',
        })
      })
    }, root)
    return () => context.revert()
  }, [rootRef])
}
