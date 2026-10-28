import type { ChatAttachment, ThreadMessage } from './types'

/** Tiny 1×1 PNG (zinc gray) for demo image chip — no network. */
const DEMO_PNG =
  'data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAoAAAAKCAYAAACNMs+9AAAAFUlEQVR42mNkYPhfz0AEYBxVSF+FABJADveWkH6aAAAAAElFTkSuQmCC'

export function buildDemoThread(): ThreadMessage[] {
  const image: ChatAttachment = {
    id: 'demo-img-1',
    name: 'screenshot.png',
    mime: 'image/png',
    kind: 'image',
    dataUrl: DEMO_PNG,
    size: 2048,
  }
  const note: ChatAttachment = {
    id: 'demo-txt-1',
    name: 'notes.md',
    mime: 'text/markdown',
    kind: 'text',
    text: '# notes\nplaceholder',
    size: 128,
  }
  return [
    {
      id: 'demo-user-1',
      role: 'user',
      content: 'Summarize the attached screenshot and look up the endpoint.',
      attachments: [image, note],
      status: 'done',
    },
    {
      id: 'demo-assistant-1',
      role: 'assistant',
      content:
        'The screenshot shows the Access playground. I looked up the chat completions route and drafted a short summary below.',
      reasoning:
        'User attached a UI screenshot and a notes file.\nI should call the models/list helper, then summarize the visible chrome without inventing product claims.\nKeep the reply short and ops-console toned.',
      toolCalls: [
        {
          index: 0,
          id: 'call_demo_list',
          name: 'list_endpoints',
          arguments: JSON.stringify({ base: '/v1', filter: 'chat' }, null, 2),
          status: 'success',
        },
        {
          index: 1,
          id: 'call_demo_read',
          name: 'read_attachment',
          arguments: JSON.stringify({ name: 'notes.md', max_chars: 400 }, null, 2),
          status: 'success',
        },
      ],
      reasoningOpen: false,
      usage: { credits: 0.75, prompt_tokens: 1840, completion_tokens: 126, source: 'upstream' },
      ttfb_ms: 380,
      latency_ms: 2140,
      status: 'done',
    },
  ]
}

/** Streaming thinking pill for visual QA (`?demo=thinking`). */
export function buildDemoThinking(): ThreadMessage[] {
  return [
    {
      id: 'demo-user-thinking',
      role: 'user',
      content: 'Walk through the Access playground layout.',
      status: 'done',
    },
    {
      id: 'demo-assistant-thinking',
      role: 'assistant',
      content: '',
      reasoning:
        'Scan the stage chrome, then the composer controls, then draft a short reply without inventing product claims.',
      status: 'streaming',
    },
  ]
}
