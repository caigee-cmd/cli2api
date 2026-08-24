import { useLayoutEffect, type RefObject } from 'react'
import gsap from 'gsap'

export function useGsapReveal(rootRef: RefObject<HTMLElement | null>, key: string) {
  useLayoutEffect(() => {
    const root = rootRef.current
    if (!root || window.matchMedia('(prefers-reduced-motion: reduce)').matches) return

    const context = gsap.context(() => {
      const items = gsap.utils.toArray<HTMLElement>('[data-gsap-reveal]')
      if (!items.length) return

      gsap.from(items, {
        autoAlpha: 0,
        y: 16,
        duration: 0.62,
        ease: 'power3.out',
        stagger: 0.075,
        clearProps: 'all',
      })
    }, root)

    return () => context.revert()
  }, [rootRef, key])
}
