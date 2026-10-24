import { useEffect, useState } from 'react'
import { CaretRight, CheckCircle, Spinner, WarningCircle } from '@phosphor-icons/react'
import { useI18n } from '@/hooks/useI18n'
import type { ChatToolCall } from './types'

function statusLabel(status: ChatToolCall['status'], t: (key: string) => string) {
  if (status === 'running') return t('toolCallRunning')
  if (status === 'error') return t('toolCallError')
  return t('toolCallSuccess')
}

function StatusIcon({ status }: { status: ChatToolCall['status'] }) {
  if (status === 'running') return <Spinner size={13} className="animate-spin text-muted" />
  if (status === 'error') return <WarningCircle size={13} weight="fill" className="text-danger" />
  return <CheckCircle size={13} weight="fill" className="text-success" />
}

export function ToolCallCard({ call }: { call: ChatToolCall }) {
  const { t } = useI18n()
  const [open, setOpen] = useState(call.status === 'running')
  const args = call.arguments || ''
  const pretty = (() => {
    if (!args.trim()) return ''
    try {
      return JSON.stringify(JSON.parse(args), null, 2)
    } catch {
      return args
    }
  })()

  useEffect(() => {
    if (call.status === 'running') setOpen(true)
  }, [call.status])

  return (
    <div className="overflow-hidden rounded-xl bg-surface-secondary">
      <button
        type="button"
        className="flex w-full items-center gap-2 px-3 py-2 text-left"
        aria-expanded={open}
        onClick={() => setOpen((prev) => !prev)}
      >
        <CaretRight size={12} className={`shrink-0 text-muted transition-transform ${open ? 'rotate-90' : ''}`} />
        <span className="min-w-0 flex-1 truncate mono text-caption font-medium text-foreground">
          {call.name || t('toolCallUntitled')}
        </span>
        <span className="inline-flex shrink-0 items-center gap-1 rounded-full bg-surface px-2 py-0.5 text-caption text-muted">
          <StatusIcon status={call.status} />
          {statusLabel(call.status, t)}
        </span>
      </button>
      {open ? (
        <div className="space-y-1.5 px-3 pb-2.5">
          <div className="text-caption font-medium text-muted">{t('toolCallArgs')}</div>
          <pre className="mono max-h-48 overflow-auto whitespace-pre-wrap break-words rounded-lg bg-surface/70 px-2.5 py-2 text-caption leading-5 text-muted">
            {pretty || t('toolCallArgsEmpty')}
          </pre>
        </div>
      ) : null}
    </div>
  )
}

export function ToolCallList({ calls }: { calls: ChatToolCall[] }) {
  if (!calls.length) return null
  return (
    <div className="space-y-1.5">
      {calls.map((call) => (
        <ToolCallCard key={`${call.index}-${call.id || call.name}`} call={call} />
      ))}
    </div>
  )
}
