import { useLayoutEffect, useRef } from 'react'
import { Check, Copy } from '@phosphor-icons/react'
import { duration, ease, gsap, prefersReducedMotion } from '@/lib/motion'

// Copy → check feedback after arc's copy-button: the old glyph shrinks into a
// blur while the new one grows out of it, and the same motion plays back.
export function CopyIcon({ copied, size = 14, className }: { copied: boolean; size?: number; className?: string }) {
  const copyRef = useRef<HTMLSpanElement>(null)
  const checkRef = useRef<HTMLSpanElement>(null)
  const first = useRef(true)

  useLayoutEffect(() => {
    const show = copied ? checkRef.current : copyRef.current
    const hide = copied ? copyRef.current : checkRef.current
    if (!show || !hide) return
    const shown = { autoAlpha: 1, scale: 1, filter: 'blur(0px)' }
    const hidden = { autoAlpha: 0, scale: 0.5, filter: 'blur(4px)' }
    if (first.current || prefersReducedMotion()) {
      first.current = false
      gsap.set(show, shown)
      gsap.set(hide, hidden)
      return
    }
    gsap.to(hide, { ...hidden, duration: duration.fast, ease: ease.out, overwrite: true })
    gsap.fromTo(show, hidden, { ...shown, duration: duration.base, ease: ease.soft, delay: duration.instant / 2, overwrite: true })
  }, [copied])

  return (
    <span className={`relative inline-grid place-items-center ${className ?? ''}`} style={{ width: size, height: size }}>
      <span ref={copyRef} className="col-start-1 row-start-1 inline-flex"><Copy size={size} /></span>
      <span ref={checkRef} className="col-start-1 row-start-1 inline-flex text-success"><Check size={size} /></span>
    </span>
  )
}
