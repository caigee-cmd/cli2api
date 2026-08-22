import { useState } from 'react'
import { Navigate, useLocation, useNavigate } from 'react-router-dom'
import { Button, ButtonGroup, Card, Input } from '@heroui/react'
import { PlugZap } from 'lucide-react'
import { useI18n } from '@/hooks/useI18n'
import { useApiKey } from '@/hooks/useApiKey'
import { useOverview } from '@/hooks/useOverview'
import { isUnauthorized } from '@/api/client'

export function LoginPage() {
  const { t, lang, setLang } = useI18n()
  const { setApiKey } = useApiKey()
  const { overview, refresh } = useOverview()
  const [password, setPassword] = useState('')
  const [busy, setBusy] = useState(false)
  const [message, setMessage] = useState('')
  const navigate = useNavigate()
  const location = useLocation()
  const from = (location.state as { from?: string } | null)?.from || '/'

  if (overview) {
    return <Navigate to={from === '/login' ? '/' : from} replace />
  }

  async function onSubmit(e: React.FormEvent) {
    e.preventDefault()
    const next = password.trim()
    if (!next) {
      setMessage(t('loginNeedKey'))
      return
    }
    setBusy(true)
    setMessage('')
    try {
      setApiKey(next)
      await refresh(next)
      navigate(from === '/login' ? '/' : from, { replace: true })
    } catch (err) {
      setMessage(isUnauthorized(err) ? t('loginBadKey') : err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="relative min-h-dvh bg-[#0b0f14] text-zinc-100">
      <div className="noise" aria-hidden />
      <div className="relative z-10 mx-auto flex min-h-dvh max-w-md flex-col justify-center px-4 py-10">
        <div className="mb-8 flex items-center justify-between">
          <div className="flex items-center gap-3">
            <div className="grid size-10 place-items-center rounded-xl bg-emerald-500/15 text-emerald-400 ring-1 ring-emerald-500/30">
              <PlugZap size={18} />
            </div>
            <div>
              <div className="text-base font-semibold tracking-tight">CLI2API</div>
              <div className="text-xs text-zinc-400">{t('brandSub')}</div>
            </div>
          </div>
          <ButtonGroup>
            <Button size="sm" variant={lang === 'zh' ? 'primary' : 'secondary'} onPress={() => setLang('zh')}>
              中文
            </Button>
            <Button size="sm" variant={lang === 'en' ? 'primary' : 'secondary'} onPress={() => setLang('en')}>
              EN
            </Button>
          </ButtonGroup>
        </div>

        <Card className="border border-white/10 bg-white/[0.03] p-6 shadow-none">
          <p className="mb-1 text-xs font-semibold uppercase tracking-[0.08em] text-emerald-400">{t('loginKicker')}</p>
          <h1 className="text-2xl font-semibold tracking-tight">{t('loginTitle')}</h1>
          <p className="mt-2 text-sm text-zinc-400">{t('loginDesc')}</p>

          <form className="mt-6 space-y-4" onSubmit={(e) => void onSubmit(e)}>
            <div>
              <div className="mb-1 text-xs text-zinc-500">{t('loginPassword')}</div>
              <Input
                type="password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                placeholder={t('loginPasswordPh')}
                autoComplete="current-password"
                aria-label={t('loginPassword')}
              />
            </div>
            {message ? <div className="text-sm text-red-400">{message}</div> : null}
            <Button type="submit" className="w-full" isPending={busy}>
              {busy ? t('loggingInConsole') : t('loginSubmit')}
            </Button>
          </form>
        </Card>
        <p className="mt-4 text-center text-xs text-zinc-500">{t('loginHint')}</p>
      </div>
    </div>
  )
}
