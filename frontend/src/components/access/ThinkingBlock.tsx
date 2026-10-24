import { useEffect, useState } from 'react'
import { CaretRight } from '@phosphor-icons/react'
import { useI18n } from '@/hooks/useI18n'

const PHASES = ['thinkingPhaseScan', 'thinkingPhasePlan', 'thinkingPhaseDraft'] as const

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
  const [open, setOpen] = useState(Boolean(defaultOpen ?? streaming))
  const [phase, setPhase] = useState(0)
  const [elapsedSec, setElapsedSec] = useState(0)

  useEffect(() => {
    if (streaming) setOpen(true)
    else setOpen(Boolean(defaultOpen))
  }, [streaming, defaultOpen])

  useEffect(() => {
    if (!streaming) return
    const id = window.setInterval(() => {
      setPhase((prev) => (prev + 1) % PHASES.length)
    }, 1600)
    return () => window.clearInterval(id)
  }, [streaming])

  useEffect(() => {
    if (!streaming) return
    const started = Date.now()
    setElapsedSec(0)
    const id = window.setInterval(() => {
      setElapsedSec(Math.floor((Date.now() - started) / 1000))
    }, 250)
    return () => window.clearInterval(id)
  }, [streaming])

  if (!text && !streaming) return null

  return (
    <div className="overflow-hidden rounded-xl bg-surface-secondary">
      <button
        type="button"
        className="flex w-full items-center gap-2 px-3 py-2 text-left text-caption font-medium text-muted"
        aria-expanded={open}
        onClick={() => setOpen((prev) => !prev)}
      >
        <CaretRight size={12} className={`shrink-0 transition-transform ${open ? 'rotate-90' : ''}`} />
        {streaming ? (
          <span className="access-thinking-shimmer inline-flex items-center gap-2">
            <span className="inline-flex items-center rounded-full bg-surface px-2 py-0.5 text-caption text-foreground">
              {t(PHASES[phase])}
            </span>
            <span className="mono text-muted">{elapsedSec}s</span>
          </span>
        ) : (
          <span className="inline-flex items-center gap-2">
            <span>{t('thinkingDone')}</span>
            {text ? <span className="mono text-muted">{t('thinkingChars', { count: text.length })}</span> : null}
          </span>
        )}
      </button>
      {open && text ? (
        <div className="px-3 pb-2.5 pt-0">
          <pre className="max-h-56 overflow-auto whitespace-pre-wrap break-words rounded-lg bg-surface/70 px-2.5 py-2 text-caption leading-5 text-muted">
            {text}
          </pre>
        </div>
      ) : null}
    </div>
  )
}
