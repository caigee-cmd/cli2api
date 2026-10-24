import type { StreamChatContentPart, StreamChatMessage } from '@/api/overview'
import type { ChatAttachment, ThreadMessage } from './types'

export function buildUserContent(text: string, attachments: ChatAttachment[]): string | StreamChatContentPart[] {
  const textFiles = attachments.filter((item) => item.kind === 'text')
  const images = attachments.filter((item) => item.kind === 'image' && item.dataUrl)
  let body = text.trim()
  for (const file of textFiles) {
    const chunk = file.text ?? ''
    body = `${body}${body ? '\n\n' : ''}--- file: ${file.name} ---\n${chunk}`
  }
  if (!images.length) return body

  const parts: StreamChatContentPart[] = []
  if (body) parts.push({ type: 'text', text: body })
  for (const image of images) {
    parts.push({ type: 'image_url', image_url: { url: image.dataUrl! } })
  }
  return parts
}

export function displayUserText(text: string, attachments: ChatAttachment[]) {
  if (!attachments.length) return text
  const names = attachments.map((item) => item.name).join(', ')
  return text.trim() ? text : names
}

/** History for the next request: prior turns + the new user turn. */
export function toApiMessages(history: ThreadMessage[], nextUser: StreamChatMessage): StreamChatMessage[] {
  const prior: StreamChatMessage[] = []
  for (const message of history) {
    if (message.status === 'error' && message.role === 'assistant' && !message.content) continue
    if (message.role === 'user') {
      prior.push({
        role: 'user',
        content: buildUserContent(message.content, message.attachments || []),
      })
      continue
    }
    if (message.content) {
      prior.push({ role: 'assistant', content: message.content })
    }
  }
  return [...prior, nextUser]
}
