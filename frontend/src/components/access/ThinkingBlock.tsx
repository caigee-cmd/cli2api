import { useEffect, useState } from 'react'
import { CaretRight, CheckCircle, Spinner } from '@phosphor-icons/react'
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

  useEffect(() => {
    if (streaming) setOpen(true)
    else setOpen(false)
  }, [streaming])

  useEffect(() => {
    if (!streaming) return
    const id = window.setInterval(() => {
      setPhase((prev) => (prev + 1) % PHASES.length)
    }, 1600)
    return () => window.clearInterval(id)
  }, [streaming])

  if (!text && !streaming) return null

  return (
    <div className="rounded-lg bg-surface-secondary">
      <button
        type="button"
        className="flex w-full items-center gap-2 px-3 py-2 text-left text-caption font-medium text-muted"
        aria-expanded={open}
        onClick={() => setOpen((prev) => !prev)}
      >
        <CaretRight size={12} className={`shrink-0 transition-transform ${open ? 'rotate-90' : ''}`} />
        {streaming ? (
          <>
            <Spinner size={13} className="shrink-0 animate-spin" />
            <span className="inline-flex items-center rounded-full bg-surface px-2 py-0.5 text-caption text-foreground">
              {t(PHASES[phase])}
            </span>
          </>
        ) : (
          <>
            <CheckCircle size={13} weight="fill" className="shrink-0 text-muted" />
            <span>{t('thinkingDone')}</span>
          </>
        )}
        {!streaming && text ? (
          <span className="ml-auto mono text-caption text-muted">{text.length}</span>
        ) : null}
      </button>
      {open && text ? (
        <div className="border-t border-separator px-3 py-2">
          <pre className="max-h-56 overflow-auto whitespace-pre-wrap break-words text-caption leading-5 text-muted">
            {text}
          </pre>
        </div>
      ) : null}
    </div>
  )
}
