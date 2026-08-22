import { useState } from 'react'
import { Button, Card, Input, Label, ListBox, Select, TextArea } from '@heroui/react'
import { useI18n } from '@/hooks/useI18n'
import { useOverview } from '@/hooks/useOverview'
import { fetchLoginStatus, loginWithPat, rewarmWorker, startDeviceLogin } from '@/api/overview'

export function AuthPage() {
  const { t } = useI18n()
  const { overview, refresh } = useOverview()
  const [pat, setPat] = useState('')
  const [busy, setBusy] = useState<'device' | 'pat' | 'rewarm' | null>(null)
  const [message, setMessage] = useState('')
  const [authUrl, setAuthUrl] = useState('')
  const [status, setStatus] = useState('idle')
  const [accountId, setAccountId] = useState('')

  const auth = overview?.auth || {}
  const worker = overview?.worker || {}
  const login = overview?.login?.login || overview?.login || {}
  const accounts = overview?.accounts || []
  const selectedAccount = accountId || accounts[0]?.id || ''

  async function onDeviceLogin() {
    setBusy('device')
    try {
      const out = await startDeviceLogin(selectedAccount)
      if (out.authUrl) {
        setAuthUrl(out.authUrl)
        setStatus(out.status || 'pending')
        setMessage(out.message || t('loginOpenMsg'))
        window.open(out.authUrl, '_blank', 'noopener,noreferrer')
      }
      await refresh()
      for (let i = 0; i < 30; i++) {
        await new Promise((r) => setTimeout(r, 2000))
        const st = await fetchLoginStatus(selectedAccount)
        const current = st.login || {}
        setStatus(current.status || 'pending')
        setMessage(current.message || '')
        if (current.authUrl) setAuthUrl(current.authUrl)
        if (current.status === 'ok' || current.status === 'error') {
          await refresh()
          break
        }
      }
    } catch (err) {
      setMessage(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(null)
    }
  }

  async function onPatLogin() {
    if (!pat.trim()) {
      setMessage(t('pastePatFirst'))
      return
    }
    setBusy('pat')
    try {
      const out = await loginWithPat(pat.trim(), selectedAccount)
      setStatus((out as any).status || 'ok')
      setMessage(t('patDone'))
      setPat('')
      await refresh()
    } catch (err) {
      setMessage(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(null)
    }
  }

  async function onRewarm() {
    setBusy('rewarm')
    try {
      await rewarmWorker(selectedAccount)
      await refresh()
      setMessage(t('rewarm'))
    } catch (err) {
      setMessage(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(null)
    }
  }

  return (
    <div className="grid gap-4 xl:grid-cols-[1.15fr_.85fr]">
      <div className="space-y-4">
        <Card className="border border-white/10 bg-white/[0.02] p-5 shadow-none">
          <div className="mb-4 flex flex-wrap items-center justify-between gap-3">
            <div>
              <div className="text-lg font-semibold">{t('connectQoder')}</div>
              <div className="text-sm text-zinc-400">{t('signIn')}</div>
            </div>
            <Button variant="secondary" isPending={busy === 'rewarm'} onPress={() => void onRewarm()}>
              {busy === 'rewarm' ? t('rewarming') : t('rewarm')}
            </Button>
          </div>

          <div className="space-y-5">

          {accounts.length > 1 ? (
            <div className="mb-4">
              <div className="mb-1 text-xs text-zinc-500">{t('account')}</div>
              <Select
                selectedKey={selectedAccount}
                onSelectionChange={(key) => setAccountId(String(key))}
                aria-label={t('account')}
              >
                <Select.Trigger>
                  <Select.Value />
                  <Select.Indicator />
                </Select.Trigger>
                <Select.Popover>
                  <ListBox>
                    {accounts.map((acc) => (
                      <ListBox.Item key={acc.id} id={acc.id} textValue={acc.id}>
                        <Label>{acc.id}</Label>
                      </ListBox.Item>
                    ))}
                  </ListBox>
                </Select.Popover>
              </Select>
            </div>
          ) : null}

            <section className="rounded-2xl border border-white/10 p-4">
              <div className="mb-1 text-sm font-medium">{t('stepBrowserTitle')}</div>
              <p className="mb-3 text-sm text-zinc-400">{t('stepBrowserDesc')}</p>
              <Button isPending={busy === 'device'} onPress={() => void onDeviceLogin()}>
                {busy === 'device' ? t('starting') : t('startBrowserLogin')}
              </Button>
              <div className="mt-3 space-y-1 text-sm">
                <div className="text-zinc-500">{status || login.status || t('idle')}</div>
                <div>{message || login.message || t('readyWhen')}</div>
              </div>
              {(authUrl || login.authUrl) && (
                <div className="mt-3 rounded-xl border border-white/10 bg-black/20 p-3">
                  <div className="mb-2 text-xs text-zinc-500">{t('authUrl')}</div>
                  <code className="mono block break-all text-xs">{authUrl || login.authUrl}</code>
                  <div className="mt-3 flex flex-wrap gap-2">
                    <Button
                      size="sm"
                      variant="secondary"
                      onPress={async () => {
                        await navigator.clipboard.writeText(authUrl || login.authUrl)
                      }}
                    >
                      {t('copy')}
                    </Button>
                    <Button
                      size="sm"
                      variant="secondary"
                      onPress={() => window.open(authUrl || login.authUrl, '_blank', 'noopener,noreferrer')}
                    >
                      {t('open')}
                    </Button>
                  </div>
                </div>
              )}
            </section>

            <section className="rounded-2xl border border-white/10 p-4">
              <div className="mb-1 text-sm font-medium">{t('stepPatTitle')}</div>
              <p className="mb-3 text-sm text-zinc-400">{t('stepPatDesc')}</p>
              <div className="flex flex-col gap-3 sm:flex-row">
                <Input
                  className="flex-1"
                  value={pat}
                  onChange={(e) => setPat(e.target.value)}
                  placeholder={t('pasteToken')}
                  aria-label={t('pat')}
                />
                <Button isPending={busy === 'pat'} onPress={() => void onPatLogin()}>
                  {busy === 'pat' ? t('loggingIn') : t('loginWithPat')}
                </Button>
              </div>
            </section>
          </div>
        </Card>
      </div>

      <div className="space-y-4">
        <Card className="border border-white/10 bg-white/[0.02] p-4 shadow-none">
          <div className="mb-3 text-sm font-medium">{t('localState')}</div>
          <dl className="space-y-3 text-sm">
            {[
              [t('hasUserBlob'), auth.has_user_blob ? t('yes') : t('no')],
              [t('userBlobBytes'), String(auth.user_blob_bytes ?? 0)],
              [t('machineId'), auth.machine_id || '—'],
              [t('workerHot'), worker.hot ? t('yes') : t('no')],
              [t('workerEndpoint'), worker.endpoint || '—'],
              [t('rewarmCount'), String(worker.rewarmCount ?? worker.rewarm_count ?? 0)],
            ].map(([k, v]) => (
              <div key={String(k)} className="flex items-start justify-between gap-4 border-b border-white/5 pb-2 last:border-0">
                <dt className="text-zinc-500">{k}</dt>
                <dd className="max-w-[60%] break-all text-right font-medium">{v}</dd>
              </div>
            ))}
          </dl>
        </Card>

        <Card className="border border-white/10 bg-white/[0.02] p-4 shadow-none">
          <div className="mb-3 text-sm font-medium">{t('raw')}</div>
          <TextArea
            readOnly
            className="min-h-48 font-mono text-xs"
            value={JSON.stringify({ auth, worker, login }, null, 2)}
          />
        </Card>
      </div>
    </div>
  )
}
