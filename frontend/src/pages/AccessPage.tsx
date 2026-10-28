import { CopyIcon } from '@/components/motion/CopyIcon'
import { copyText } from '@/lib/clipboard'
import { useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { Button, Chip, Tab, Tabs } from '@heroui/react'
import { ArrowsClockwise } from '@phosphor-icons/react'
import { useI18n } from '@/hooks/useI18n'
import { useOverview } from '@/hooks/useOverview'
import { buildRawPayload, fetchAccounts, fetchModels, streamChat, testRawApi, type RawApiFormat } from '@/api/overview'
import type { ModelInfo, Overview } from '@/api/types'
import type { StreamToolCall } from '@/api/overview'
import { absUrl } from '@/lib/url'
import { modelCreditsText, modelIsFree } from '@/lib/format'
import { AccessPageSkeleton } from '@/components/ui/PageSkeletons'
import { ProviderMark } from '@/components/ProviderMark'
import { accountProviderLabel } from '@/lib/provider'
import { EndpointList } from '@/components/EndpointList'
import { CaiaiSoftLink } from '@/components/CaiaiSoftLink'
import { ChatComposer, type ComposerOption } from '@/components/access/ChatComposer'
import { ChatHistoryRail, type ChatHistoryItem } from '@/components/access/ChatHistoryRail'
import { ChatThread } from '@/components/access/ChatThread'
import { RawApiPanel } from '@/components/access/RawApiPanel'
import { buildUserContent, toApiMessages } from '@/components/access/buildMessages'
import { buildDemoThinking, buildDemoThread } from '@/components/access/demoSeed'
import type { ChatAttachment, ChatToolCall, ThreadMessage } from '@/components/access/types'

type RequestState = 'idle' | 'loading' | 'success' | 'error'
type AccessTab = 'chat' | 'raw'

type ChatSession = {
  id: string
  title: string
  messages: ThreadMessage[]
  updatedAt: number
}

function shellQuote(value: string) {
  return `'${value.replaceAll("'", `'"'"'`)}'`
}

function newId(prefix: string) {
  return `${prefix}-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 8)}`
}

function mapToolCalls(calls: StreamToolCall[], streaming: boolean): ChatToolCall[] {
  return calls.map((call, index) => ({
    index: typeof call.index === 'number' ? call.index : index,
    id: call.id || `tool-${index}`,
    name: call.function?.name || '',
    arguments: call.function?.arguments || '',
    status: streaming ? 'running' : 'success',
  }))
}

function demoMode(): 'full' | 'thinking' | null {
  if (typeof window === 'undefined') return null
  try {
    const value = new URLSearchParams(window.location.search).get('demo')
    if (value === '1' || value === 'full') return 'full'
    if (value === 'thinking') return 'thinking'
    return null
  } catch {
    return null
  }
}

function initialDemoMessages(): ThreadMessage[] {
  const mode = demoMode()
  if (mode === 'thinking') return buildDemoThinking()
  if (mode === 'full') return buildDemoThread()
  return []
}

function titleFromMessages(messages: ThreadMessage[]) {
  const firstUser = messages.find((message) => message.role === 'user' && message.content.trim())
  if (!firstUser) return ''
  const text = firstUser.content.trim().replace(/\s+/g, ' ')
  return text.length > 36 ? `${text.slice(0, 36)}…` : text
}

function createSession(messages: ThreadMessage[] = []): ChatSession {
  const id = newId('session')
  return {
    id,
    title: titleFromMessages(messages),
    messages,
    updatedAt: Date.now(),
  }
}

const DEMO_MODEL_SEED: Array<{ id: string; display_name: string; credits?: string; free?: boolean; provider: string }> = [
  { id: 'hy3', display_name: 'HY3', credits: '0.75 credits', provider: 'workbuddy' },
  { id: 'glm-4.6', display_name: 'GLM-4.6', credits: '0.5 credits', provider: 'workbuddy' },
  { id: 'free-demo', display_name: 'Free Demo', credits: '0 credits', free: true, provider: 'workbuddy' },
]

export function AccessPage() {
  const { t } = useI18n()
  const { overview, loading } = useOverview()
  const [accessTab, setAccessTab] = useState<AccessTab>('chat')
  const [rawFormat, setRawFormat] = useState<RawApiFormat>('chat')
  const [historyCollapsed, setHistoryCollapsed] = useState(true)
  const [sessions, setSessions] = useState<ChatSession[]>(() => [createSession(initialDemoMessages())])
  const [activeSessionId, setActiveSessionId] = useState('')
  const resolvedSessionId = activeSessionId || sessions[0]?.id || ''
  const activeSession = sessions.find((session) => session.id === resolvedSessionId) || sessions[0]
  const messages = activeSession?.messages || []
  const [poolModels, setPoolModels] = useState<ModelInfo[]>([])
  const [accounts, setAccounts] = useState<NonNullable<Overview['accounts']>>([])
  useEffect(() => {
    let cancelled = false
    void Promise.allSettled([fetchModels(), fetchAccounts(false)]).then(([modelsResult, accountsResult]) => {
      if (cancelled) return
      if (modelsResult.status === 'fulfilled') setPoolModels(modelsResult.value.data || [])
      if (accountsResult.status === 'fulfilled') setAccounts(accountsResult.value.data || [])
    })
    return () => { cancelled = true }
  }, [])
  const base = absUrl(overview?.access?.openai_base_url || '/v1')
  const chatPath = overview?.access?.chat_completions || '/v1/chat/completions'
  const messagesPath = overview?.access?.messages || '/v1/messages'
  const responsesPath = overview?.access?.responses || '/v1/responses'
  const rawPath = rawFormat === 'messages'
    ? messagesPath
    : rawFormat === 'responses'
      ? responsesPath
      : chatPath
  const rawEndpoint = absUrl(rawPath)
  const [model, setModel] = useState('')
  const [accountId, setAccountId] = useState('')
  const [accountCatalog, setAccountCatalog] = useState<{ accountId: string; models: ModelInfo[]; error: string } | null>(null)
  const [prompt, setPrompt] = useState('只回复OK')
  const [chatPrompt, setChatPrompt] = useState('')
  const [attachments, setAttachments] = useState<ChatAttachment[]>([])
  const [chatState, setChatState] = useState<RequestState>('idle')
  const chatAbortRef = useRef<AbortController | null>(null)
  const [output, setOutput] = useState('')
  const [requestState, setRequestState] = useState<RequestState>('idle')
  const [elapsedMs, setElapsedMs] = useState<number | null>(null)
  const [copied, setCopied] = useState<'base' | 'curl' | ''>('')
  const selectedAccount = accountId || ''
  const catalogReady = !selectedAccount || accountCatalog?.accountId === selectedAccount
  const models = selectedAccount ? (catalogReady ? accountCatalog?.models || [] : []) : poolModels
  const modelsLoading = Boolean(selectedAccount) && !catalogReady
  const modelsError = catalogReady ? accountCatalog?.error || '' : ''
  const demoModelIds = demoMode() != null ? DEMO_MODEL_SEED.map((item) => item.id) : []
  const selectedModel = models.some((item) => item.id === model)
    ? model
    : demoModelIds.includes(model)
      ? model
      : (models[0]?.id || demoModelIds[0] || '')

  useEffect(() => {
    if (!selectedAccount) return
    let cancelled = false
    void fetchModels(selectedAccount)
      .then((data) => {
        if (cancelled) return
        setAccountCatalog({ accountId: selectedAccount, models: data.data || [], error: '' })
      })
      .catch((error) => {
        if (cancelled) return
        setAccountCatalog({
          accountId: selectedAccount,
          models: [],
          error: error instanceof Error ? error.message : String(error),
        })
      })
    return () => {
      cancelled = true
    }
  }, [selectedAccount])

  useEffect(() => () => {
    chatAbortRef.current?.abort()
  }, [])

  const readyAccounts = accounts.filter((account) => account.enabled !== false && (
    account.ready === true || account.hot === true || account.status === 'ready' || account.status === 'hot'
  ))

  const payload = useMemo(
    () => buildRawPayload(rawFormat, selectedModel, prompt),
    [prompt, rawFormat, selectedModel],
  )

  const curl = useMemo(
    () => `curl -sS ${shellQuote(rawEndpoint)} \\
  -H "Authorization: Bearer $CLI2API_API_KEY" \\
  -H ${shellQuote('Content-Type: application/json')}${selectedAccount ? ` \\
  -H ${shellQuote(`X-Qoder-Account: ${selectedAccount}`)}` : ''} \\
  -d ${shellQuote(JSON.stringify(payload))}`,
    [payload, rawEndpoint, selectedAccount],
  )

  if (loading && !overview) return <AccessPageSkeleton />

  async function copy(value: string, kind: 'base' | 'curl') {
    await copyText(value)
    setCopied(kind)
    window.setTimeout(() => setCopied(''), 1100)
  }

  async function onTest() {
    const startedAt = performance.now()
    setRequestState('loading')
    setElapsedMs(null)
    setOutput('')
    try {
      const data = await testRawApi(rawPath, payload, selectedAccount || undefined)
      setOutput(JSON.stringify(data, null, 2))
      setRequestState('success')
    } catch (error) {
      setOutput(error instanceof Error ? error.message : String(error))
      setRequestState('error')
    } finally {
      setElapsedMs(Math.round(performance.now() - startedAt))
    }
  }

  function stopChat() {
    chatAbortRef.current?.abort()
    chatAbortRef.current = null
  }

  function updateActiveMessages(
    next: ThreadMessage[] | ((prev: ThreadMessage[]) => ThreadMessage[]),
  ) {
    const targetId = resolvedSessionId
    setSessions((prev) => prev.map((session) => {
      if (session.id !== targetId) return session
      const messagesNext = typeof next === 'function' ? next(session.messages) : next
      return {
        ...session,
        messages: messagesNext,
        title: titleFromMessages(messagesNext) || session.title,
        updatedAt: Date.now(),
      }
    }))
  }

  function patchAssistant(id: string, patch: Partial<ThreadMessage> | ((prev: ThreadMessage) => Partial<ThreadMessage>)) {
    updateActiveMessages((prev) => prev.map((message) => {
      if (message.id !== id) return message
      const next = typeof patch === 'function' ? patch(message) : patch
      return { ...message, ...next }
    }))
  }

  async function onChatSend() {
    const content = chatPrompt.trim()
    if ((!content && !attachments.length) || !selectedModel || modelsLoading || chatState === 'loading') return
    stopChat()
    const controller = new AbortController()
    chatAbortRef.current = controller
    const pendingAttachments = attachments
    const userMessage: ThreadMessage = {
      id: newId('user'),
      role: 'user',
      content,
      attachments: pendingAttachments,
      status: 'done',
    }
    const assistantId = newId('assistant')
    const assistantMessage: ThreadMessage = {
      id: assistantId,
      role: 'assistant',
      content: '',
      status: 'streaming',
    }
    const history = messages
    updateActiveMessages([...history, userMessage, assistantMessage])
    setChatPrompt('')
    setAttachments([])
    setChatState('loading')

    const apiUser = {
      role: 'user' as const,
      content: buildUserContent(content, pendingAttachments),
    }
    const apiMessages = toApiMessages(history, apiUser)

    const startedAt = performance.now()
    let firstTokenAt: number | null = null
    const markFirstToken = () => {
      if (firstTokenAt == null) firstTokenAt = performance.now()
    }
    const timingPatch = (): Pick<ThreadMessage, 'ttfb_ms' | 'latency_ms'> => {
      const endedAt = performance.now()
      return {
        latency_ms: Math.max(0, Math.round(endedAt - startedAt)),
        ttfb_ms: firstTokenAt != null ? Math.max(0, Math.round(firstTokenAt - startedAt)) : undefined,
      }
    }

    try {
      await streamChat(selectedModel, apiMessages, selectedAccount || undefined, {
        signal: controller.signal,
        onContent: (_chunk, full) => {
          if (_chunk) markFirstToken()
          patchAssistant(assistantId, { content: full })
        },
        onReasoning: (_chunk, full) => {
          if (_chunk) markFirstToken()
          patchAssistant(assistantId, { reasoning: full })
        },
        onToolCalls: (calls) => {
          markFirstToken()
          patchAssistant(assistantId, { toolCalls: mapToolCalls(calls, true) })
        },
        onUsage: (usage) => patchAssistant(assistantId, { usage }),
      })
      patchAssistant(assistantId, (prev) => ({
        status: 'done',
        ...timingPatch(),
        toolCalls: prev.toolCalls?.map((call) => ({ ...call, status: 'success' as const })),
      }))
      setChatState('success')
    } catch (error) {
      if (controller.signal.aborted) {
        patchAssistant(assistantId, (prev) => ({
          status: 'done',
          ...timingPatch(),
          toolCalls: prev.toolCalls?.map((call) => ({ ...call, status: 'success' as const })),
        }))
        setChatState('success')
        return
      }
      const message = error instanceof Error ? error.message : String(error)
      patchAssistant(assistantId, { status: 'error', error: message, ...timingPatch() })
      setChatState('error')
    } finally {
      if (chatAbortRef.current === controller) chatAbortRef.current = null
    }
  }

  const responseStatus = requestState === 'success'
    ? t('requestComplete')
    : requestState === 'error'
      ? t('requestFailed')
      : requestState === 'loading'
        ? t('requesting')
        : t('requestIdle')

  const previewDemo = demoMode() != null

  const liveAccountOptions: ComposerOption[] = [
    {
      id: 'auto',
      textValue: t('autoAccount'),
      label: t('autoAccount'),
      icon: <ArrowsClockwise size={14} className="text-muted" />,
    },
    ...accounts.map((account) => ({
      id: account.id,
      textValue: `${account.name || account.id} ${accountProviderLabel(account.provider, account.region, t)}`,
      label: account.name || account.id,
      hint: accountProviderLabel(account.provider, account.region, t),
      icon: <ProviderMark provider={account.provider} size={14} /> as ReactNode,
    })),
  ]
  const accountOptions: ComposerOption[] = accounts.length || !previewDemo
    ? liveAccountOptions
    : [
        {
          id: 'auto',
          textValue: t('autoAccount'),
          label: t('autoAccount'),
          icon: <ArrowsClockwise size={14} className="text-muted" />,
        },
        {
          id: 'demo-wb',
          textValue: 'wb-demo WorkBuddy',
          label: 'wb-demo',
          hint: 'WorkBuddy',
        },
      ]

  const liveModelOptions: ComposerOption[] = models.map((item) => {
    const credits = modelCreditsText(item)
    const free = modelIsFree(item)
    const title = item.display_name || item.id
    const badge = free ? t('modelFree') : credits
    const provider = item.provider || item.owned_by || ''
    return {
      id: item.id,
      textValue: `${title} ${item.id} ${provider} ${credits} ${free ? 'free' : ''}`,
      label: badge ? `${title} ${badge}` : title,
      hint: [item.id, provider, badge].filter(Boolean).join(' · '),
    }
  })
  const modelOptions: ComposerOption[] = liveModelOptions.length || !previewDemo
    ? liveModelOptions
    : DEMO_MODEL_SEED.map((item) => {
        const credits = modelCreditsText(item)
        const free = modelIsFree(item)
        const title = item.display_name || item.id
        const badge = free ? t('modelFree') : credits
        return {
          id: item.id,
          textValue: `${title} ${item.id} ${item.provider} ${credits} ${free ? 'free' : ''}`,
          label: badge ? `${title} ${badge}` : title,
          hint: [item.id, item.provider, badge].filter(Boolean).join(' · '),
        }
      })

  const canChatSend = Boolean(
    (chatPrompt.trim() || attachments.length)
    && selectedModel
    && !modelsLoading
    && (accounts.length || previewDemo),
  )

  function loadDemo() {
    updateActiveMessages(buildDemoThread())
    setChatState('idle')
  }

  function onNewChat() {
    stopChat()
    setChatState('idle')
    setChatPrompt('')
    setAttachments([])
    const session = createSession([])
    setSessions((prev) => [session, ...prev])
    setActiveSessionId(session.id)
  }

  function onSelectSession(id: string) {
    if (id === resolvedSessionId) return
    stopChat()
    setChatState('idle')
    setChatPrompt('')
    setAttachments([])
    setActiveSessionId(id)
  }

  const historyItems: ChatHistoryItem[] = sessions
    .slice()
    .sort((a, b) => b.updatedAt - a.updatedAt)
    .map((session) => ({
      id: session.id,
      title: session.title || t('chatHistoryUntitled'),
      updatedAt: session.updatedAt,
    }))

  return (
    <div className="access-page space-y-4">
      <section className="access-info-row flex flex-wrap items-center gap-x-3 gap-y-2 px-0.5 py-0.5" title={t('apiPlaygroundHint')}>
        <h2 className="shrink-0 text-body font-semibold text-foreground">{t('apiPlayground')}</h2>
        <div className="flex min-w-0 flex-1 items-center gap-1.5 text-caption text-muted">
          <span className="shrink-0">{t('baseUrl')}</span>
          <code className="mono min-w-0 truncate font-medium text-foreground">{base}</code>
          <Button isIconOnly size="sm" variant="ghost" className="size-6 min-w-6 shrink-0" aria-label={t('copyBaseUrl')} onPress={() => void copy(base, 'base')}>
            <CopyIcon copied={copied === 'base'} size={13} />
          </Button>
        </div>
        <div className="flex items-center gap-2 text-caption text-muted">
          <span className="status-dot" data-state={readyAccounts.length ? 'ok' : undefined} />
          <span>{t('readyAccounts', { ready: readyAccounts.length, total: accounts.length })}</span>
          <Chip size="sm" variant="soft" color={readyAccounts.length ? 'success' : 'warning'}>
            {readyAccounts.length ? t('endpointReady') : t('degraded')}
          </Chip>
        </div>
      </section>

      {!accounts.length ? (
        <div className="space-y-1 px-1">
          <p className="text-caption text-muted">{t('noAccountsForTest')}</p>
          <CaiaiSoftLink />
        </div>
      ) : null}

      {modelsError ? (
        <p className="px-1 text-caption text-danger">{modelsError}</p>
      ) : null}

      <Tabs selectedKey={accessTab} onSelectionChange={(key) => setAccessTab(String(key) as AccessTab)}>
        <Tabs.ListContainer className="w-fit">
          <Tabs.List aria-label={t('accessTabsLabel')}>
            <Tab id="chat" className="gap-1.5 whitespace-nowrap">
              <Tabs.Indicator className="transition-none" />
              {t('accessTabChat')}
            </Tab>
            <Tab id="raw" className="gap-1.5 whitespace-nowrap">
              <Tabs.Indicator className="transition-none" />
              {t('accessTabApi')}
            </Tab>
          </Tabs.List>
        </Tabs.ListContainer>

        <Tabs.Panel id="chat" className="pt-4">
          {/* Flat cohesion: hist rail + stage peers on the page — no outer shell box. */}
          <div className="flex min-h-[640px] md:min-h-[72vh]">
            <ChatHistoryRail
              collapsed={historyCollapsed}
              onCollapsedChange={setHistoryCollapsed}
              items={historyItems}
              activeId={resolvedSessionId}
              onSelect={onSelectSession}
              onNew={onNewChat}
            />
            <div className="access-chat-stage flex min-w-0 flex-1 flex-col">
              <ChatThread
                messages={messages}
                busy={chatState === 'loading'}
                onLoadDemo={loadDemo}
              />
              <ChatComposer
                value={chatPrompt}
                onChange={setChatPrompt}
                attachments={attachments}
                onAttachmentsChange={setAttachments}
                disabled={!accounts.length && demoMode() == null}
                streaming={chatState === 'loading'}
                canSend={canChatSend}
                onSend={() => void onChatSend()}
                onStop={stopChat}
                accountValue={selectedAccount || 'auto'}
                onAccountChange={(next) => setAccountId(next === 'auto' ? '' : next)}
                accountOptions={accountOptions}
                modelValue={selectedModel}
                onModelChange={setModel}
                modelOptions={modelOptions}
                modelsLoading={modelsLoading}
              />
            </div>
          </div>
        </Tabs.Panel>

        <Tabs.Panel id="raw" className="space-y-4 pt-4">
          <RawApiPanel
            format={rawFormat}
            onFormatChange={setRawFormat}
            requestPath={rawPath}
            prompt={prompt}
            onPromptChange={setPrompt}
            onTest={() => void onTest()}
            requestState={requestState}
            elapsedMs={elapsedMs}
            output={output}
            responseStatus={responseStatus}
            curl={curl}
            copiedCurl={copied === 'curl'}
            onCopyCurl={() => void copy(curl, 'curl')}
            canSend={Boolean(prompt.trim() && selectedModel && !modelsLoading)}
            accountValue={selectedAccount || 'auto'}
            onAccountChange={(next) => setAccountId(next === 'auto' ? '' : next)}
            accountOptions={accountOptions}
            modelValue={selectedModel}
            onModelChange={setModel}
            modelOptions={modelOptions}
            modelsLoading={modelsLoading}
            disabled={!accounts.length && demoMode() == null}
          />
          <EndpointList access={overview?.access} />
        </Tabs.Panel>
      </Tabs>
    </div>
  )
}
