import { Button, Card, Chip, Skeleton } from '@heroui/react'
import { ArrowUpRight, CircleGauge, Copy, Database, ExternalLink, Server, Users } from 'lucide-react'
import { useI18n } from '@/hooks/useI18n'
import { useOverview } from '@/hooks/useOverview'
import { absUrl } from '@/lib/url'

export function OverviewPage() {
  const { t } = useI18n()
  const { overview, loading, error } = useOverview()
  const proxyOk = Boolean(overview?.proxy?.ok)
  const workerOk = Boolean(overview?.worker?.ok)
  const hot = Boolean(overview?.worker?.hot)
  const accounts = overview?.accounts || []
  const readyAccounts = accounts.filter((account) => account.ready).length
  const hotAccounts = accounts.filter((account) => account.hot).length
  const base = absUrl(overview?.access?.openai_base_url || '/v1')

  const metrics = [
    { label: t('metricRuntime'), value: proxyOk ? t('running') : t('down'), detail: 'Go control plane', icon: Server, ok: proxyOk },
    { label: t('metricWorker'), value: workerOk ? (hot ? t('hot') : t('up')) : t('down'), detail: `${hotAccounts}/${accounts.length} hot`, icon: CircleGauge, ok: workerOk },
    { label: t('accountsTitle'), value: String(accounts.length), detail: `${readyAccounts} ready`, icon: Users, ok: readyAccounts > 0 },
    { label: 'SQLite', value: accounts.length ? t('ready') : t('checking'), detail: '/data/qoder.db', icon: Database, ok: accounts.length > 0 },
  ]

  const endpoints = [
    { name: t('endpointOpenAI'), url: base, method: 'BASE' },
    { name: t('endpointChat'), url: absUrl(overview?.access?.chat_completions || `${base}/chat/completions`), method: 'POST' },
    { name: t('endpointModels'), url: absUrl(overview?.access?.models || `${base}/models`), method: 'GET' },
    { name: t('endpointHealth'), url: absUrl(overview?.access?.health || '/health'), method: 'GET' },
  ]

  return (
    <div className="space-y-7">
      <section className="grid gap-6 border-b border-[var(--app-line)] pb-7 xl:grid-cols-[minmax(0,1fr)_360px] xl:items-end">
        <div>
          <h2 className="text-2xl font-semibold tracking-[-0.03em] sm:text-3xl">{t('homeDisplay')}</h2>
          <p className="mt-2 max-w-2xl text-sm leading-6 text-[var(--app-muted)]">{t('homeLead')}</p>
        </div>
        <div className="xl:text-right">
          <div className="mono text-xs text-[var(--app-faint)]">{overview?.time || '—'}</div>
          <div className="mt-2 inline-flex items-center gap-2 text-sm font-medium">
            <span className="status-dot" data-state={proxyOk && workerOk ? 'ok' : 'danger'} />
            {proxyOk && workerOk ? t('running') : t('degraded')}
          </div>
        </div>
      </section>

      <section className="grid overflow-hidden rounded-xl border border-[var(--app-line)] bg-[var(--app-surface)] sm:grid-cols-2 xl:grid-cols-4">
        {metrics.map((metric, index) => {
          const Icon = metric.icon
          return (
            <div key={metric.label} className={`min-h-36 p-5 ${index ? 'border-t border-[var(--app-line)] sm:border-l sm:border-t-0' : ''} ${index === 2 ? 'sm:border-l-0 sm:border-t xl:border-l xl:border-t-0' : ''}`}>
              <div className="flex items-start justify-between gap-3">
                <span className="text-xs font-medium text-[var(--app-muted)]">{metric.label}</span>
                <Icon size={16} className="text-[var(--app-faint)]" />
              </div>
              <div className="mt-7 flex items-end justify-between gap-3">
                <div>
                  <div className="text-2xl font-semibold tracking-[-0.035em]">{loading ? '…' : metric.value}</div>
                  <div className="mono mt-1 text-[11px] text-[var(--app-faint)]">{metric.detail}</div>
                </div>
                <span className="status-dot" data-state={metric.ok ? 'ok' : 'danger'} />
              </div>
            </div>
          )
        })}
      </section>

      <section className="grid gap-5 xl:grid-cols-[minmax(0,1.15fr)_minmax(380px,.85fr)]">
        <Card className="app-panel-flat overflow-hidden rounded-xl p-0 shadow-none">
          <div className="flex items-center justify-between gap-3 border-b border-[var(--app-line)] px-5 py-4">
            <div>
              <h3 className="font-semibold tracking-[-0.015em]">{t('serviceDetail')}</h3>
              <p className="mt-0.5 text-xs text-[var(--app-faint)]">{t('runtimeSnapshot')}</p>
            </div>
            <Chip size="sm" variant="soft" color={workerOk ? 'success' : 'warning'}>{workerOk ? t('ready') : t('degraded')}</Chip>
          </div>
          {loading ? (
            <div className="space-y-3 p-5">
              <Skeleton className="h-12 w-full rounded-lg" />
              <Skeleton className="h-12 w-full rounded-lg" />
              <Skeleton className="h-12 w-4/5 rounded-lg" />
            </div>
          ) : error ? (
            <div className="p-5 text-sm text-[var(--app-danger)]">{t('failedOverview', { msg: error })}</div>
          ) : (
            <dl className="divide-y divide-[var(--app-line)]">
              {[
                [t('proxy'), proxyOk ? 'online' : t('down')],
                [t('workerHot'), hot ? 'true' : 'false'],
                [t('endpoint'), overview?.worker?.endpoint || overview?.proxy?.chat_url || '—'],
                [t('rewarm'), String(overview?.worker?.rewarmCount ?? overview?.worker?.rewarm_count ?? 0)],
                [t('lastError'), overview?.worker?.lastError || overview?.worker?.last_error || '—'],
              ].map(([label, value]) => (
                <div key={String(label)} className="grid gap-2 px-5 py-3.5 sm:grid-cols-[160px_minmax(0,1fr)]">
                  <dt className="text-xs text-[var(--app-faint)]">{label}</dt>
                  <dd className="break-all text-sm font-medium sm:text-right">{value}</dd>
                </div>
              ))}
            </dl>
          )}
        </Card>

        <Card className="app-panel-flat overflow-hidden rounded-xl p-0 shadow-none">
          <div className="border-b border-[var(--app-line)] px-5 py-4">
            <h3 className="font-semibold tracking-[-0.015em]">{t('endpoints')}</h3>
            <p className="mt-0.5 text-xs text-[var(--app-faint)]">{t('routesHint')}</p>
          </div>
          <div className="divide-y divide-[var(--app-line)]">
            {endpoints.map((item) => (
              <div key={item.name} className="group grid gap-2 px-5 py-4 sm:grid-cols-[70px_minmax(0,1fr)_auto] sm:items-center">
                <span className="mono text-[10px] font-semibold text-[var(--accent)]">{item.method}</span>
                <div className="min-w-0">
                  <div className="text-sm font-medium">{item.name}</div>
                  <code className="mono mt-1 block truncate text-[11px] text-[var(--app-faint)]">{item.url}</code>
                </div>
                <div className="flex gap-1 opacity-100 sm:opacity-0 sm:group-hover:opacity-100">
                  <Button isIconOnly size="sm" variant="ghost" aria-label={t('copy')} onPress={() => void navigator.clipboard.writeText(item.url)}><Copy size={14} /></Button>
                  <Button isIconOnly size="sm" variant="ghost" aria-label={t('open')} onPress={() => window.open(item.url, '_blank', 'noopener,noreferrer')}><ExternalLink size={14} /></Button>
                </div>
              </div>
            ))}
          </div>
          <div className="flex items-center justify-between border-t border-[var(--app-line)] px-5 py-3 text-xs text-[var(--app-faint)]">
            <span>Authorization: Bearer</span>
            <ArrowUpRight size={14} />
          </div>
        </Card>
      </section>
    </div>
  )
}
