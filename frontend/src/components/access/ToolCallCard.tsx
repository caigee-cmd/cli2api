import { useEffect, useMemo, useState } from 'react'
import { CaretRight, CheckCircle, CopySimple, Spinner, WarningCircle } from '@phosphor-icons/react'
import { useI18n } from '@/hooks/useI18n'
import { copyText } from '@/lib/clipboard'
import type { ChatToolCall } from './types'

function statusLabel(status: ChatToolCall['status'], t: (key: string) => string) {
  if (status === 'running') return t('toolCallRunning')
  if (status === 'error') return t('toolCallError')
  return t('toolCallSuccess')
}

function StatusIcon({ status }: { status: ChatToolCall['status'] }) {
  if (status === 'running') return <Spinner size={12} className="animate-spin text-muted" />
  if (status === 'error') return <WarningCircle size={12} weight="fill" className="text-danger" />
  return <CheckCircle size={12} weight="fill" className="text-success" />
}

function prettyArgs(args: string) {
  if (!args.trim()) return ''
  try {
    return JSON.stringify(JSON.parse(args), null, 2)
  } catch {
    return args
  }
}

export function ToolCallCard({ call, forceOpen }: { call: ChatToolCall; forceOpen?: boolean }) {
  const { t } = useI18n()
  const [open, setOpen] = useState(call.status === 'error' || Boolean(forceOpen))
  const [copied, setCopied] = useState(false)
  const pretty = useMemo(() => prettyArgs(call.arguments || ''), [call.arguments])

  useEffect(() => {
    if (call.status === 'error') setOpen(true)
  }, [call.status])

  useEffect(() => {
    if (forceOpen) setOpen(true)
  }, [forceOpen])

  async function onCopy() {
    if (!pretty) return
    await copyText(pretty)
    setCopied(true)
    window.setTimeout(() => setCopied(false), 1200)
  }

  return (
    <div className="overflow-hidden rounded-lg">
      <button
        type="button"
        className="flex w-full items-center gap-2 px-2 py-1.5 text-left"
        aria-expanded={open}
        onClick={() => setOpen((prev) => !prev)}
      >
        <CaretRight size={11} className={`shrink-0 text-muted transition-transform ${open ? 'rotate-90' : ''}`} />
        <span className="mono min-w-0 flex-1 truncate text-caption font-medium text-foreground">
          {call.name || t('toolCallUntitled')}
        </span>
        <span className="inline-flex shrink-0 items-center gap-1 text-caption text-muted">
          <StatusIcon status={call.status} />
          {statusLabel(call.status, t)}
        </span>
      </button>
      {open ? (
        <div className="space-y-1.5 px-2 pb-2">
          <div className="flex items-center justify-between gap-2">
            <div className="text-caption font-medium text-muted">{t('toolCallArgs')}</div>
            <button
              type="button"
              className="inline-flex items-center gap-1 rounded-md px-1.5 py-0.5 text-caption text-muted hover:bg-surface-secondary hover:text-foreground"
              onClick={(event) => {
                event.stopPropagation()
                void onCopy()
              }}
            >
              <CopySimple size={12} />
              {copied ? t('copied') : t('copy')}
            </button>
          </div>
          <pre className="mono max-h-40 overflow-auto whitespace-pre-wrap break-words rounded-lg bg-surface-secondary/80 px-2.5 py-2 text-caption leading-5 text-muted">
            {pretty || t('toolCallArgsEmpty')}
          </pre>
        </div>
      ) : null}
    </div>
  )
}

export function ToolCallList({ calls }: { calls: ChatToolCall[] }) {
  const { t } = useI18n()
  const hasError = calls.some((call) => call.status === 'error')
  const anyRunning = calls.some((call) => call.status === 'running')
  const [groupOpen, setGroupOpen] = useState(hasError || anyRunning || calls.length === 1)

  useEffect(() => {
    if (hasError || anyRunning) setGroupOpen(true)
  }, [hasError, anyRunning])

  if (!calls.length) return null

  if (calls.length === 1) {
    return (
      <div className="rounded-xl bg-surface-secondary/70">
        <ToolCallCard call={calls[0]} forceOpen={calls[0].status === 'error'} />
      </div>
    )
  }

  return (
    <div className="overflow-hidden rounded-xl bg-surface-secondary/70">
      <button
        type="button"
        className="flex w-full items-center gap-2 px-2.5 py-1.5 text-left text-caption text-muted"
        aria-expanded={groupOpen}
        onClick={() => setGroupOpen((prev) => !prev)}
      >
        <CaretRight size={11} className={`shrink-0 transition-transform ${groupOpen ? 'rotate-90' : ''}`} />
        <span className="min-w-0 flex-1 truncate font-medium text-foreground">
          {t('toolCallGroup', { n: calls.length })}
        </span>
        {anyRunning ? <Spinner size={12} className="animate-spin" /> : null}
      </button>
      {groupOpen ? (
        <div className="space-y-0.5 border-t border-separator/40 px-0.5 py-1">
          {calls.map((call) => (
            <ToolCallCard key={`${call.index}-${call.id || call.name}`} call={call} />
          ))}
        </div>
      ) : null}
    </div>
  )
}
