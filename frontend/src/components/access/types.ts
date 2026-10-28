export type ChatAttachment = {
  id: string
  name: string
  mime: string
  kind: 'text' | 'image'
  text?: string
  dataUrl?: string
  size: number
}

export type ToolCallStatus = 'running' | 'success' | 'error'

export type ChatToolCall = {
  index: number
  id: string
  name: string
  arguments: string
  status: ToolCallStatus
}

export type ThreadMessageUsage = {
  prompt_tokens?: number
  completion_tokens?: number
  total_tokens?: number
  /** Present when the upstream billed subscription credits for this turn. */
  credits?: number
  source?: string
}

export type ThreadMessage = {
  id: string
  role: 'user' | 'assistant'
  content: string
  attachments?: ChatAttachment[]
  reasoning?: string
  reasoningOpen?: boolean
  toolCalls?: ChatToolCall[]
  /** Per-turn usage from the stream; show credits only when set. */
  usage?: ThreadMessageUsage
  /** Client-measured TTFT (ms) from stream start to first content/reasoning chunk. */
  ttfb_ms?: number
  /** Client-measured total stream duration (ms). */
  latency_ms?: number
  status: 'pending' | 'streaming' | 'done' | 'error'
  error?: string
}

export const TEXT_ATTACHMENT_EXTS = new Set([
  'txt', 'md', 'markdown', 'json', 'csv', 'tsv', 'yml', 'yaml', 'toml', 'xml', 'html', 'htm', 'css', 'scss',
  'js', 'jsx', 'ts', 'tsx', 'mjs', 'cjs', 'py', 'go', 'rs', 'java', 'kt', 'swift', 'c', 'h', 'cpp', 'hpp',
  'sh', 'bash', 'zsh', 'sql', 'graphql', 'env', 'ini', 'conf', 'log', 'rst', 'tex',
])

export const IMAGE_ATTACHMENT_MIMES = new Set([
  'image/png', 'image/jpeg', 'image/jpg', 'image/gif', 'image/webp',
])

export function attachmentExt(name: string) {
  const idx = name.lastIndexOf('.')
  return idx >= 0 ? name.slice(idx + 1).toLowerCase() : ''
}
