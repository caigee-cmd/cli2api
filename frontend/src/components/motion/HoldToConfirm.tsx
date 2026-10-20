import { useEffect, useRef, type ReactNode } from 'react'
import { Button, Tooltip } from '@heroui/react'
import { duration, ease, gsap, prefersReducedMotion } from '@/lib/motion'

const HOLD_SECONDS = 1.1

// Icon button that commits only after it is held, after arc's hold-to-confirm:
// a fill tracks the hold linearly, letting go early rewinds it, finishing fires
// onConfirm. Pointer and keyboard (Space / Enter) holds both work via HeroUI's
// press events. Use for single, on-screen items only; batch or irreversible
// actions keep a confirmation dialog.
export function HoldToConfirm({
  label,
  onConfirm,
  isPending,
  children,
}: {
  label: string
  onConfirm: () => void
  isPending?: boolean
  children: ReactNode
}) {
  const fillRef = useRef<HTMLSpanElement>(null)
  const tweenRef = useRef<gsap.core.Tween | null>(null)

  useEffect(() => () => { tweenRef.current?.kill() }, [])

  const start = () => {
    const fill = fillRef.current
    if (!fill || isPending) return
    tweenRef.current?.kill()
    tweenRef.current = gsap.to(fill, {
      scaleX: 1,
      duration: HOLD_SECONDS * (1 - Number(gsap.getProperty(fill, 'scaleX'))),
      ease: ease.linear,
      onComplete: () => {
        tweenRef.current = null
        gsap.to(fill, { scaleX: 0, duration: prefersReducedMotion() ? 0 : duration.base, ease: ease.out, delay: duration.fast })
        onConfirm()
      },
    })
  }

  const cancel = () => {
    const fill = fillRef.current
    if (!fill || !tweenRef.current) return
    tweenRef.current.kill()
    tweenRef.current = null
    gsap.to(fill, { scaleX: 0, duration: prefersReducedMotion() ? 0 : duration.base, ease: ease.soft })
  }

  return (
    <Tooltip delay={300}>
      <Tooltip.Trigger>
        <Button
          isIconOnly
          size="sm"
          variant="ghost"
          aria-label={label}
          isPending={isPending}
          className="relative overflow-hidden"
          onPressStart={start}
          onPressEnd={cancel}
        >
          <span
            ref={fillRef}
            aria-hidden
            className="pointer-events-none absolute inset-0 origin-left scale-x-0 bg-danger/20"
          />
          <span className="relative inline-flex">{children}</span>
        </Button>
      </Tooltip.Trigger>
      <Tooltip.Content>{label}</Tooltip.Content>
    </Tooltip>
  )
}
