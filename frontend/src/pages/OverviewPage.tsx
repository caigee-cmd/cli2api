import { Chip, Card, Skeleton } from '@heroui/react'
import { useI18n } from '@/hooks/useI18n'
import { useOverview } from '@/hooks/useOverview'
import { absUrl } from '@/lib/url'

export function OverviewPage() {
  const { t } = useI18n()
  const { overview, loading, error } = useOverview()
  const proxyOk = !!overview?.proxy?.ok
  const workerOk = !!overview?.worker?.ok
  const hot = !!overview?.worker?.hot
  const authOk = !!overview?.auth?.has_user_blob || !!overview?.auth?.has_pat
  const base = absUrl(overview?.access?.openai_base_url || '/v1')

  const metrics = [
    { label: t('metricRuntime'), value: proxyOk ? t('running') : t('down') },
    { label: t('metricWorker'), value: workerOk ? (hot ? t('hot') : t('up')) : t('down') },
    { label: t('metricAuth'), value: authOk ? t('ready') : t('missing') },
    { label: t('metricPort'), value: String(overview?.proxy?.port || location.port || '3010') },
  ]

  const endpoints = [
    { name: t('endpointOpenAI'), url: base },
    { name: t('endpointChat'), url: absUrl(overview?.access?.chat_completions || `${base}/chat/completions`) },
    { name: t('endpointModels'), url: absUrl(overview?.access?.models || `${base}/models`) },
    { name: t('endpointHealth'), url: absUrl(overview?.access?.health || '/health') },
  ]

  return (
    <div className="space-y-6">
      <section className="grid gap-4 lg:grid-cols-[1.4fr_.8fr] lg:items-end">
        <div>
          <p className="mb-1 text-xs font-semibold uppercase tracking-[0.08em] text-emerald-400">{t('homeEyebrow')}</p>
          <h2 className="text-3xl font-semibold tracking-tight sm:text-4xl">{t('homeDisplay')}</h2>
          <p className="mt-2 max-w-xl text-sm text-zinc-400">{t('homeLead')}</p>
        </div>
        <div className="space-y-2 text-left lg:text-right">
          <div className="text-xs text-zinc-500">{overview?.time || '—'}</div>
          <div className="text-xs text-zinc-500">{t('homeNote')}</div>
        </div>
      </section>

      <section className="grid grid-cols-2 overflow-hidden rounded-2xl border border-white/10 bg-white/[0.02] md:grid-cols-4">
        {metrics.map((m, idx) => (
          <div
            key={m.label}
            className={`px-4 py-4 ${idx % 2 === 0 ? 'border-r border-white/10' : ''} ${idx < 2 ? 'border-b border-white/10 md:border-b-0' : ''} ${idx < 3 ? 'md:border-r' : ''}`}
          >
            <div className="text-xs text-zinc-500">{m.label}</div>
            <div className="mt-2 text-2xl font-semibold tracking-tight">{loading ? '…' : m.value}</div>
          </div>
        ))}
      </section>

      <section className="grid gap-4 lg:grid-cols-[1.1fr_.9fr]">
        <Card className="border border-white/10 bg-white/[0.02] p-4 shadow-none">
          <div className="mb-3 text-sm font-medium">{t('serviceDetail')}</div>
          {loading ? (
            <div className="space-y-2">
              <Skeleton className="h-4 w-full rounded-lg" />
              <Skeleton className="h-4 w-4/5 rounded-lg" />
              <Skeleton className="h-4 w-3/5 rounded-lg" />
            </div>
          ) : error ? (
            <div className="text-sm text-red-400">{t('failedOverview', { msg: error })}</div>
          ) : (
            <dl className="space-y-3 text-sm">
              {[
                [t('proxy'), proxyOk ? 'ok' : t('down')],
                [t('workerHot'), hot ? 'true' : 'false'],
                [t('endpoint'), overview?.worker?.endpoint || overview?.proxy?.chat_url || '—'],
                [t('rewarm'), String(overview?.worker?.rewarmCount ?? overview?.worker?.rewarm_count ?? 0)],
                [t('lastError'), overview?.worker?.lastError || overview?.worker?.last_error || '—'],
              ].map(([k, v]) => (
                <div key={String(k)} className="flex items-start justify-between gap-4 border-b border-white/5 pb-2 last:border-0">
                  <dt className="text-zinc-500">{k}</dt>
                  <dd className="max-w-[65%] break-all text-right font-medium">{v}</dd>
                </div>
              ))}
            </dl>
          )}
        </Card>

        <Card className="border border-white/10 bg-white/[0.02] p-4 shadow-none">
          <div className="mb-3 text-sm font-medium">{t('endpoints')}</div>
          <div className="space-y-3">
            {endpoints.map((item) => (
              <div key={item.name} className="rounded-xl border border-white/8 bg-black/20 p-3">
                <div className="mb-1 flex items-center justify-between gap-2">
                  <div className="text-sm">{item.name}</div>
                  <Chip size="sm" variant="soft">{item.name === t('endpointOpenAI') ? 'OpenAI' : 'HTTP'}</Chip>
                </div>
                <code className="mono block break-all text-xs text-zinc-300">{item.url}</code>
              </div>
            ))}
          </div>
        </Card>
      </section>
    </div>
  )
}
