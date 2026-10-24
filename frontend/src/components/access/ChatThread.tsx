import { useEffect, useRef } from 'react'
import { ChatTeardropText } from '@phosphor-icons/react'
import { EmptyPanel } from '@/components/ui/EmptyPanel'
import { useI18n } from '@/hooks/useI18n'
import { ChatMessageView } from './ChatMessage'
import type { ThreadMessage } from './types'

export function ChatThread({
  messages,
  busy,
}: {
  messages: ThreadMessage[]
  busy: boolean
}) {
  const { t } = useI18n()
  const bottomRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ block: 'end' })
  }, [messages, busy])

  return (
    <div
      className="min-h-0 flex-1 space-y-4 overflow-y-auto p-5 sm:p-6"
      aria-live="polite"
      aria-busy={busy}
    >
      {!messages.length ? (
        <EmptyPanel
          className="min-h-72"
          icon={<ChatTeardropText size={21} />}
          title={t('chatTryEmptyTitle')}
          hint={t('chatTryEmptyHint')}
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
