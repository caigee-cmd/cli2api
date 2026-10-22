import { useLayoutEffect, useRef, useSyncExternalStore } from 'react'
import { CheckCircle, Info, Spinner, WarningCircle, X } from '@phosphor-icons/react'
import { dismissToast, getToasts, removeToast, subscribeToasts, type ToastItem } from '@/lib/toast'
import { duration, ease, gsap, prefersReducedMotion } from '@/lib/motion'

const icon = {
  loading: <Spinner size={16} className="animate-spin text-muted motion-reduce:animate-none" />,
  success: <CheckCircle size={16} weight="fill" className="text-success" />,
  error: <WarningCircle size={16} weight="fill" className="text-danger" />,
  info: <Info size={16} weight="fill" className="text-accent" />,
}

function Toast({ item }: { item: ToastItem }) {
  const ref = useRef<HTMLDivElement>(null)
  const bodyRef = useRef<HTMLDivElement>(null)
  const firstVersion = useRef(item.version)

  // Enter: rise in from below.
  useLayoutEffect(() => {
    const el = ref.current
    if (!el || prefersReducedMotion()) return
    const tween = gsap.from(el, { autoAlpha: 0, y: 16, scale: 0.96, duration: duration.slow, ease: ease.out, clearProps: 'transform,opacity,visibility' })
    return () => { tween.kill() }
  }, [])

  // In-place update (same id): crossfade the copy instead of stacking a new toast.
  useLayoutEffect(() => {
    const body = bodyRef.current
    if (!body || item.version === firstVersion.current || prefersReducedMotion()) return
    const tween = gsap.fromTo(body, { autoAlpha: 0, y: 4 }, { autoAlpha: 1, y: 0, duration: duration.base, ease: ease.out })
    return () => { tween.kill() }
  }, [item.version])

  // Exit: collapse and fade, then drop from the store.
  useLayoutEffect(() => {
    const el = ref.current
    if (!item.leaving || !el) return
    if (prefersReducedMotion()) {
      removeToast(item.id)
      return
    }
    const tween = gsap.to(el, {
      autoAlpha: 0,
      x: 24,
      height: 0,
      marginTop: 0,
      paddingTop: 0,
      paddingBottom: 0,
      duration: duration.base,
      ease: ease.inOut,
      onComplete: () => removeToast(item.id),
    })
    return () => { tween.kill() }
  }, [item.leaving, item.id])

  return (
    <div
      ref={ref}
      role={item.type === 'error' ? 'alert' : 'status'}
      className="pointer-events-auto mt-2 flex w-full items-start gap-2.5 overflow-hidden rounded-xl bg-overlay px-3.5 py-3 text-body text-foreground shadow-overlay"
    >
      <span className="mt-0.5 inline-flex shrink-0">{icon[item.type]}</span>
      <div ref={bodyRef} className="min-w-0 flex-1">
        <div className="font-medium leading-5">{item.title}</div>
        {item.description ? <div className="mt-0.5 break-words text-caption leading-5 text-muted">{item.description}</div> : null}
      </div>
      <button
        type="button"
        aria-label="Close"
        onClick={() => dismissToast(item.id)}
        className="grid size-6 shrink-0 place-items-center rounded-md text-muted transition-colors hover:bg-surface-secondary hover:text-foreground"
      >
        <X size={12} />
      </button>
    </div>
  )
}

export function Toaster() {
  const items = useSyncExternalStore(subscribeToasts, getToasts, getToasts)
  return (
    <div aria-live="polite" className="pointer-events-none fixed bottom-4 right-4 z-[100] flex w-[min(360px,calc(100vw-2rem))] flex-col">
      {items.map((item) => <Toast key={item.id} item={item} />)}
    </div>
  )
}
