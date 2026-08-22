import { useState } from 'react'
import { Navigate, useLocation, useNavigate } from 'react-router-dom'
import { Button, ButtonGroup, InputGroup } from '@heroui/react'
import { Eye, EyeOff } from 'lucide-react'
import { useI18n } from '@/hooks/useI18n'
import { useApiKey } from '@/hooks/useApiKey'
import { useOverview } from '@/hooks/useOverview'
import { isUnauthorized } from '@/api/client'

export function LoginPage() {
  const { t, lang, setLang } = useI18n()
  const { setApiKey } = useApiKey()
  const { overview, refresh } = useOverview()
  const [password, setPassword] = useState('')
  const [showPassword, setShowPassword] = useState(false)
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
      <div className="relative z-10 mx-auto grid min-h-dvh w-full max-w-6xl lg:grid-cols-[1.15fr_.85fr]">
        <section className="flex flex-col justify-between border-white/10 px-5 py-8 sm:px-10 lg:border-r lg:px-12 lg:py-12">
          <div className="flex items-center justify-between gap-4">
            <div>
              <div className="text-sm font-semibold tracking-tight">CLI2API</div>
              <div className="mt-1 text-xs text-zinc-500">{t('brandSub')}</div>
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

          <div className="max-w-xl py-16 lg:py-0">
            <p className="mb-3 text-xs font-semibold uppercase tracking-[0.08em] text-emerald-400">{t('loginKicker')}</p>
            <h1 className="text-4xl font-semibold tracking-tight text-white sm:text-5xl">{t('loginTitle')}</h1>
            <p className="mt-4 max-w-md text-sm leading-6 text-zinc-400">{t('loginLead')}</p>
            <dl className="mt-10 max-w-md space-y-4 text-sm">
              <div className="flex items-start justify-between gap-6 border-b border-white/8 pb-3">
                <dt className="text-zinc-500">{t('loginUnlocks')}</dt>
                <dd className="text-right font-medium text-zinc-200">{t('loginUnlocksValue')}</dd>
              </div>
              <div className="flex items-start justify-between gap-6 border-b border-white/8 pb-3">
                <dt className="text-zinc-500">{t('loginNot')}</dt>
                <dd className="text-right font-medium text-zinc-200">{t('loginNotValue')}</dd>
              </div>
              <div className="flex items-start justify-between gap-6">
                <dt className="text-zinc-500">{t('loginNext')}</dt>
                <dd className="text-right font-medium text-zinc-200">{t('loginNextValue')}</dd>
              </div>
            </dl>
          </div>

          <p className="hidden text-xs text-zinc-600 lg:block">{t('loginHint')}</p>
        </section>

        <section className="flex items-center px-5 py-8 sm:px-10 lg:px-12">
          <form className="w-full max-w-sm space-y-5" onSubmit={(e) => void onSubmit(e)}>
            <div>
              <h2 className="text-lg font-semibold tracking-tight">{t('loginFormTitle')}</h2>
              <p className="mt-1 text-sm text-zinc-500">{t('loginDesc')}</p>
            </div>
            <div>
              <label className="mb-1 block text-xs text-zinc-500" htmlFor="console-password">
                {t('loginPassword')}
              </label>
              <InputGroup fullWidth>
                <InputGroup.Input
                  id="console-password"
                  type={showPassword ? 'text' : 'password'}
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                  placeholder={t('loginPasswordPh')}
                  autoComplete="current-password"
                  autoFocus
                  aria-label={t('loginPassword')}
                />
                <InputGroup.Suffix>
                  <Button
                    isIconOnly
                    size="sm"
                    variant="ghost"
                    type="button"
                    className="text-zinc-400"
                    onPress={() => setShowPassword((v) => !v)}
                    aria-label={showPassword ? t('hidePassword') : t('showPassword')}
                  >
                    {showPassword ? <EyeOff size={16} /> : <Eye size={16} />}
                  </Button>
                </InputGroup.Suffix>
              </InputGroup>
            </div>
            {message ? <p className="text-sm text-red-400">{message}</p> : null}
            <Button type="submit" className="w-full" isPending={busy}>
              {busy ? t('loggingInConsole') : t('loginSubmit')}
            </Button>
            <p className="text-xs leading-5 text-zinc-600 lg:hidden">{t('loginHint')}</p>
          </form>
        </section>
      </div>
    </div>
  )
}
