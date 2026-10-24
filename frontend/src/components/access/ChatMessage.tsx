import { useState } from 'react'
import { PageAlert } from '@/components/ui/PageAlert'
import { Skeleton } from '@heroui/react'
import { CopySimple, FileText } from '@phosphor-icons/react'
import { useI18n } from '@/hooks/useI18n'
import { copyText } from '@/lib/clipboard'
import { formatCompact, formatCredits } from '@/lib/format'
import { ThinkingBlock } from './ThinkingBlock'
import { ToolCallList } from './ToolCallCard'
import type { ThreadMessage } from './types'

function AttachmentStrip({ message }: { message: ThreadMessage }) {
  if (!message.attachments?.length) return null
  return (
    <div className="mt-2 flex flex-wrap gap-1.5">
      {message.attachments.map((file) => (
        <span
          key={file.id}
          className="inline-flex max-w-[12rem] items-center gap-1.5 rounded-xl bg-surface/70 px-1.5 py-1 text-caption text-muted"
        >
          {file.kind === 'image' && file.dataUrl ? (
            <img src={file.dataUrl} alt="" className="size-7 shrink-0 rounded-lg object-cover" />
          ) : (
            <span className="grid size-7 shrink-0 place-items-center rounded-lg bg-surface">
              <FileText size={12} />
            </span>
          )}
          <span className="truncate">{file.name}</span>
        </span>
      ))}
    </div>
  )
}

function UsageFoot({ message }: { message: ThreadMessage }) {
  const { t } = useI18n()
  const usage = message.usage
  if (!usage || usage.credits == null) return null
  const prompt = usage.prompt_tokens
  const completion = usage.completion_tokens
  const hasTokens = typeof prompt === 'number' || typeof completion === 'number'
  const tokens = hasTokens
    ? t('chatTryUsageTokens', {
        prompt: formatCompact(prompt ?? 0),
        completion: formatCompact(completion ?? 0),
      })
    : ''
  return (
    <span className="mono text-caption text-muted">
      {tokens
        ? t('chatTryUsageFoot', { tokens, amount: formatCredits(usage.credits) })
        : t('chatTryCreditsUsed', { amount: formatCredits(usage.credits) })}
    </span>
  )
}

export function ChatMessageView({ message }: { message: ThreadMessage }) {
  const { t } = useI18n()
  const [copied, setCopied] = useState(false)
  const isUser = message.role === 'user'

  if (isUser) {
    return (
      <div className="ml-auto max-w-[92%] sm:max-w-[78%]">
        <div className="rounded-2xl rounded-br-md bg-surface-secondary/90 px-3.5 py-2.5">
          {message.content ? (
            <div className="whitespace-pre-wrap break-words text-body leading-[1.65] text-foreground">{message.content}</div>
          ) : null}
          <AttachmentStrip message={message} />
        </div>
      </div>
    )
  }

  const showSkeleton = message.status === 'streaming' && !message.content && !message.reasoning && !message.toolCalls?.length
  const canCopy = Boolean(message.content) && message.status === 'done'

  async function onCopy() {
    if (!message.content) return
    await copyText(message.content)
    setCopied(true)
    window.setTimeout(() => setCopied(false), 1200)
  }

  return (
    <div className="max-w-[96%] space-y-2 sm:max-w-[88%]">
      {message.reasoning || message.status === 'streaming' ? (
        <ThinkingBlock
          text={message.reasoning || ''}
          streaming={message.status === 'streaming' && (!message.content || Boolean(message.reasoning))}
          defaultOpen={message.reasoningOpen}
        />
      ) : null}
      {message.toolCalls?.length ? <ToolCallList calls={message.toolCalls} /> : null}
      {message.status === 'error' ? (
        <PageAlert status="danger" title={t('requestFailed')} description={message.error || ''} />
      ) : showSkeleton ? (
        <div className="space-y-2.5 pt-0.5">
          <Skeleton className="h-3 w-28 rounded-md" />
          <Skeleton className="h-3 w-full rounded-md" />
          <Skeleton className="h-3 w-[86%] rounded-md" />
          <Skeleton className="h-3 w-[68%] rounded-md" />
        </div>
      ) : message.content ? (
        <div className="whitespace-pre-wrap break-words rounded-2xl rounded-tl-md bg-surface-secondary/50 px-3.5 py-2.5 text-body leading-[1.65] text-foreground">
          {message.content}
        </div>
      ) : null}
      {(canCopy || (message.usage && message.usage.credits != null)) ? (
        <div className="flex flex-wrap items-center gap-x-3 gap-y-1 px-0.5">
          {canCopy ? (
            <button
              type="button"
              className="inline-flex items-center gap-1 text-caption text-muted hover:text-foreground"
              onClick={() => void onCopy()}
            >
              <CopySimple size={12} />
              {copied ? t('copied') : t('copy')}
            </button>
          ) : null}
          <UsageFoot message={message} />
        </div>
      ) : null}
    </div>
  )
}
