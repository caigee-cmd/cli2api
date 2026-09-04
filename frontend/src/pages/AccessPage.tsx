import { useEffect, useMemo, useState, type ReactNode } from 'react'
import { Button, Card, Chip, Description, Label, ListBox, Select, Skeleton, TextArea } from '@heroui/react'
import {
  ArrowsClockwise,
  BracketsCurly,
  Check,
  CheckCircle,
  Clock,
  Copy,
  PaperPlaneTilt,
  TerminalWindow,
} from '@phosphor-icons/react'
import { useI18n } from '@/hooks/useI18n'
import { useOverview } from '@/hooks/useOverview'
import { fetchModels, testChat } from '@/api/overview'
import type { ModelInfo } from '@/api/types'
import { absUrl } from '@/lib/url'
import { EmptyPanel } from '@/components/ui/EmptyPanel'
import { PageAlert } from '@/components/ui/PageAlert'
import { AccessPageSkeleton } from '@/components/ui/PageSkeletons'
import { ProviderMark } from '@/components/ProviderMark'
import { accountProviderLabel } from '@/lib/provider'
import { EndpointList } from '@/components/EndpointList'

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
      <Label className="text-sm font-medium text-muted">{label}</Label>
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
  const poolModels = overview?.models || []
  const accounts = overview?.accounts || []
  const base = absUrl(overview?.access?.openai_base_url || '/v1')
  const chatPath = overview?.access?.chat_completions || '/v1/chat/completions'
  const chatEndpoint = absUrl(chatPath)
  const [model, setModel] = useState('')
  const [accountId, setAccountId] = useState('')
  const [accountCatalog, setAccountCatalog] = useState<{ accountId: string; models: ModelInfo[]; error: string } | null>(null)
  const [prompt, setPrompt] = useState('只回复OK')
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
    await navigator.clipboard.writeText(value)
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

  const responseStatus = requestState === 'success'
    ? t('requestComplete')
    : requestState === 'error'
      ? t('requestFailed')
      : requestState === 'loading'
        ? t('requesting')
        : t('requestIdle')

  return (
    <div className="space-y-6">
      <section className="grid gap-5 border-b border-separator pb-6 lg:grid-cols-[minmax(0,1fr)_auto] lg:items-end">
        <div data-gsap-reveal>
          <h2 className="text-2xl font-semibold tracking-[-0.035em]">{t('apiPlayground')}</h2>
          <p className="mt-1 max-w-2xl text-sm leading-6 text-muted">{t('apiPlaygroundHint')}</p>
        </div>
        <div className="flex items-center gap-2 text-xs text-muted">
          <span className="status-dot" data-state={readyAccounts.length ? 'ok' : undefined} />
          <span>{t('readyAccounts', { ready: readyAccounts.length, total: accounts.length })}</span>
        </div>
      </section>

      <section data-gsap-reveal className="grid overflow-hidden rounded-3xl border border-border bg-surface sm:grid-cols-3">
        <div className="border-b border-separator px-5 py-4 sm:col-span-3">
          <div className="flex items-center justify-between gap-4">
            <div>
              <h3 className="font-semibold tracking-[-0.015em]">{t('connection')}</h3>
              <p className="mt-0.5 text-xs leading-5 text-muted">{t('clientConfigHint')}</p>
            </div>
            <Chip size="sm" variant="soft" color={readyAccounts.length ? 'success' : 'warning'}>
              {readyAccounts.length ? t('endpointReady') : t('degraded')}
            </Chip>
          </div>
        </div>
        <div className="min-w-0 border-b border-separator p-4 sm:col-span-2 sm:border-r sm:border-b-0 sm:p-5">
          <div className="mb-2 flex items-center justify-between gap-3">
            <span className="text-xs font-medium text-muted">{t('baseUrl')}</span>
            <Button isIconOnly size="sm" variant="ghost" aria-label={t('copyBaseUrl')} onPress={() => void copy(base, 'base')}>
              {copied === 'base' ? <Check size={15} /> : <Copy size={15} />}
            </Button>
          </div>
          <code className="mono block truncate text-sm font-medium text-foreground">{base}</code>
        </div>
        <div className="grid grid-cols-2 divide-x divide-separator sm:grid-cols-1 sm:divide-x-0 sm:divide-y">
          <div className="p-4 sm:px-5 sm:py-3">
            <div className="text-xs font-medium text-muted">{t('protocol')}</div>
            <div className="mt-1 text-sm font-medium">HTTP / SSE</div>
          </div>
          <div className="p-4 sm:px-5 sm:py-3">
            <div className="text-xs font-medium text-muted">{t('authentication')}</div>
            <div className="mono mt-1 truncate text-xs font-medium">Bearer API key</div>
          </div>
        </div>
      </section>

      <EndpointList access={overview?.access} />

      <Card data-gsap-reveal className="overflow-hidden p-0">
        <div className="grid xl:grid-cols-[minmax(440px,.92fr)_minmax(0,1.08fr)]">
          <div className="border-b border-separator xl:border-r xl:border-b-0">
            <div className="border-b border-separator px-5 py-5 sm:px-7">
              <div className="flex items-center gap-3">
                <div className="grid size-8 place-items-center rounded-lg bg-surface-secondary text-foreground">
                  <PaperPlaneTilt size={15} weight="bold" />
                </div>
                <div>
                  <h3 className="font-semibold tracking-[-0.015em]">{t('requestBuilder')}</h3>
                  <p className="mt-0.5 text-xs text-muted">POST {chatPath}</p>
                </div>
              </div>
            </div>

            <div className="space-y-6 p-5 sm:p-7">
              <div className="grid gap-5 sm:grid-cols-2">
                <div className="space-y-2">
                  <PlaygroundSelect
                    label={t('account')}
                    value={selectedAccount || 'auto'}
                    onChange={(next) => setAccountId(next === 'auto' ? '' : next)}
                    options={[
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
                    ]}
                  />
                  <p className="text-xs leading-5 text-muted">{selectedAccount ? t('fixedAccountHint') : t('autoAccountHint')}</p>
                </div>

                <div className="space-y-2">
                  {modelsLoading ? (
                    <div className="flex flex-col gap-1">
                      <div className="text-sm font-medium text-muted">{t('model')}</div>
                      <Skeleton className="h-10 rounded-lg" />
                    </div>
                  ) : models.length ? (
                    <PlaygroundSelect
                      label={t('model')}
                      value={selectedModel}
                      onChange={setModel}
                      placeholder={t('model')}
                      options={models.map((item) => ({
                        id: item.id,
                        textValue: `${item.display_name || item.id} ${item.id} ${item.owned_by || item.provider || ''}`,
                        label: item.display_name || item.id,
                        hint: item.provider || item.owned_by ? `${item.id} · ${item.provider || item.owned_by}` : item.id,
                      }))}
                    />
                  ) : (
                    <div className="flex flex-col gap-1">
                      <div className="text-sm font-medium text-muted">{t('model')}</div>
                      <div className="flex h-10 items-center rounded-lg border border-dashed border-border px-3 text-xs leading-5 text-muted">
                        {modelsError || (selectedAccount ? t('noAccountModels') : t('noModelsYet'))}
                      </div>
                    </div>
                  )}
                  <p className="text-xs leading-5 text-muted">{selectedAccount ? t('accountModelHint') : t('modelRoutingHint')}</p>
                </div>
              </div>

              <div className="space-y-3">
                <div className="flex items-center justify-between gap-4">
                  <div className="text-sm font-medium text-muted">{t('prompt')}</div>
                  <span className="mono text-[10px] text-muted">{prompt.length}</span>
                </div>
                <TextArea
                  fullWidth
                  rows={9}
                  className="min-h-56 resize-y px-4 py-3 text-sm leading-6"
                  value={prompt}
                  onChange={(event) => setPrompt(event.target.value)}
                  placeholder={t('promptPlaceholder')}
                  aria-label={t('prompt')}
                />
                <p className="text-xs leading-5 text-muted">{t('promptHelper')}</p>
              </div>

              {!accounts.length ? (
                <PageAlert status="warning" title={t('noAccountsForTest')} />
              ) : null}

              <Button fullWidth isPending={requestState === 'loading'} isDisabled={!prompt.trim() || !selectedModel || modelsLoading} onPress={() => void onTest()}>
                <PaperPlaneTilt size={16} />
                {requestState === 'loading' ? t('requesting') : t('sendRequest')}
              </Button>
            </div>
          </div>

          <div className="flex min-h-[620px] min-w-0 flex-col bg-surface-secondary/45" aria-live="polite" aria-busy={requestState === 'loading'}>
            <div className="flex min-h-16 items-center justify-between gap-4 border-b border-separator px-5 py-4 sm:px-6">
              <div>
                <h3 className="font-semibold tracking-[-0.015em]">{t('responseInspector')}</h3>
                <p className="mt-0.5 text-xs text-muted">{t('responseInspectorHint')}</p>
              </div>
              <div className="flex items-center gap-2">
                {elapsedMs !== null ? (
                  <span className="mono flex items-center gap-1.5 text-[10px] text-muted">
                    <Clock size={12} />
                    {elapsedMs} ms
                  </span>
                ) : null}
                <Chip
                  size="sm"
                  variant="soft"
                  color={requestState === 'success' ? 'success' : requestState === 'error' ? 'danger' : undefined}
                >
                  {responseStatus}
                </Chip>
              </div>
            </div>

            <div className="min-h-0 flex-1 p-5 sm:p-6">
              {requestState === 'idle' ? (
                <EmptyPanel
                  className="min-h-80"
                  icon={<BracketsCurly size={21} />}
                  title={t('responseEmptyTitle')}
                  hint={t('responseEmptyHint')}
                />
              ) : requestState === 'loading' ? (
                <div className="space-y-3 pt-1">
                  <Skeleton className="h-3 w-32 rounded-lg" />
                  <Skeleton className="h-3 w-full rounded-lg" />
                  <Skeleton className="h-3 w-[88%] rounded-lg" />
                  <Skeleton className="h-3 w-[72%] rounded-lg" />
                  <Skeleton className="mt-7 h-3 w-[92%] rounded-lg" />
                  <Skeleton className="h-3 w-[64%] rounded-lg" />
                </div>
              ) : requestState === 'error' ? (
                <div className="space-y-3">
                  <PageAlert status="danger" title={t('requestFailed')} description={output} />
                </div>
              ) : (
                <div>
                  <div className="mb-4 flex items-center gap-2 text-xs font-medium text-success">
                    <CheckCircle size={15} weight="fill" />
                    {t('responseReceived')}
                  </div>
                  <pre className="mono overflow-x-auto whitespace-pre-wrap break-words text-xs leading-6 text-muted">{output}</pre>
                </div>
              )}
            </div>
          </div>
        </div>
      </Card>

      <section data-gsap-reveal className="overflow-hidden rounded-3xl border border-border bg-surface">
        <div className="flex items-center justify-between gap-4 border-b border-separator px-5 py-3.5">
          <div className="flex min-w-0 items-center gap-3">
            <TerminalWindow className="shrink-0 text-muted" size={16} />
            <div className="min-w-0">
              <div className="text-sm font-semibold">{t('curlExample')}</div>
              <div className="mt-0.5 truncate text-xs text-muted">{t('curlGeneratedHint')}</div>
            </div>
          </div>
          <Button size="sm" variant="ghost" onPress={() => void copy(curl, 'curl')}>
            {copied === 'curl' ? <Check size={14} /> : <Copy size={14} />}
            {copied === 'curl' ? t('copied') : t('copy')}
          </Button>
        </div>
        <pre className="mono max-h-80 overflow-auto whitespace-pre-wrap p-5 text-xs leading-6 text-muted">{curl}</pre>
      </section>
    </div>
  )
}
