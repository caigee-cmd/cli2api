import { useEffect, useRef } from 'react'
import { ChatCircleDots } from '@phosphor-icons/react'
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

  const empty = !messages.length

  return (
    <div
      className={`access-thread min-h-0 flex-1 overflow-y-auto px-4 sm:px-6 ${
        empty ? 'py-5 sm:py-6' : 'pb-2 pt-4 sm:pb-2.5 sm:pt-5'
      }`}
      aria-live="polite"
      aria-busy={busy}
    >
      {empty ? (
        <EmptyPanel
          className="min-h-56"
          icon={<ChatCircleDots size={22} />}
          title={t('chatTryEmptyTitle')}
          hint={t('chatTryEmptyHint')}
          action={
            onLoadDemo ? (
              <button
                type="button"
                className="inline-flex h-8 items-center rounded-[10px] px-2.5 text-caption font-medium text-muted transition-colors hover:bg-[var(--sidebar-hover)] hover:text-foreground"
                onClick={onLoadDemo}
              >
                {t('chatDemoLoad')}
              </button>
            ) : undefined
          }
        />
      ) : (
        <div className="space-y-4">
          {messages.map((message) => (
            <ChatMessageView key={message.id} message={message} />
          ))}
        </div>
      )}
      <div ref={bottomRef} />
    </div>
  )
}
