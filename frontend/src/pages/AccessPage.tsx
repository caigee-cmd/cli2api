import { CopyIcon } from '@/components/motion/CopyIcon'
import { copyText } from '@/lib/clipboard'
import { useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { Button, Card, Chip, Description, Label, ListBox, Select, Skeleton } from '@heroui/react'
import { ArrowsClockwise } from '@phosphor-icons/react'
import { useI18n } from '@/hooks/useI18n'
import { useOverview } from '@/hooks/useOverview'
import { fetchAccounts, fetchModels, streamChat, testChat } from '@/api/overview'
import type { ModelInfo, Overview } from '@/api/types'
import type { StreamToolCall } from '@/api/overview'
import { absUrl } from '@/lib/url'
import { modelCreditsText, modelIsFree } from '@/lib/format'
import { PageAlert } from '@/components/ui/PageAlert'
import { AccessPageSkeleton } from '@/components/ui/PageSkeletons'
import { ProviderMark } from '@/components/ProviderMark'
import { accountProviderLabel } from '@/lib/provider'
import { EndpointList } from '@/components/EndpointList'
import { ChatComposer } from '@/components/access/ChatComposer'
import { ChatThread } from '@/components/access/ChatThread'
import { RawApiPanel } from '@/components/access/RawApiPanel'
import { buildUserContent, toApiMessages } from '@/components/access/buildMessages'
import type { ChatAttachment, ChatToolCall, ThreadMessage } from '@/components/access/types'

type RequestState = 'idle' | 'loading' | 'success' | 'error'

type PlaygroundOption = {
  id: string
  textValue: string
  label: string
  hint?: string
  icon?: ReactNode
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

function PlaygroundSelect({
  label,
  value,
  onChange,
  placeholder,
  isDisabled,
  options,
}: {
  label: string
  value: string
  onChange: (value: string) => void
  placeholder?: string
  isDisabled?: boolean
  options: PlaygroundOption[]
}) {
  const selected = options.find((option) => option.id === value)
  return (
    <Select
      fullWidth
      value={value || null}
      placeholder={placeholder}
      isDisabled={isDisabled}
      onChange={(next) => {
        if (typeof next === 'string' && next) onChange(next)
      }}
    >
      <Label className="text-body font-medium text-muted">{label}</Label>
      <Select.Trigger className="items-center">
        <Select.Value className="min-w-0 truncate">
          {({ defaultChildren, isPlaceholder }) => {
            if (isPlaceholder || !selected) return defaultChildren
            return (
              <span className="flex min-w-0 items-center gap-2">
                {selected.icon ? <span className="grid size-5 shrink-0 place-items-center">{selected.icon}</span> : null}
                <span className="truncate">{selected.label}</span>
              </span>
            )
          }}
        </Select.Value>
        <Select.Indicator />
      </Select.Trigger>
      <Select.Popover className="max-h-72">
        <ListBox>
          {options.map((option) => (
            <ListBox.Item key={option.id} id={option.id} textValue={option.textValue}>
              {option.icon ? <span className="grid size-5 shrink-0 place-items-center">{option.icon}</span> : null}
              <div className="min-w-0 flex-1">
                <Label className="block truncate">{option.label}</Label>
                {option.hint ? <Description className="truncate">{option.hint}</Description> : null}
              </div>
              <ListBox.ItemIndicator />
            </ListBox.Item>
          ))}
        </ListBox>
      </Select.Popover>
    </Select>
  )
}

export function AccessPage() {
  const { t } = useI18n()
  const { overview, loading } = useOverview()
  const [rawOpen, setRawOpen] = useState(false)
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
  const chatEndpoint = absUrl(chatPath)
  const [model, setModel] = useState('')
  const [accountId, setAccountId] = useState('')
  const [accountCatalog, setAccountCatalog] = useState<{ accountId: string; models: ModelInfo[]; error: string } | null>(null)
  const [prompt, setPrompt] = useState('只回复OK')
  const [chatPrompt, setChatPrompt] = useState('')
  const [attachments, setAttachments] = useState<ChatAttachment[]>([])
  const [messages, setMessages] = useState<ThreadMessage[]>([])
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
  const selectedModel = models.some((item) => item.id === model) ? model : models[0]?.id || ''

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

  const payload = useMemo(() => ({
    model: selectedModel,
    stream: false,
    messages: [{ role: 'user', content: prompt || '只回复OK' }],
  }), [prompt, selectedModel])

  const curl = useMemo(
    () => `curl -sS ${shellQuote(chatEndpoint)} \\
  -H "Authorization: Bearer $CLI2API_API_KEY" \\
  -H ${shellQuote('Content-Type: application/json')}${selectedAccount ? ` \\
  -H ${shellQuote(`X-Qoder-Account: ${selectedAccount}`)}` : ''} \\
  -d ${shellQuote(JSON.stringify(payload))}`,
    [chatEndpoint, payload, selectedAccount],
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
      const data = await testChat(payload.model, payload.messages[0].content, selectedAccount || undefined)
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

  function patchAssistant(id: string, patch: Partial<ThreadMessage> | ((prev: ThreadMessage) => Partial<ThreadMessage>)) {
    setMessages((prev) => prev.map((message) => {
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
    setMessages([...history, userMessage, assistantMessage])
    setChatPrompt('')
    setAttachments([])
    setChatState('loading')

    const apiUser = {
      role: 'user' as const,
      content: buildUserContent(content, pendingAttachments),
    }
    const apiMessages = toApiMessages(history, apiUser)

    try {
      await streamChat(selectedModel, apiMessages, selectedAccount || undefined, {
        signal: controller.signal,
        onContent: (_chunk, full) => patchAssistant(assistantId, { content: full }),
        onReasoning: (_chunk, full) => patchAssistant(assistantId, { reasoning: full }),
        onToolCalls: (calls) => patchAssistant(assistantId, { toolCalls: mapToolCalls(calls, true) }),
      })
      patchAssistant(assistantId, (prev) => ({
        status: 'done',
        toolCalls: prev.toolCalls?.map((call) => ({ ...call, status: 'success' as const })),
      }))
      setChatState('success')
    } catch (error) {
      if (controller.signal.aborted) {
        patchAssistant(assistantId, (prev) => ({
          status: 'done',
          toolCalls: prev.toolCalls?.map((call) => ({ ...call, status: 'success' as const })),
        }))
        setChatState('success')
        return
      }
      const message = error instanceof Error ? error.message : String(error)
      patchAssistant(assistantId, { status: 'error', error: message })
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

  const accountOptions: PlaygroundOption[] = [
    {
      id: 'auto',
      textValue: t('autoAccount'),
      label: t('autoAccount'),
      icon: <ArrowsClockwise size={15} className="text-muted" />,
    },
    ...accounts.map((account) => ({
      id: account.id,
      textValue: `${account.name || account.id} ${accountProviderLabel(account.provider, account.region, t)}`,
      label: account.name || account.id,
      hint: accountProviderLabel(account.provider, account.region, t),
      icon: <ProviderMark provider={account.provider} size={16} />,
    })),
  ]

  const modelOptions: PlaygroundOption[] = models.map((item) => {
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

  const canChatSend = Boolean(
    (chatPrompt.trim() || attachments.length)
    && selectedModel
    && !modelsLoading
    && accounts.length,
  )

  return (
    <div className="space-y-5">
      <section className="grid gap-3 pb-1 lg:grid-cols-[minmax(0,1fr)_auto] lg:items-end">
        <div>
          <h2 className="text-title font-semibold">{t('apiPlayground')}</h2>
          <p className="mt-1 max-w-2xl text-body leading-6 text-muted">{t('apiPlaygroundHint')}</p>
        </div>
        <div className="flex items-center gap-2 text-caption text-muted">
          <span className="status-dot" data-state={readyAccounts.length ? 'ok' : undefined} />
          <span>{t('readyAccounts', { ready: readyAccounts.length, total: accounts.length })}</span>
        </div>
      </section>

      <section className="overflow-hidden rounded-xl bg-surface shadow-surface">
        <div className="flex flex-wrap items-center justify-between gap-3 px-4 py-3">
          <div className="min-w-0">
            <div className="text-caption font-medium text-muted">{t('baseUrl')}</div>
            <code className="mono mt-0.5 block truncate text-body font-medium text-foreground">{base}</code>
          </div>
          <div className="flex items-center gap-2">
            <Button isIconOnly size="sm" variant="ghost" aria-label={t('copyBaseUrl')} onPress={() => void copy(base, 'base')}>
              <CopyIcon copied={copied === 'base'} size={15} />
            </Button>
            <Chip size="sm" variant="soft" color={readyAccounts.length ? 'success' : 'warning'}>
              {readyAccounts.length ? t('endpointReady') : t('degraded')}
            </Chip>
          </div>
        </div>
      </section>

      <Card className="overflow-hidden p-0">
        <div className="grid gap-4 p-4 sm:grid-cols-2 sm:gap-5 sm:p-5">
          <div className="space-y-1.5">
            <PlaygroundSelect
              label={t('account')}
              value={selectedAccount || 'auto'}
              onChange={(next) => setAccountId(next === 'auto' ? '' : next)}
              options={accountOptions}
            />
            <p className="text-caption leading-5 text-muted">{selectedAccount ? t('fixedAccountHint') : t('autoAccountHint')}</p>
          </div>
          <div className="space-y-1.5">
            {modelsLoading ? (
              <div className="flex flex-col gap-1">
                <div className="text-body font-medium text-muted">{t('model')}</div>
                <Skeleton className="h-10 rounded-lg" />
              </div>
            ) : models.length ? (
              <PlaygroundSelect
                label={t('model')}
                value={selectedModel}
                onChange={setModel}
                placeholder={t('model')}
                options={modelOptions}
              />
            ) : (
              <div className="flex flex-col gap-1">
                <div className="text-body font-medium text-muted">{t('model')}</div>
                <div className="flex h-10 items-center rounded-lg bg-surface-secondary px-3 text-caption leading-5 text-muted">
                  {modelsError || (selectedAccount ? t('noAccountModels') : t('noModelsYet'))}
                </div>
              </div>
            )}
            <p className="text-caption leading-5 text-muted">{selectedAccount ? t('accountModelHint') : t('modelRoutingHint')}</p>
          </div>
        </div>
        {!accounts.length ? (
          <div className="px-4 pb-4 sm:px-5 sm:pb-5">
            <PageAlert status="warning" title={t('noAccountsForTest')} />
          </div>
        ) : null}
      </Card>

      <Card className="overflow-hidden p-0">
        <div className="flex min-h-[640px] flex-col md:min-h-[70vh]">
          <ChatThread messages={messages} busy={chatState === 'loading'} />
          <ChatComposer
            value={chatPrompt}
            onChange={setChatPrompt}
            attachments={attachments}
            onAttachmentsChange={setAttachments}
            disabled={!accounts.length}
            streaming={chatState === 'loading'}
            canSend={canChatSend}
            onSend={() => void onChatSend()}
            onStop={stopChat}
          />
        </div>
      </Card>

      <RawApiPanel
        open={rawOpen}
        onOpenChange={setRawOpen}
        chatPath={chatPath}
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
      />

      <EndpointList access={overview?.access} />
    </div>
  )
}
