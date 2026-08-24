import { useMemo, useState } from 'react'
import { Button, Card, Chip, Label, ListBox, Select, Skeleton, TextArea } from '@heroui/react'
import {
  BracketsCurly,
  Check,
  CheckCircle,
  Clock,
  Copy,
  PaperPlaneTilt,
  TerminalWindow,
  WarningCircle,
} from '@phosphor-icons/react'
import { useI18n } from '@/hooks/useI18n'
import { useOverview } from '@/hooks/useOverview'
import { testChat } from '@/api/overview'
import { absUrl } from '@/lib/url'
import { AccessPageSkeleton } from '@/components/ui/PageSkeletons'
import { QoderMark } from '@/components/QoderMark'
import { isQoderGlobalProvider } from '@/lib/provider'

type RequestState = 'idle' | 'loading' | 'success' | 'error'

function shellQuote(value: string) {
  return `'${value.replaceAll("'", `'"'"'`)}'`
}

export function AccessPage() {
  const { t } = useI18n()
  const { overview, loading } = useOverview()
  const models = overview?.models || []
  const accounts = overview?.accounts || []
  const base = absUrl(overview?.access?.openai_base_url || '/v1')
  const [model, setModel] = useState('')
  const [accountId, setAccountId] = useState('')
  const [prompt, setPrompt] = useState('只回复OK')
  const [output, setOutput] = useState('')
  const [requestState, setRequestState] = useState<RequestState>('idle')
  const [elapsedMs, setElapsedMs] = useState<number | null>(null)
  const [copied, setCopied] = useState<'base' | 'curl' | ''>('')
  const selectedAccount = accountId || ''
  const selectedModel = models.some((item) => item.id === model) ? model : models[0]?.id || 'qwen3.7-plus'
  const readyAccounts = accounts.filter((account) => account.enabled !== false && (
    account.ready === true || account.hot === true || account.status === 'ready' || account.status === 'hot'
  ))

  const payload = useMemo(() => ({
    model: selectedModel,
    stream: false,
    messages: [{ role: 'user', content: prompt || '只回复OK' }],
  }), [prompt, selectedModel])

  const curl = useMemo(
    () => `curl -sS ${shellQuote(`${base}/chat/completions`)} \\
  -H "Authorization: Bearer $CLI2API_API_KEY" \\
  -H ${shellQuote('Content-Type: application/json')}${selectedAccount ? ` \\
  -H ${shellQuote(`X-Qoder-Account: ${selectedAccount}`)}` : ''} \\
  -d ${shellQuote(JSON.stringify(payload))}`,
    [base, payload, selectedAccount],
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
      <section className="grid gap-5 border-b border-[var(--app-line)] pb-6 lg:grid-cols-[minmax(0,1fr)_auto] lg:items-end">
        <div data-gsap-reveal>
          <h2 className="text-2xl font-semibold tracking-[-0.035em]">{t('apiPlayground')}</h2>
          <p className="mt-1 max-w-2xl text-sm leading-6 text-[var(--app-muted)]">{t('apiPlaygroundHint')}</p>
        </div>
        <div className="flex items-center gap-2 text-xs text-[var(--app-muted)]">
          <span className="status-dot" data-state={readyAccounts.length ? 'ok' : undefined} />
          <span>{t('readyAccounts', { ready: readyAccounts.length, total: accounts.length })}</span>
        </div>
      </section>

      <section data-gsap-reveal className="grid overflow-hidden rounded-lg border border-[var(--app-line)] bg-[var(--app-surface)] sm:grid-cols-3">
        <div className="min-w-0 border-b border-[var(--app-line)] p-4 sm:col-span-2 sm:border-r sm:border-b-0 sm:p-5">
          <div className="mb-2 flex items-center justify-between gap-3">
            <span className="text-xs font-medium text-[var(--app-faint)]">{t('baseUrl')}</span>
            <Button isIconOnly size="sm" variant="ghost" aria-label={t('copyBaseUrl')} onPress={() => void copy(base, 'base')}>
              {copied === 'base' ? <Check size={15} /> : <Copy size={15} />}
            </Button>
          </div>
          <code className="mono block truncate text-sm font-medium text-[var(--app-ink)]">{base}</code>
        </div>
        <div className="grid grid-cols-2 divide-x divide-[var(--app-line)] sm:grid-cols-1 sm:divide-x-0 sm:divide-y">
          <div className="p-4 sm:px-5 sm:py-3">
            <div className="text-[10px] font-semibold tracking-[0.08em] text-[var(--app-faint)] uppercase">{t('protocol')}</div>
            <div className="mt-1 text-sm font-medium">HTTP / SSE</div>
          </div>
          <div className="p-4 sm:px-5 sm:py-3">
            <div className="text-[10px] font-semibold tracking-[0.08em] text-[var(--app-faint)] uppercase">{t('authentication')}</div>
            <div className="mono mt-1 truncate text-xs font-medium">Bearer API key</div>
          </div>
        </div>
      </section>

      <Card data-gsap-reveal className="app-panel overflow-hidden rounded-lg p-0">
        <div className="grid xl:grid-cols-[minmax(440px,.92fr)_minmax(0,1.08fr)]">
          <div className="border-b border-[var(--app-line)] xl:border-r xl:border-b-0">
            <div className="border-b border-[var(--app-line)] px-5 py-5 sm:px-7">
              <div className="flex items-center gap-3">
                <div className="grid size-8 place-items-center rounded-lg bg-[var(--app-ink)] text-[var(--app-bg)]">
                  <PaperPlaneTilt size={15} weight="bold" />
                </div>
                <div>
                  <h3 className="font-semibold tracking-[-0.015em]">{t('requestBuilder')}</h3>
                  <p className="mt-0.5 text-xs text-[var(--app-faint)]">POST /chat/completions</p>
                </div>
              </div>
            </div>

            <div className="space-y-7 p-5 sm:p-7">
              <div className="space-y-3">
                  <div className="text-sm font-medium text-[var(--app-muted)]">{t('account')}</div>
                  <Select selectedKey={selectedAccount || 'auto'} onSelectionChange={(key) => setAccountId(String(key) === 'auto' ? '' : String(key))} aria-label={t('account')}>
                    <Select.Trigger>
                      <Select.Value>
                        {({ defaultChildren }) => {
                          const selected = accounts.find((account) => account.id === selectedAccount)
                          return (
                            <span className="inline-flex min-w-0 items-center gap-2">
                              {selected && isQoderGlobalProvider(selected.provider) ? <QoderMark size={16} /> : null}
                              <span className="truncate">{defaultChildren}</span>
                            </span>
                          )
                        }}
                      </Select.Value>
                      <Select.Indicator />
                    </Select.Trigger>
                    <Select.Popover>
                      <ListBox>
                        <ListBox.Item id="auto" textValue={t('autoAccount')}><Label>{t('autoAccount')}</Label></ListBox.Item>
                        {accounts.map((account) => (
                          <ListBox.Item key={account.id} id={account.id} textValue={account.name || account.id}>
                            <div className="flex min-w-0 items-center gap-2">
                              {isQoderGlobalProvider(account.provider) ? <QoderMark size={16} /> : null}
                              <Label>{account.name || account.id}</Label>
                            </div>
                          </ListBox.Item>
                        ))}
                      </ListBox>
                    </Select.Popover>
                  </Select>
                  <p className="text-xs leading-5 text-[var(--app-faint)]">{selectedAccount ? t('fixedAccountHint') : t('autoAccountHint')}</p>
              </div>

              <div className="space-y-3">
                  <div className="text-sm font-medium text-[var(--app-muted)]">{t('model')}</div>
                  <Select selectedKey={selectedModel} onSelectionChange={(key) => setModel(String(key))} aria-label={t('model')}>
                    <Select.Trigger><Select.Value /><Select.Indicator /></Select.Trigger>
                    <Select.Popover>
                      <ListBox>
                        {(models.length ? models : [{ id: 'qwen3.7-plus' }]).map((item) => (
                          <ListBox.Item key={item.id} id={item.id} textValue={item.display_name || item.id}>
                            <Label>{item.display_name || item.id} · {item.id}</Label>
                          </ListBox.Item>
                        ))}
                      </ListBox>
                    </Select.Popover>
                  </Select>
                  <p className="text-xs leading-5 text-[var(--app-faint)]">{t('modelRoutingHint')}</p>
              </div>

              <div className="space-y-3">
                <div className="flex items-center justify-between gap-4">
                  <div className="text-sm font-medium text-[var(--app-muted)]">{t('prompt')}</div>
                  <span className="mono text-[10px] text-[var(--app-faint)]">{prompt.length}</span>
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
                <p className="text-xs leading-5 text-[var(--app-faint)]">{t('promptHelper')}</p>
              </div>

              {!accounts.length ? (
                <div className="flex gap-3 border-t border-[var(--app-line)] pt-4 text-sm text-[var(--app-muted)]">
                  <WarningCircle className="mt-0.5 shrink-0 text-[var(--app-danger)]" size={16} />
                  <span>{t('noAccountsForTest')}</span>
                </div>
              ) : null}

              <Button fullWidth isPending={requestState === 'loading'} isDisabled={!prompt.trim() || !selectedModel} onPress={() => void onTest()}>
                <PaperPlaneTilt size={16} />
                {requestState === 'loading' ? t('requesting') : t('sendRequest')}
              </Button>
            </div>
          </div>

          <div className="flex min-h-[620px] min-w-0 flex-col bg-[var(--app-code)]/45" aria-live="polite" aria-busy={requestState === 'loading'}>
            <div className="flex min-h-16 items-center justify-between gap-4 border-b border-[var(--app-line)] px-5 py-4 sm:px-6">
              <div>
                <h3 className="font-semibold tracking-[-0.015em]">{t('responseInspector')}</h3>
                <p className="mt-0.5 text-xs text-[var(--app-faint)]">{t('responseInspectorHint')}</p>
              </div>
              <div className="flex items-center gap-2">
                {elapsedMs !== null ? (
                  <span className="mono flex items-center gap-1.5 text-[10px] text-[var(--app-faint)]">
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
                <div className="grid min-h-80 place-items-center">
                  <div className="max-w-xs text-center">
                    <div className="mx-auto grid size-12 place-items-center rounded-lg border border-[var(--app-line)] bg-[var(--app-surface)] text-[var(--app-faint)]">
                      <BracketsCurly size={21} />
                    </div>
                    <h4 className="mt-4 text-sm font-semibold">{t('responseEmptyTitle')}</h4>
                    <p className="mt-2 text-xs leading-5 text-[var(--app-faint)]">{t('responseEmptyHint')}</p>
                  </div>
                </div>
              ) : requestState === 'loading' ? (
                <div className="space-y-3 pt-1">
                  <Skeleton className="h-3 w-32 rounded" />
                  <Skeleton className="h-3 w-full rounded" />
                  <Skeleton className="h-3 w-[88%] rounded" />
                  <Skeleton className="h-3 w-[72%] rounded" />
                  <Skeleton className="mt-7 h-3 w-[92%] rounded" />
                  <Skeleton className="h-3 w-[64%] rounded" />
                </div>
              ) : requestState === 'error' ? (
                <div className="rounded-lg border border-[color-mix(in_srgb,var(--app-danger)_24%,transparent)] bg-[color-mix(in_srgb,var(--app-danger)_7%,transparent)] p-4">
                  <div className="flex items-center gap-2 text-sm font-semibold text-[var(--app-danger)]">
                    <WarningCircle size={17} />
                    {t('requestFailed')}
                  </div>
                  <pre className="mono mt-3 whitespace-pre-wrap break-words text-xs leading-6 text-[var(--app-muted)]">{output}</pre>
                </div>
              ) : (
                <div>
                  <div className="mb-4 flex items-center gap-2 text-xs font-medium text-[var(--app-ok)]">
                    <CheckCircle size={15} weight="fill" />
                    {t('responseReceived')}
                  </div>
                  <pre className="mono overflow-x-auto whitespace-pre-wrap break-words text-xs leading-6 text-[var(--app-muted)]">{output}</pre>
                </div>
              )}
            </div>
          </div>
        </div>
      </Card>

      <section data-gsap-reveal className="app-panel-flat overflow-hidden rounded-lg">
        <div className="flex items-center justify-between gap-4 border-b border-[var(--app-line)] px-5 py-3.5">
          <div className="flex min-w-0 items-center gap-3">
            <TerminalWindow className="shrink-0 text-[var(--app-faint)]" size={16} />
            <div className="min-w-0">
              <div className="text-sm font-semibold">{t('curlExample')}</div>
              <div className="mt-0.5 truncate text-xs text-[var(--app-faint)]">{t('curlGeneratedHint')}</div>
            </div>
          </div>
          <Button size="sm" variant="ghost" onPress={() => void copy(curl, 'curl')}>
            {copied === 'curl' ? <Check size={14} /> : <Copy size={14} />}
            {copied === 'curl' ? t('copied') : t('copy')}
          </Button>
        </div>
        <pre className="mono max-h-80 overflow-auto whitespace-pre-wrap p-5 text-xs leading-6 text-[var(--app-muted)]">{curl}</pre>
      </section>
    </div>
  )
}
