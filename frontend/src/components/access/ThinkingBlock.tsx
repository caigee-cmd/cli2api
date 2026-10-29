import { useEffect, useRef, useState } from 'react'
import { CaretRight, CircleNotch } from '@phosphor-icons/react'
import { useI18n } from '@/hooks/useI18n'
import { duration, ease, gsap, prefersReducedMotion } from '@/lib/motion'

export function ThinkingBlock({
  text,
  streaming,
  defaultOpen,
}: {
  text: string
  streaming: boolean
  defaultOpen?: boolean
}) {
  const { t } = useI18n()
  const [open, setOpen] = useState(Boolean(defaultOpen))
  const [elapsedSec, setElapsedSec] = useState(0)
  const finalSecRef = useRef(0)
  const bodyWrapRef = useRef<HTMLDivElement>(null)
  const bodyInnerRef = useRef<HTMLPreElement>(null)

  useEffect(() => {
    // Streaming stays collapsed to one pill; only expand on explicit defaultOpen when done.
    if (streaming) setOpen(false)
    else if (defaultOpen != null) setOpen(Boolean(defaultOpen))
  }, [streaming, defaultOpen])

  useEffect(() => {
    if (!streaming) return
    const started = Date.now()
    setElapsedSec(0)
    const id = window.setInterval(() => {
      const sec = Math.floor((Date.now() - started) / 1000)
      setElapsedSec(sec)
      finalSecRef.current = sec
    }, 250)
    return () => window.clearInterval(id)
  }, [streaming])

  useEffect(() => {
    if (!streaming && elapsedSec === 0 && finalSecRef.current > 0) {
      setElapsedSec(finalSecRef.current)
    }
  }, [streaming, elapsedSec])

  useEffect(() => {
    const wrap = bodyWrapRef.current
    const inner = bodyInnerRef.current
    if (!wrap) return

    const reduce = prefersReducedMotion()
    if (!open || !text) {
      if (reduce) {
        wrap.style.height = '0px'
        wrap.style.opacity = '0'
        return
      }
      gsap.to(wrap, {
        height: 0,
        opacity: 0,
        duration: duration.fast,
        ease: ease.inOut,
        overwrite: true,
      })
      return
    }

    const target = inner?.scrollHeight ?? 0
    if (reduce) {
      wrap.style.height = 'auto'
      wrap.style.opacity = '1'
      return
    }
    gsap.fromTo(
      wrap,
      { height: 0, opacity: 0 },
      {
        height: target,
        opacity: 1,
        duration: duration.base,
        ease: ease.out,
        overwrite: true,
        onComplete: () => {
          wrap.style.height = 'auto'
        },
      },
    )
  }, [open, text])

  if (!text && !streaming) return null

  const reduceMotion = prefersReducedMotion()
  const doneSec = streaming ? elapsedSec : (elapsedSec || finalSecRef.current)

  return (
    <div className="overflow-hidden rounded-xl">
      <button
        type="button"
        className="inline-flex max-w-full items-center gap-1.5 rounded-full bg-surface-secondary px-2.5 py-1 text-left text-caption text-muted"
        aria-expanded={open}
        onClick={() => setOpen((prev) => !prev)}
      >
        {streaming ? (
          <>
            <CircleNotch
              size={12}
              className={`shrink-0 text-muted ${reduceMotion ? '' : 'animate-spin'}`}
            />
            <span className={reduceMotion ? '' : 'access-thinking-shimmer'}>{t('thinkingWorking')}</span>
            <span className="mono text-muted/80">{elapsedSec}s</span>
          </>
        ) : (
          <>
            <CaretRight size={11} className={`shrink-0 transition-transform duration-200 ease-out ${open ? 'rotate-90' : ''}`} />
            <span>{t('thinkingDone')}</span>
            {doneSec > 0 ? <span className="mono text-muted/80">{t('thinkingElapsed', { n: doneSec })}</span> : null}
            {!doneSec && text ? <span className="mono text-muted/80">{t('thinkingChars', { count: text.length })}</span> : null}
          </>
        )}
      </button>
      <div ref={bodyWrapRef} className="overflow-hidden" style={{ height: 0, opacity: 0 }}>
        {text ? (
          <pre
            ref={bodyInnerRef}
            className="mt-1.5 max-h-52 overflow-auto whitespace-pre-wrap break-words rounded-xl bg-surface-secondary/80 px-3 py-2 text-caption leading-5 text-muted"
          >
            {text}
          </pre>
        ) : null}
      </div>
    </div>
  )
}
