import { useCallback, useEffect, useMemo, useState } from 'react'
import {
  Button,
  ButtonGroup,
  Card,
  Chip,
  Input,
  Modal,
  Tab,
  Table,
  Tabs,
} from '@heroui/react'
import {
  ArrowClockwise,
  MagnifyingGlass,
  Scroll,
  TerminalWindow,
  TrashSimple,
  WarningCircle,
  X,
} from '@phosphor-icons/react'
import {
  clearRequestLogs,
  fetchRequestLog,
  fetchRequestLogs,
  fetchRuntimeLogs,
  type RequestLog,
  type RuntimeLogEntry,
} from '@/api/logs'
import { LogsPageSkeleton } from '@/components/ui/PageSkeletons'
import { useI18n } from '@/hooks/useI18n'

type PageTab = 'requests' | 'runtime'
type RequestFilter = 'all' | 'ok' | 'error' | 'canceled'
type RuntimeFilter = 'all' | 'info' | 'warn' | 'error'

function statusColor(status?: string): 'success' | 'warning' | 'danger' | 'default' {
  if (status === 'ok') return 'success'
  if (status === 'streaming' || status === 'started') return 'warning'
  if (status === 'error' || status === 'canceled') return 'danger'
  return 'default'
}

function levelDot(level?: string) {
  if (level === 'error') return 'danger'
  if (level === 'warn') return undefined
  return 'ok'
}

function formatTime(value?: string | null, lang: 'en' | 'zh' = 'zh') {
  if (!value) return '—'
  const date = new Date(value)
  if (!Number.isFinite(date.getTime())) return value
  return new Intl.DateTimeFormat(lang === 'zh' ? 'zh-CN' : 'en-US', {
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
    hour12: false,
  }).format(date)
}

function formatTokens(log: RequestLog) {
  const prompt = log.prompt_tokens
  const completion = log.completion_tokens
  if (prompt == null && completion == null) return '—'
  return `${prompt ?? 0} / ${completion ?? 0}`
}

function formatLatency(ms?: number | null) {
  if (ms == null) return '—'
  if (ms < 1000) return `${ms}ms`
  return `${(ms / 1000).toFixed(1)}s`
}

export function LogsPage() {
  const { t, lang } = useI18n()
  const [tab, setTab] = useState<PageTab>('requests')
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [requestFilter, setRequestFilter] = useState<RequestFilter>('all')
  const [runtimeFilter, setRuntimeFilter] = useState<RuntimeFilter>('all')
  const [requestQuery, setRequestQuery] = useState('')
  const [runtimeQuery, setRuntimeQuery] = useState('')
  const [requests, setRequests] = useState<RequestLog[]>([])
  const [total, setTotal] = useState(0)
  const [runtime, setRuntime] = useState<RuntimeLogEntry[]>([])
  const [selected, setSelected] = useState<RequestLog | null>(null)
  const [detailOpen, setDetailOpen] = useState(false)
  const [clearOpen, setClearOpen] = useState(false)
  const [busy, setBusy] = useState(false)

  const loadRequests = useCallback(async (quiet = false) => {
    if (!quiet) setLoading(true)
    try {
      const result = await fetchRequestLogs({
        status: requestFilter === 'all' ? undefined : requestFilter,
        q: requestQuery.trim() || undefined,
        limit: 50,
      })
      setRequests(result.items || [])
      setTotal(result.total || 0)
      setError('')
    } catch (err) {
      if (!quiet) setError(err instanceof Error ? err.message : String(err))
    } finally {
      if (!quiet) setLoading(false)
    }
  }, [requestFilter, requestQuery])

  const loadRuntime = useCallback(async (quiet = false) => {
    if (!quiet) setLoading(true)
    try {
      const result = await fetchRuntimeLogs({
        level: runtimeFilter === 'all' ? undefined : runtimeFilter,
        q: runtimeQuery.trim() || undefined,
        limit: 200,
      })
      setRuntime(result.items || [])
      setError('')
    } catch (err) {
      if (!quiet) setError(err instanceof Error ? err.message : String(err))
    } finally {
      if (!quiet) setLoading(false)
    }
  }, [runtimeFilter, runtimeQuery])

  const load = useCallback(async (quiet = false) => {
    if (tab === 'requests') await loadRequests(quiet)
    else await loadRuntime(quiet)
  }, [tab, loadRequests, loadRuntime])

  useEffect(() => {
    const delay = tab === 'requests' && requestQuery.trim() ? 280 : tab === 'runtime' && runtimeQuery.trim() ? 280 : 0
    const timer = window.setTimeout(() => void load(false), delay)
    return () => window.clearTimeout(timer)
  }, [load, tab, requestQuery, runtimeQuery])

  useEffect(() => {
    if (tab !== 'runtime') return
    const timer = window.setInterval(() => void loadRuntime(true), 3000)
    return () => window.clearInterval(timer)
  }, [tab, loadRuntime])

  const shownLabel = useMemo(() => {
    const shown = tab === 'requests' ? requests.length : runtime.length
    const count = tab === 'requests' ? total : runtime.length
    return t('logsShownTotal', { shown, total: count })
  }, [tab, requests.length, runtime.length, total, t])

  async function openDetail(id: string) {
    setBusy(true)
    try {
      const detail = await fetchRequestLog(id)
      setSelected(detail)
      setDetailOpen(true)
      setError('')
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  async function onClear() {
    setBusy(true)
    try {
      await clearRequestLogs()
      setClearOpen(false)
      await loadRequests(false)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  if (loading && requests.length === 0 && runtime.length === 0) {
    return <LogsPageSkeleton />
  }

  return (
    <div className="space-y-6">
      <section className="flex flex-wrap items-end justify-between gap-4 border-b border-[var(--app-line)] pb-4">
        <div>
          <h2 data-gsap-reveal className="text-2xl font-semibold tracking-[-0.035em]">{t('navLogs')}</h2>
          <p className="mt-1 max-w-2xl text-sm leading-6 text-[var(--app-muted)]">{t('logsLead')}</p>
        </div>
        <div className="flex items-center gap-2">
          {tab === 'requests' ? (
            <Button size="sm" variant="ghost" onPress={() => setClearOpen(true)} isDisabled={busy || total === 0}>
              <TrashSimple size={14} />{t('logsClear')}
            </Button>
          ) : null}
          <Button size="sm" variant="secondary" isPending={busy} onPress={() => void load(false)}>
            <ArrowClockwise size={15} />{t('refresh')}
          </Button>
        </div>
      </section>

      {error ? (
        <div className="flex items-start gap-2 rounded-lg border border-[color-mix(in_srgb,var(--app-danger)_28%,var(--app-line))] bg-[color-mix(in_srgb,var(--app-danger)_7%,var(--app-surface))] px-4 py-3 text-sm text-[var(--app-danger)]">
          <WarningCircle className="mt-0.5 shrink-0" size={17} />
          {t('failedLogs', { msg: error })}
        </div>
      ) : null}

      <Tabs.Root selectedKey={tab} onSelectionChange={(key) => setTab(String(key) as PageTab)}>
        <Tabs.List className="grid max-w-md grid-cols-2 gap-1 rounded-lg bg-[var(--app-surface-muted)] p-1">
          <Tab id="requests" className="flex items-center justify-center gap-1.5 rounded-md px-3 py-1.5 text-xs font-medium text-[var(--app-faint)] data-[hovered=true]:text-[var(--app-fg)] data-[selected=true]:bg-[var(--app-surface)] data-[selected=true]:text-[var(--app-ink)] data-[selected=true]:shadow-sm">
            <Scroll size={13} />{t('logsRequests')}
          </Tab>
          <Tab id="runtime" className="flex items-center justify-center gap-1.5 rounded-md px-3 py-1.5 text-xs font-medium text-[var(--app-faint)] data-[hovered=true]:text-[var(--app-fg)] data-[selected=true]:bg-[var(--app-surface)] data-[selected=true]:text-[var(--app-ink)] data-[selected=true]:shadow-sm">
            <TerminalWindow size={13} />{t('logsRuntime')}
          </Tab>
        </Tabs.List>

        <Tabs.Panel id="requests" className="space-y-4 pt-5">
          <div className="flex flex-wrap items-center justify-between gap-3">
            <div className="flex flex-wrap items-center gap-3">
              <Input
                className="sm:w-72"
                value={requestQuery}
                onChange={(event) => setRequestQuery(event.target.value)}
                placeholder={t('logsSearchRequests')}
                aria-label={t('logsSearchRequests')}
              />
              <ButtonGroup className="toolbar-group">
                {([
                  ['all', t('logsFilterAll')],
                  ['ok', t('logsFilterOk')],
                  ['error', t('logsFilterError')],
                  ['canceled', t('logsFilterCanceled')],
                ] as Array<[RequestFilter, string]>).map(([value, label]) => (
                  <Button
                    key={value}
                    size="sm"
                    variant={requestFilter === value ? 'secondary' : 'ghost'}
                    onPress={() => setRequestFilter(value)}
                  >
                    {label}
                  </Button>
                ))}
              </ButtonGroup>
            </div>
            <div className="mono text-[11px] text-[var(--app-faint)]">{shownLabel}</div>
          </div>

          <Card data-gsap-reveal className="app-panel-flat overflow-hidden rounded-lg p-0 shadow-none">
            {requests.length === 0 ? (
              <div className="grid min-h-72 place-items-center px-6 py-12 text-center">
                <div>
                  <MagnifyingGlass size={22} className="mx-auto text-[var(--app-faint)]" />
                  <div className="mt-4 text-sm font-medium">{t('logsEmptyRequests')}</div>
                </div>
              </div>
            ) : (
              <Table>
                <Table.ScrollContainer>
                  <Table.Content aria-label={t('logsRequests')}>
                    <Table.Header>
                      <Table.Column isRowHeader>{t('logsColTime')}</Table.Column>
                      <Table.Column>{t('logsColModel')}</Table.Column>
                      <Table.Column>{t('logsColAccount')}</Table.Column>
                      <Table.Column>{t('logsColStatus')}</Table.Column>
                      <Table.Column>{t('logsColStream')}</Table.Column>
                      <Table.Column>{t('logsColLatency')}</Table.Column>
                      <Table.Column>{t('logsColTokens')}</Table.Column>
                    </Table.Header>
                    <Table.Body>
                      {requests.map((item) => (
                        <Table.Row key={item.id} className="cursor-pointer" onClick={() => void openDetail(item.id)}>
                          <Table.Cell>
                            <div className="py-1">
                              <div className="mono text-xs">{formatTime(item.created_at, lang)}</div>
                              <div className="mono mt-0.5 text-[10px] text-[var(--app-faint)]">{item.id}</div>
                            </div>
                          </Table.Cell>
                          <Table.Cell>
                            <div className="text-sm font-medium">{item.requested_model || '—'}</div>
                            {item.mapped_model && item.mapped_model !== item.requested_model ? (
                              <div className="mono mt-0.5 text-[10px] text-[var(--app-faint)]">{item.mapped_model}</div>
                            ) : null}
                          </Table.Cell>
                          <Table.Cell><span className="mono text-xs">{item.account_id || '—'}</span></Table.Cell>
                          <Table.Cell>
                            <Chip size="sm" variant="soft" color={statusColor(item.status)}>{item.status}</Chip>
                            {item.error_kind ? <div className="mono mt-1 text-[10px] text-[var(--app-faint)]">{item.error_kind}</div> : null}
                          </Table.Cell>
                          <Table.Cell><span className="text-xs text-[var(--app-muted)]">{item.stream ? t('logsStreamYes') : t('logsStreamNo')}</span></Table.Cell>
                          <Table.Cell><span className="mono text-xs">{formatLatency(item.latency_ms)}</span></Table.Cell>
                          <Table.Cell><span className="mono text-xs">{formatTokens(item)}</span></Table.Cell>
                        </Table.Row>
                      ))}
                    </Table.Body>
                  </Table.Content>
                </Table.ScrollContainer>
              </Table>
            )}
          </Card>
        </Tabs.Panel>

        <Tabs.Panel id="runtime" className="space-y-4 pt-5">
          <div className="flex flex-wrap items-center justify-between gap-3">
            <div className="flex flex-wrap items-center gap-3">
              <Input
                className="sm:w-72"
                value={runtimeQuery}
                onChange={(event) => setRuntimeQuery(event.target.value)}
                placeholder={t('logsSearchRuntime')}
                aria-label={t('logsSearchRuntime')}
              />
              <ButtonGroup className="toolbar-group">
                {([
                  ['all', t('logsLevelAll')],
                  ['info', t('logsLevelInfo')],
                  ['warn', t('logsLevelWarn')],
                  ['error', t('logsLevelError')],
                ] as Array<[RuntimeFilter, string]>).map(([value, label]) => (
                  <Button
                    key={value}
                    size="sm"
                    variant={runtimeFilter === value ? 'secondary' : 'ghost'}
                    onPress={() => setRuntimeFilter(value)}
                  >
                    {label}
                  </Button>
                ))}
              </ButtonGroup>
            </div>
            <div className="flex items-center gap-2 text-xs text-[var(--app-faint)]">
              <span className="status-dot" data-state="ok" />
              {t('logsAutoRefresh')}
              <span className="mono">{shownLabel}</span>
            </div>
          </div>

          <Card data-gsap-reveal className="app-panel-flat overflow-hidden rounded-lg p-0 shadow-none">
            {runtime.length === 0 ? (
              <div className="grid min-h-72 place-items-center px-6 py-12 text-center">
                <div>
                  <TerminalWindow size={22} className="mx-auto text-[var(--app-faint)]" />
                  <div className="mt-4 text-sm font-medium">{t('logsEmptyRuntime')}</div>
                </div>
              </div>
            ) : (
              <div className="divide-y divide-[var(--app-line)]">
                {runtime.map((entry) => (
                  <div key={entry.id} className="grid gap-2 px-5 py-3 sm:grid-cols-[150px_72px_minmax(0,1fr)] sm:items-start">
                    <div className="mono text-[11px] text-[var(--app-faint)]">{formatTime(entry.time, lang)}</div>
                    <div className="flex items-center gap-2 text-xs">
                      <span className="status-dot" data-state={levelDot(entry.level)} />
                      <span className="font-medium uppercase tracking-[0.04em] text-[var(--app-muted)]">{entry.level}</span>
                    </div>
                    <div className="min-w-0">
                      {entry.account_id ? <div className="mono mb-1 text-[10px] text-[var(--app-faint)]">{entry.account_id}</div> : null}
                      <div className="mono break-all text-xs leading-5 text-[var(--app-ink)]">{entry.message}</div>
                    </div>
                  </div>
                ))}
              </div>
            )}
          </Card>
        </Tabs.Panel>
      </Tabs.Root>

      <Modal isOpen={detailOpen} onOpenChange={setDetailOpen}>
        <Modal.Backdrop>
          <Modal.Container>
            <Modal.Dialog className="max-w-2xl bg-[var(--app-surface)] text-[var(--app-ink)]">
              <div className="flex items-start justify-between gap-3 border-b border-[var(--app-line)] px-5 py-4">
                <div>
                  <h3 className="font-semibold">{t('logsDetailTitle')}</h3>
                  <p className="mono mt-1 text-[11px] text-[var(--app-faint)]">{selected?.id}</p>
                </div>
                <Button isIconOnly size="sm" variant="ghost" onPress={() => setDetailOpen(false)} aria-label={t('close')}>
                  <X size={14} />
                </Button>
              </div>
              <div className="space-y-4 px-5 py-4">
                <dl className="grid gap-3 sm:grid-cols-2">
                  {[
                    [t('logsColStatus'), selected?.status || '—'],
                    [t('logsColModel'), selected?.requested_model || '—'],
                    [t('logsColAccount'), selected?.account_id || '—'],
                    [t('logsColLatency'), formatLatency(selected?.latency_ms)],
                    [t('logsColTokens'), selected ? formatTokens(selected) : '—'],
                    [t('logsColStream'), selected?.stream ? t('logsStreamYes') : t('logsStreamNo')],
                  ].map(([label, value]) => (
                    <div key={String(label)}>
                      <dt className="text-[11px] text-[var(--app-faint)]">{label}</dt>
                      <dd className="mt-1 break-all text-sm font-medium">{value}</dd>
                    </div>
                  ))}
                </dl>
                {selected?.error_message ? (
                  <div className="rounded-lg border border-[var(--app-line)] bg-[var(--app-surface-muted)] px-3 py-2 text-xs leading-5 text-[var(--app-muted)]">
                    {selected.error_kind ? <span className="mono mr-2 text-[var(--app-faint)]">{selected.error_kind}</span> : null}
                    {selected.error_message}
                  </div>
                ) : null}
                <div>
                  <div className="text-xs font-medium text-[var(--app-muted)]">{t('logsAttempts')}</div>
                  {selected?.attempts?.length ? (
                    <div className="mt-2 divide-y divide-[var(--app-line)] rounded-lg border border-[var(--app-line)]">
                      {selected.attempts.map((attempt) => (
                        <div key={attempt.id} className="grid gap-1 px-3 py-2.5 text-xs sm:grid-cols-[48px_minmax(0,1fr)_auto]">
                          <div className="mono text-[var(--app-faint)]">#{attempt.attempt_index}</div>
                          <div>
                            <div className="font-medium">{attempt.account_id || '—'}</div>
                            <div className="mt-0.5 text-[var(--app-faint)]">{attempt.error_message || attempt.status}</div>
                          </div>
                          <Chip size="sm" variant="soft" color={statusColor(attempt.status === 'failover' ? 'canceled' : attempt.status)}>
                            {attempt.status}
                          </Chip>
                        </div>
                      ))}
                    </div>
                  ) : (
                    <p className="mt-2 text-xs text-[var(--app-faint)]">{t('logsNoAttempts')}</p>
                  )}
                </div>
              </div>
            </Modal.Dialog>
          </Modal.Container>
        </Modal.Backdrop>
      </Modal>

      <Modal isOpen={clearOpen} onOpenChange={setClearOpen}>
        <Modal.Backdrop>
          <Modal.Container>
            <Modal.Dialog className="max-w-md bg-[var(--app-surface)] text-[var(--app-ink)]">
              <div className="px-5 py-4">
                <h3 className="font-semibold">{t('logsClear')}</h3>
                <p className="mt-2 text-sm leading-6 text-[var(--app-muted)]">{t('logsClearConfirm')}</p>
                <div className="mt-5 flex justify-end gap-2">
                  <Button size="sm" variant="ghost" onPress={() => setClearOpen(false)}>{t('close')}</Button>
                  <Button size="sm" variant="danger" isPending={busy} onPress={() => void onClear()}>
                    <TrashSimple size={14} />{t('logsClear')}
                  </Button>
                </div>
              </div>
            </Modal.Dialog>
          </Modal.Container>
        </Modal.Backdrop>
      </Modal>
    </div>
  )
}
