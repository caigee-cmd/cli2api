import { useLayoutEffect, type RefObject } from 'react'
import gsap from 'gsap'

export function useGsapReveal(rootRef: RefObject<HTMLElement | null>, key: string) {
  useLayoutEffect(() => {
    const root = rootRef.current
    if (!root) return

    const context = gsap.context(() => {
      const media = gsap.matchMedia()
      media.add('(prefers-reduced-motion: reduce)', () => {
        gsap.set('[data-gsap-reveal]', { clearProps: 'all' })
      })
      media.add('(prefers-reduced-motion: no-preference)', () => {
        const items = gsap.utils.toArray<HTMLElement>('[data-gsap-reveal]')
        if (!items.length) return
        gsap.from(items, {
          autoAlpha: 0,
          y: 10,
          duration: 0.42,
          ease: 'power3.out',
          stagger: 0.045,
          clearProps: 'all',
        })
      })
    }, root)

    return () => context.revert()
  }, [rootRef, key])
}

export function pressScale(target: HTMLElement) {
  if (window.matchMedia('(prefers-reduced-motion: reduce)').matches) return
  gsap.fromTo(target, { scale: 0.975 }, { scale: 1, duration: 0.18, ease: 'power2.out', overwrite: true })
}
