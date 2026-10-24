import { useEffect, useRef } from 'react'
import { EmptyPanel } from '@/components/ui/EmptyPanel'
import { useI18n } from '@/hooks/useI18n'
import { ChatMessageView } from './ChatMessage'
import type { ThreadMessage } from './types'

export function ChatThread({
  messages,
  busy,
  onLoadDemo,
}: {
  messages: ThreadMessage[]
  busy: boolean
  onLoadDemo?: () => void
}) {
  const { t } = useI18n()
  const bottomRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ block: 'end' })
  }, [messages, busy])

  return (
    <div
      className="min-h-0 flex-1 space-y-4 overflow-y-auto px-4 py-5 sm:px-6 sm:py-6"
      aria-live="polite"
      aria-busy={busy}
    >
      {!messages.length ? (
        <EmptyPanel
          className="min-h-64"
          title={t('chatTryEmptyTitle')}
          hint={t('chatTryEmptyHint')}
          action={
            onLoadDemo ? (
              <button
                type="button"
                className="rounded-lg bg-surface-secondary px-3 py-1.5 text-caption font-medium text-foreground hover:bg-surface-tertiary"
                onClick={onLoadDemo}
              >
                {t('chatDemoLoad')}
              </button>
            ) : undefined
          }
        />
      ) : (
        <div className="space-y-5">
          {messages.map((message) => (
            <ChatMessageView key={message.id} message={message} />
          ))}
        </div>
      )}
      <div ref={bottomRef} />
    </div>
  )
}
