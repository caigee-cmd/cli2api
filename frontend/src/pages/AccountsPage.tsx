import { Card, Chip } from '@heroui/react'
import { useI18n } from '@/hooks/useI18n'
import { useOverview } from '@/hooks/useOverview'

export function AccountsPage() {
  const { t } = useI18n()
  const { overview, loading } = useOverview()
  const accounts = overview?.accounts || []

  return (
    <div className="space-y-4">
      <div>
        <p className="mb-1 text-xs font-semibold uppercase tracking-[0.08em] text-emerald-400">{t('catalog')}</p>
        <h2 className="text-2xl font-semibold tracking-tight sm:text-3xl">{t('accountsTitle')}</h2>
        <p className="mt-1 text-sm text-zinc-400">{t('accountsDesc')}</p>
      </div>
      <Card className="border border-white/10 bg-white/[0.02] p-4 shadow-none">
        {loading ? (
          <div className="text-sm text-zinc-400">{t('waitingOverview')}</div>
        ) : accounts.length === 0 ? (
          <div className="text-sm text-zinc-400">{t('noAccounts')}</div>
        ) : (
          <div className="space-y-3">
            {accounts.map((acc) => (
              <div key={acc.id} className="rounded-xl border border-white/10 bg-black/20 p-4">
                <div className="mb-2 flex items-center justify-between gap-3">
                  <div className="font-medium">{acc.id}</div>
                  <Chip size="sm" variant="soft" color={acc.ready ? 'success' : 'warning'}>
                    {acc.ready ? t('ready') : t('degraded')}
                  </Chip>
                </div>
                <code className="mono block break-all text-xs text-zinc-400">{acc.url || '—'}</code>
                {acc.last_error ? (
                  <div className="mt-2 text-xs text-red-400">{acc.last_error}</div>
                ) : null}
              </div>
            ))}
          </div>
        )}
      </Card>
    </div>
  )
}
