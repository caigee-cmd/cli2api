import { useMemo, useState } from 'react'
import { Button, Input } from '@heroui/react'
import { useI18n } from '@/hooks/useI18n'
import { useOverview } from '@/hooks/useOverview'
import { fetchLoginStatus, loginWithPat, rewarmWorker, startDeviceLogin } from '@/api/overview'
import type { Overview } from '@/api/types'

type AccountRow = NonNullable<Overview['accounts']>[number]
type AccountBusy = { id: string; kind: 'device' | 'pat' | 'rewarm' }

function cooldownLabel(until?: string | null) {
  if (!until) return ''
  const ms = Date.parse(until) - Date.now()
  if (!Number.isFinite(ms) || ms <= 0) return ''
  const sec = Math.ceil(ms / 1000)
  if (sec < 60) return `${sec}s`
  return `${Math.ceil(sec / 60)}m`
}

function statusLabel(acc: AccountRow, t: (key: string) => string) {
  if (cooldownLabel(acc.down_until)) return t('cooling')
  if (acc.hot) return t('signedIn')
  if (acc.ready) return t('ready')
  return t('needQoderLogin')
}

export function AccountsPage() {
  const { t } = useI18n()
  const { overview, loading, refresh } = useOverview()
  const accounts = overview?.accounts || []
  const rows: AccountRow[] = accounts.length
    ? accounts
    : [{ id: 'default', ready: !!overview?.worker?.ok, hot: !!overview?.worker?.hot }]
  const [busy, setBusy] = useState<AccountBusy | null>(null)
  const [patById, setPatById] = useState<Record<string, string>>({})
  const [noteById, setNoteById] = useState<Record<string, string>>({})
  const [urlById, setUrlById] = useState<Record<string, string>>({})
  const [copied, setCopied] = useState('')

  const workerLogin = overview?.login?.login || overview?.login || {}
  const signedCount = useMemo(() => rows.filter((acc) => acc.hot).length, [rows])

  function setNote(id: string, text: string) {
    setNoteById((prev) => ({ ...prev, [id]: text }))
  }

  async function onDeviceLogin(id: string) {
    setBusy({ id, kind: 'device' })
    setNote(id, t('starting'))
    try {
      const out = await startDeviceLogin(id)
      if (out.authUrl) {
        setUrlById((prev) => ({ ...prev, [id]: out.authUrl || '' }))
        setNote(id, out.message || t('loginOpenMsg'))
        window.open(out.authUrl, '_blank', 'noopener,noreferrer')
      }
      await refresh()
      for (let i = 0; i < 30; i++) {
        await new Promise((r) => setTimeout(r, 2000))
        const st = await fetchLoginStatus(id)
        const current = st.login || {}
        if (current.authUrl) setUrlById((prev) => ({ ...prev, [id]: current.authUrl }))
        setNote(id, current.message || t('waitingQoderLogin'))
        if (current.status === 'ok' || current.status === 'error') {
          await refresh()
          setNote(id, current.status === 'ok' ? t('qoderLoginDone') : current.message || t('error'))
          break
        }
      }
    } catch (err) {
      setNote(id, err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(null)
    }
  }

  async function onPatLogin(id: string) {
    const pat = (patById[id] || '').trim()
    if (!pat) {
      setNote(id, t('pastePatFirst'))
      return
    }
    setBusy({ id, kind: 'pat' })
    try {
      await loginWithPat(pat, id)
      setPatById((prev) => ({ ...prev, [id]: '' }))
      setNote(id, t('patDone'))
      await refresh()
    } catch (err) {
      setNote(id, err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(null)
    }
  }

  async function onRewarm(id: string) {
    setBusy({ id, kind: 'rewarm' })
    try {
      await rewarmWorker(id)
      await refresh()
      setNote(id, t('rewarmDone'))
    } catch (err) {
      setNote(id, err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(null)
    }
  }

  return (
    <div className="space-y-6">
      <section className="grid grid-cols-3 overflow-hidden rounded-2xl border border-white/10 bg-white/[0.02]">
        <div className="border-r border-white/10 px-4 py-4">
          <div className="text-xs text-zinc-500">{t('workers')}</div>
          <div className="mt-2 text-2xl font-semibold tracking-tight">{loading ? '…' : rows.length}</div>
        </div>
        <div className="border-r border-white/10 px-4 py-4">
          <div className="text-xs text-zinc-500">{t('signedIn')}</div>
          <div className="mt-2 text-2xl font-semibold tracking-tight">{loading ? '…' : signedCount}</div>
        </div>
        <div className="px-4 py-4">
          <div className="text-xs text-zinc-500">{t('needQoderLogin')}</div>
          <div className="mt-2 text-2xl font-semibold tracking-tight">{loading ? '…' : rows.length - signedCount}</div>
        </div>
      </section>

      {loading && !rows.length ? (
        <div className="text-sm text-zinc-400">{t('waitingOverview')}</div>
      ) : (
        <div className="overflow-hidden rounded-2xl border border-white/10 bg-white/[0.02]">
          {rows.map((acc, idx) => {
            const cooling = cooldownLabel(acc.down_until)
            const lastError = acc.last_error || acc.lastError
            const inFlight = acc.in_flight ?? acc.inFlight ?? 0
            const authUrl = urlById[acc.id]
            const note = noteById[acc.id] || (rows.length === 1 ? workerLogin.message : '')
            const thisBusy = busy?.id === acc.id ? busy.kind : null
            const status = statusLabel(acc, t)
            return (
              <article
                key={acc.id}
                className={`px-4 py-5 sm:px-5 ${idx ? 'border-t border-white/10' : ''}`}
              >
                <div className="flex flex-wrap items-start justify-between gap-3">
                  <div>
                    <div className="text-lg font-semibold tracking-tight">{acc.id}</div>
                    <div className="mt-1 text-sm text-zinc-400">{status}{cooling ? ` · ${t('cooldown')} ${cooling}` : ''}</div>
                  </div>
                  <dl className="flex flex-wrap gap-x-5 gap-y-1 text-xs text-zinc-500">
                    <div>{t('inFlight')} {inFlight}</div>
                    <div>{t('restarts')} {acc.restarts ?? 0}</div>
                    {acc.kind ? <div>{t('errorKind')} {acc.kind}</div> : null}
                  </dl>
                </div>

                <p className="mt-3 max-w-2xl text-sm text-zinc-500">{t('qoderLoginHint')}</p>

                <div className="mt-4 grid gap-4 lg:grid-cols-[minmax(0,1fr)_minmax(16rem,.9fr)] lg:items-end">
                  <div className="flex flex-wrap gap-2">
                    <Button size="sm" isPending={thisBusy === 'device'} onPress={() => void onDeviceLogin(acc.id)}>
                      {thisBusy === 'device' ? t('starting') : t('startBrowserLogin')}
                    </Button>
                    {acc.hot ? (
                      <Button size="sm" variant="secondary" isPending={thisBusy === 'rewarm'} onPress={() => void onRewarm(acc.id)}>
                        {thisBusy === 'rewarm' ? t('rewarming') : t('rewarm')}
                      </Button>
                    ) : null}
                  </div>
                  <div>
                    <div className="mb-1 text-xs text-zinc-500">{t('patFallback')}</div>
                    <div className="flex gap-2">
                      <Input
                        className="min-w-0 flex-1"
                        type="password"
                        value={patById[acc.id] || ''}
                        onChange={(e) => setPatById((prev) => ({ ...prev, [acc.id]: e.target.value }))}
                        placeholder={t('pasteToken')}
                        aria-label={`${acc.id} PAT`}
                      />
                      <Button size="sm" variant="secondary" isPending={thisBusy === 'pat'} onPress={() => void onPatLogin(acc.id)}>
                        {thisBusy === 'pat' ? t('loggingIn') : t('usePat')}
                      </Button>
                    </div>
                  </div>
                </div>

                {authUrl ? (
                  <div className="mt-4 border-t border-white/8 pt-4">
                    <div className="mb-1 text-xs text-zinc-500">{t('authUrl')}</div>
                    <code className="mono block break-all text-xs text-zinc-300">{authUrl}</code>
                    <div className="mt-3 flex flex-wrap gap-2">
                      <Button
                        size="sm"
                        variant="secondary"
                        onPress={async () => {
                          await navigator.clipboard.writeText(authUrl)
                          setCopied(acc.id)
                          setTimeout(() => setCopied(''), 900)
                        }}
                      >
                        {copied === acc.id ? t('copied') : t('copy')}
                      </Button>
                      <Button size="sm" variant="secondary" onPress={() => window.open(authUrl, '_blank', 'noopener,noreferrer')}>
                        {t('open')}
                      </Button>
                    </div>
                  </div>
                ) : null}

                {note || lastError ? (
                  <p className={`mt-4 text-sm ${lastError && !note ? 'text-red-400' : 'text-zinc-300'}`}>
                    {note || lastError}
                  </p>
                ) : null}
              </article>
            )
          })}
        </div>
      )}
    </div>
  )
}
