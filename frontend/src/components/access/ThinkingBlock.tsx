import { useEffect, useRef, useState } from 'react'
import { CaretRight, CircleNotch } from '@phosphor-icons/react'
import { useI18n } from '@/hooks/useI18n'
import { prefersReducedMotion } from '@/lib/motion'

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
            <CaretRight size={11} className={`shrink-0 transition-transform ${open ? 'rotate-90' : ''}`} />
            <span>{t('thinkingDone')}</span>
            {doneSec > 0 ? <span className="mono text-muted/80">{t('thinkingElapsed', { n: doneSec })}</span> : null}
            {!doneSec && text ? <span className="mono text-muted/80">{t('thinkingChars', { count: text.length })}</span> : null}
          </>
        )}
      </button>
      {open && text ? (
        <div className="mt-1.5">
          <pre className="max-h-52 overflow-auto whitespace-pre-wrap break-words rounded-xl bg-surface-secondary/80 px-3 py-2 text-caption leading-5 text-muted">
            {text}
          </pre>
        </div>
      ) : null}
    </div>
  )
}
