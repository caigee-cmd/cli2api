import { useMemo, useState } from 'react'
import { Button, Card, Chip, Input } from '@heroui/react'
import { useI18n } from '@/hooks/useI18n'
import { useOverview } from '@/hooks/useOverview'
import { fetchLoginStatus, loginWithPat, rewarmWorker, startDeviceLogin } from '@/api/overview'

function cooldownLabel(until?: string | null) {
  if (!until) return ''
  const ms = Date.parse(until) - Date.now()
  if (!Number.isFinite(ms) || ms <= 0) return ''
  const sec = Math.ceil(ms / 1000)
  if (sec < 60) return `${sec}s`
  return `${Math.ceil(sec / 60)}m`
}

type AccountBusy = { id: string; kind: 'device' | 'pat' | 'rewarm' }

export function AccountsPage() {
  const { t } = useI18n()
  const { overview, loading, refresh } = useOverview()
  const accounts = overview?.accounts || []
  const rows = accounts.length ? accounts : [{ id: 'default', ready: !!overview?.worker?.ok, hot: !!overview?.worker?.hot }]
  const [busy, setBusy] = useState<AccountBusy | null>(null)
  const [patById, setPatById] = useState<Record<string, string>>({})
  const [noteById, setNoteById] = useState<Record<string, string>>({})
  const [urlById, setUrlById] = useState<Record<string, string>>({})

  const workerLogin = overview?.login?.login || overview?.login || {}

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

  const signedCount = useMemo(
    () => rows.filter((acc) => acc.hot || acc.ready).length,
    [rows],
  )

  return (
    <div className="space-y-4">
      <div>
        <p className="mb-1 text-xs font-semibold uppercase tracking-[0.08em] text-emerald-400">{t('catalog')}</p>
        <h2 className="text-2xl font-semibold tracking-tight sm:text-3xl">{t('accountsTitle')}</h2>
        <p className="mt-1 text-sm text-zinc-400">{t('accountsDesc')}</p>
        <p className="mt-2 text-xs text-zinc-500">{t('accountsSigned', { n: signedCount, total: rows.length })}</p>
      </div>
      {loading && !rows.length ? (
        <div className="text-sm text-zinc-400">{t('waitingOverview')}</div>
      ) : (
        <div className="space-y-3">
          {rows.map((acc) => {
            const cooling = cooldownLabel(acc.down_until)
            const lastError = acc.last_error || acc.lastError
            const inFlight = acc.in_flight ?? acc.inFlight ?? 0
            const authUrl = urlById[acc.id]
            const note = noteById[acc.id]
            const thisBusy = busy?.id === acc.id ? busy.kind : null
            return (
              <Card key={acc.id} className="border border-white/10 bg-white/[0.02] p-5 shadow-none">
                <div className="mb-3 flex flex-wrap items-start justify-between gap-3">
                  <div>
                    <div className="text-lg font-semibold">{acc.id}</div>
                    <div className="mt-1 text-xs text-zinc-500">{t('qoderLoginHint')}</div>
                  </div>
                  <div className="flex flex-wrap gap-2">
                    <Chip size="sm" variant="soft" color={acc.ready ? 'success' : 'warning'}>
                      {acc.ready ? t('ready') : t('degraded')}
                    </Chip>
                    {acc.hot ? (
                      <Chip size="sm" variant="soft" color="success">
                        {t('hot')}
                      </Chip>
                    ) : (
                      <Chip size="sm" variant="soft" color="warning">
                        {t('needQoderLogin')}
                      </Chip>
                    )}
                    {cooling ? (
                      <Chip size="sm" variant="soft" color="warning">
                        {t('cooldown')}: {cooling}
                      </Chip>
                    ) : null}
                  </div>
                </div>

                <div className="mb-4 flex flex-wrap gap-3 text-xs text-zinc-500">
                  <span>{t('inFlight')}: {inFlight}</span>
                  <span>{t('restarts')}: {acc.restarts ?? 0}</span>
                  {acc.kind ? <span>{t('errorKind')}: {acc.kind}</span> : null}
                </div>

                <div className="grid gap-3 lg:grid-cols-[1fr_auto]">
                  <div className="flex flex-wrap gap-2">
                    <Button
                      size="sm"
                      isPending={thisBusy === 'device'}
                      onPress={() => void onDeviceLogin(acc.id)}
                    >
                      {thisBusy === 'device' ? t('starting') : t('startBrowserLogin')}
                    </Button>
                    <Button
                      size="sm"
                      variant="secondary"
                      isPending={thisBusy === 'rewarm'}
                      onPress={() => void onRewarm(acc.id)}
                    >
                      {thisBusy === 'rewarm' ? t('rewarming') : t('rewarm')}
                    </Button>
                  </div>
                  <div className="flex min-w-0 flex-col gap-2 sm:flex-row">
                    <Input
                      className="sm:w-64"
                      type="password"
                      value={patById[acc.id] || ''}
                      onChange={(e) => setPatById((prev) => ({ ...prev, [acc.id]: e.target.value }))}
                      placeholder={t('pasteToken')}
                      aria-label={`${acc.id} PAT`}
                    />
                    <Button
                      size="sm"
                      variant="secondary"
                      isPending={thisBusy === 'pat'}
                      onPress={() => void onPatLogin(acc.id)}
                    >
                      {thisBusy === 'pat' ? t('loggingIn') : t('loginWithPat')}
                    </Button>
                  </div>
                </div>

                {authUrl ? (
                  <div className="mt-4 rounded-xl border border-white/10 bg-black/20 p-3">
                    <div className="mb-2 text-xs text-zinc-500">{t('authUrl')}</div>
                    <code className="mono block break-all text-xs">{authUrl}</code>
                    <div className="mt-3 flex flex-wrap gap-2">
                      <Button
                        size="sm"
                        variant="secondary"
                        onPress={async () => {
                          await navigator.clipboard.writeText(authUrl)
                        }}
                      >
                        {t('copy')}
                      </Button>
                      <Button
                        size="sm"
                        variant="secondary"
                        onPress={() => window.open(authUrl, '_blank', 'noopener,noreferrer')}
                      >
                        {t('open')}
                      </Button>
                    </div>
                  </div>
                ) : null}

                {note || lastError || workerLogin.message ? (
                  <div className="mt-3 text-sm text-zinc-300">
                    {note || lastError || (rows.length === 1 ? workerLogin.message : '')}
                  </div>
                ) : null}
              </Card>
            )
          })}
        </div>
      )}
    </div>
  )
}
