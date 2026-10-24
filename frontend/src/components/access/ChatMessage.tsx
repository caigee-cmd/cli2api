import { PageAlert } from '@/components/ui/PageAlert'
import { Skeleton } from '@heroui/react'
import { FileText } from '@phosphor-icons/react'
import { useI18n } from '@/hooks/useI18n'
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
          className="inline-flex max-w-[12rem] items-center gap-1.5 rounded-xl bg-surface/80 px-1.5 py-1 text-caption text-muted"
        >
          {file.kind === 'image' && file.dataUrl ? (
            <img src={file.dataUrl} alt="" className="size-7 shrink-0 rounded-lg object-cover" />
          ) : (
            <span className="grid size-7 shrink-0 place-items-center rounded-lg bg-surface-secondary">
              <FileText size={12} />
            </span>
          )}
          <span className="truncate">{file.name}</span>
        </span>
      ))}
    </div>
  )
}

export function ChatMessageView({ message }: { message: ThreadMessage }) {
  const { t } = useI18n()
  const isUser = message.role === 'user'

  if (isUser) {
    return (
      <div className="ml-auto max-w-[92%] sm:max-w-[78%]">
        <div className="rounded-2xl rounded-br-md bg-surface-secondary px-3.5 py-2.5">
          {message.content ? (
            <div className="whitespace-pre-wrap break-words text-body leading-6 text-foreground">{message.content}</div>
          ) : null}
          <AttachmentStrip message={message} />
        </div>
      </div>
    )
  }

  const showSkeleton = message.status === 'streaming' && !message.content && !message.reasoning && !message.toolCalls?.length

  return (
    <div className="max-w-[96%] space-y-2 sm:max-w-[88%]">
      <div className="text-caption font-medium text-muted">
        {t('chatTryAssistantLabel')}
        {message.status === 'streaming' ? <span className="ml-2">{t('chatTryStreaming')}</span> : null}
      </div>
      {message.reasoning || message.status === 'streaming' ? (
        <ThinkingBlock
          text={message.reasoning || ''}
          streaming={message.status === 'streaming'}
          defaultOpen={message.reasoningOpen}
        />
      ) : null}
      {message.toolCalls?.length ? <ToolCallList calls={message.toolCalls} /> : null}
      {message.status === 'error' ? (
        <PageAlert status="danger" title={t('requestFailed')} description={message.error || ''} />
      ) : showSkeleton ? (
        <div className="space-y-3 pt-1">
          <Skeleton className="h-3 w-32 rounded-lg" />
          <Skeleton className="h-3 w-full rounded-lg" />
          <Skeleton className="h-3 w-[88%] rounded-lg" />
          <Skeleton className="h-3 w-[72%] rounded-lg" />
        </div>
      ) : message.content ? (
        <div className="whitespace-pre-wrap break-words rounded-2xl bg-surface px-3.5 py-2.5 text-body leading-6 text-foreground shadow-surface">
          {message.content}
        </div>
      ) : null}
    </div>
  )
}
