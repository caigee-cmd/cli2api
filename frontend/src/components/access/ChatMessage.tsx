import { useEffect, useRef, useState } from 'react'
import { PageAlert } from '@/components/ui/PageAlert'
import { Button, Skeleton } from '@heroui/react'
import { FileText } from '@phosphor-icons/react'
import { CopyIcon } from '@/components/motion/CopyIcon'
import { ProviderMark } from '@/components/ProviderMark'
import { useI18n } from '@/hooks/useI18n'
import { copyText } from '@/lib/clipboard'
import { formatCompact, formatCredits } from '@/lib/format'
import { LatencyStats } from '@/components/ui/LatencyStats'
import { duration, gsap, prefersReducedMotion } from '@/lib/motion'
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

/** Split plain text into paragraph-ish segments for quiet stream fade-in. */
function ContentSegments({
  text,
  streaming,
}: {
  text: string
  streaming: boolean
}) {
  const segments = text.split(/(\n{2,})/)
  let lastTextIdx = -1
  for (let i = segments.length - 1; i >= 0; i -= 1) {
    if (segments[i] && !/^\n{2,}$/.test(segments[i])) {
      lastTextIdx = i
      break
    }
  }

  return (
    <div className={`whitespace-pre-wrap break-words text-body leading-[1.65] text-foreground ${streaming ? 'access-streaming' : ''}`}>
      {segments.map((part, index) => {
        if (!part) return null
        const isSep = /^\n{2,}$/.test(part)
        if (isSep) return <span key={`s-${index}`}>{part}</span>
        const fresh = streaming && index === lastTextIdx
        return (
          <span key={`p-${index}`} className={fresh ? 'access-stream-seg' : undefined}>
            {part}
          </span>
        )
      })}
    </div>
  )
}

function useEnterFade<T extends HTMLElement>() {
  const ref = useRef<T>(null)
  useEffect(() => {
    const el = ref.current
    if (!el || prefersReducedMotion()) return
    gsap.from(el, {
      autoAlpha: 0,
      duration: duration.fast,
      ease: 'power2.out',
    })
  }, [])
  return ref
}

export function ChatMessageView({ message }: { message: ThreadMessage }) {
  const { t } = useI18n()
  const [copied, setCopied] = useState(false)
  const isUser = message.role === 'user'
  // Enter fade only for user bubbles — assistant starts as skeleton/stream and
  // must not re-fade when content replaces the skeleton (no fade-after-skeleton flash).
  const rootRef = useEnterFade<HTMLDivElement>()

  if (isUser) {
    return (
      <div ref={rootRef} className="ml-auto max-w-[92%] sm:max-w-[78%]">
        <div className="access-bubble access-bubble--user rounded-2xl rounded-br-md px-3.5 py-2.5">
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
  const streaming = message.status === 'streaming'
  const showUsage = Boolean(message.usage && message.usage.credits != null)
  const showLatency =
    message.status === 'done' &&
    (message.latency_ms != null || message.ttfb_ms != null || typeof message.usage?.completion_tokens === 'number')

  async function onCopy() {
    if (!message.content) return
    await copyText(message.content)
    setCopied(true)
    window.setTimeout(() => setCopied(false), 1200)
  }

  return (
    <div className="group/access-msg max-w-[96%] space-y-1.5 sm:max-w-[88%]">
      <div className="flex items-center gap-1.5 px-0.5">
        <span className="grid size-5 shrink-0 place-items-center rounded-lg border border-border bg-surface-secondary">
          <ProviderMark provider={message.provider} size={13} />
        </span>
      </div>
      {message.reasoning || streaming ? (
        <ThinkingBlock
          text={message.reasoning || ''}
          streaming={streaming && (!message.content || Boolean(message.reasoning))}
          defaultOpen={message.reasoningOpen}
        />
      ) : null}
      {message.toolCalls?.length ? <ToolCallList calls={message.toolCalls} /> : null}
      {message.status === 'error' ? (
        <PageAlert status="danger" title={t('requestFailed')} description={message.error || ''} />
      ) : showSkeleton ? (
        <div className="space-y-2 pt-0.5">
          <Skeleton className="h-3 w-28 rounded-md" />
          <Skeleton className="h-3 w-full rounded-md" />
          <Skeleton className="h-3 w-[86%] rounded-md" />
          <Skeleton className="h-3 w-[68%] rounded-md" />
        </div>
      ) : message.content ? (
        <div className="access-bubble access-bubble--assistant px-0.5 py-0">
          <ContentSegments text={message.content} streaming={streaming} />
        </div>
      ) : null}
      {(canCopy || showUsage || showLatency) ? (
        <div className="access-msg-foot space-y-1 px-0.5 pt-0.5">
          <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
            {canCopy ? (
              <Button
                size="sm"
                variant="ghost"
                className="h-5 min-h-5 gap-1 px-1.5 text-caption text-muted/80"
                onPress={() => void onCopy()}
              >
                <CopyIcon copied={copied} size={12} />
                {copied ? t('copied') : t('copy')}
              </Button>
            ) : null}
            <UsageFoot message={message} />
          </div>
          {showLatency ? (
            <LatencyStats
              variant="inline"
              latencyMs={message.latency_ms}
              ttfbMs={message.ttfb_ms}
              completionTokens={message.usage?.completion_tokens}
              stream
              className="access-latency-inline max-h-0 overflow-hidden opacity-0 transition-[opacity,max-height] duration-200 ease-[cubic-bezier(0.2,0.7,0.2,1)] group-hover/access-msg:max-h-5 group-hover/access-msg:opacity-100 focus-within:max-h-5 focus-within:opacity-100"
              labels={{
                ttft: t('logsColTTFT'),
                total: t('logsLatencyTotal'),
                tps: t('logsOutputTPS'),
              }}
            />
          ) : null}
        </div>
      ) : null}
    </div>
  )
}
