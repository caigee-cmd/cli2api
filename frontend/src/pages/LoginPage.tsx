import { useState } from 'react'
import { Navigate, useLocation, useNavigate } from 'react-router-dom'
import { Button, ButtonGroup, Card, Chip, InputGroup } from '@heroui/react'
import { ArrowRight, Eye, EyeOff, KeyRound, Moon, ServerCog, ShieldCheck, Sun } from 'lucide-react'
import { useI18n } from '@/hooks/useI18n'
import { useApiKey } from '@/hooks/useApiKey'
import { useOverview } from '@/hooks/useOverview'
import { useTheme } from '@/hooks/useTheme'
import { isUnauthorized } from '@/api/client'

export function LoginPage() {
  const { t, lang, setLang } = useI18n()
  const { setApiKey } = useApiKey()
  const { overview, refresh } = useOverview()
  const { theme, toggleTheme } = useTheme()
  const [password, setPassword] = useState('')
  const [showPassword, setShowPassword] = useState(false)
  const [busy, setBusy] = useState(false)
  const [message, setMessage] = useState('')
  const navigate = useNavigate()
  const location = useLocation()
  const from = (location.state as { from?: string } | null)?.from || '/'

  if (overview) return <Navigate to={from === '/login' ? '/' : from} replace />

  async function onSubmit(event: React.FormEvent) {
    event.preventDefault()
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
    } catch (error) {
      setMessage(isUnauthorized(error) ? t('loginBadKey') : error instanceof Error ? error.message : String(error))
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="relative min-h-dvh overflow-hidden bg-[var(--app-bg)] text-[var(--app-ink)]">
      <div className="noise" aria-hidden />
      <div className="absolute inset-x-0 top-0 h-px bg-[var(--accent)] opacity-70" />
      <main className="relative z-10 mx-auto grid min-h-dvh w-full max-w-[1480px] lg:grid-cols-[minmax(0,1.22fr)_minmax(420px,.78fr)]">
        <section className="flex min-h-[52vh] flex-col border-[var(--app-line)] px-5 py-6 sm:px-10 sm:py-8 lg:min-h-dvh lg:border-r lg:px-14 lg:py-10 xl:px-20">
          <header className="flex items-center justify-between gap-4">
            <div className="flex items-center gap-3">
              <div className="grid size-10 place-items-center rounded-xl border border-[var(--accent-line)] bg-[var(--accent-soft)] text-[var(--accent)]">
                <ServerCog size={19} />
              </div>
              <div>
                <div className="font-semibold tracking-[-0.02em]">Qoder API Proxy</div>
                <div className="text-xs text-[var(--app-faint)]">{t('controlPlane')}</div>
              </div>
            </div>
            <div className="flex items-center gap-2">
              <ButtonGroup>
                <Button size="sm" variant={lang === 'zh' ? 'primary' : 'secondary'} onPress={() => setLang('zh')}>中</Button>
                <Button size="sm" variant={lang === 'en' ? 'primary' : 'secondary'} onPress={() => setLang('en')}>EN</Button>
              </ButtonGroup>
              <Button isIconOnly size="sm" variant="secondary" onPress={toggleTheme} aria-label={theme === 'dark' ? 'Light mode' : 'Dark mode'}>
                {theme === 'dark' ? <Sun size={16} /> : <Moon size={16} />}
              </Button>
            </div>
          </header>

          <div className="my-auto max-w-3xl py-16 lg:py-24">
            <div className="mb-5 flex items-center gap-3">
              <Chip size="sm" variant="soft" color="success">LOCAL / PRIVATE</Chip>
              <span className="mono text-xs text-[var(--app-faint)]">:3010</span>
            </div>
            <p className="mb-3 text-xs font-semibold tracking-[0.14em] text-[var(--accent)] uppercase">{t('loginKicker')}</p>
            <h1 className="max-w-3xl text-[clamp(2.8rem,6vw,5.8rem)] font-semibold leading-[0.92] tracking-[-0.055em] text-balance">
              {t('loginTitle')}
            </h1>
            <p className="mt-6 max-w-xl text-base leading-7 text-[var(--app-muted)] sm:text-lg">{t('loginLead')}</p>

            <div className="mt-12 grid max-w-2xl border-y border-[var(--app-line)] sm:grid-cols-3">
              {[
                [ShieldCheck, t('loginUnlocks'), t('loginUnlocksValue')],
                [KeyRound, t('loginNot'), t('loginNotValue')],
                [ArrowRight, t('loginNext'), t('loginNextValue')],
              ].map(([Icon, label, value], index) => {
                const IconComponent = Icon as typeof ShieldCheck
                return (
                  <div key={String(label)} className={`py-5 sm:px-5 ${index ? 'border-t border-[var(--app-line)] sm:border-l sm:border-t-0' : ''}`}>
                    <IconComponent size={16} className="mb-4 text-[var(--accent)]" />
                    <div className="text-xs text-[var(--app-faint)]">{String(label)}</div>
                    <div className="mt-1.5 text-sm font-medium leading-5">{String(value)}</div>
                  </div>
                )
              })}
            </div>
          </div>

          <footer className="hidden items-center justify-between text-xs text-[var(--app-faint)] lg:flex">
            <span>{t('loginHint')}</span>
            <span className="mono">QODER / OPENAI COMPATIBLE</span>
          </footer>
        </section>

        <section className="flex items-center px-5 py-10 sm:px-10 lg:px-12 xl:px-16">
          <Card className="app-panel w-full rounded-2xl p-0 shadow-none">
            <form onSubmit={(event) => void onSubmit(event)}>
              <div className="border-b border-[var(--app-line)] px-6 py-6 sm:px-8">
                <div className="flex items-center justify-between gap-3">
                  <div>
                    <h2 className="text-xl font-semibold tracking-[-0.025em]">{t('loginFormTitle')}</h2>
                    <p className="mt-1 text-sm text-[var(--app-muted)]">{t('loginDesc')}</p>
                  </div>
                  <span className="status-dot" data-state="ok" />
                </div>
              </div>
              <div className="space-y-5 px-6 py-7 sm:px-8 sm:py-8">
                <div>
                  <label className="mb-2 block text-xs font-medium text-[var(--app-muted)]" htmlFor="console-password">
                    {t('loginPassword')}
                  </label>
                  <InputGroup fullWidth>
                    <InputGroup.Input
                      id="console-password"
                      type={showPassword ? 'text' : 'password'}
                      value={password}
                      onChange={(event) => setPassword(event.target.value)}
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
                        aria-label={showPassword ? t('hidePassword') : t('showPassword')}
                        onPress={() => setShowPassword((value) => !value)}
                      >
                        {showPassword ? <EyeOff size={16} /> : <Eye size={16} />}
                      </Button>
                    </InputGroup.Suffix>
                  </InputGroup>
                  <p className={`mt-2 min-h-5 text-xs ${message ? 'text-[var(--app-danger)]' : 'text-[var(--app-faint)]'}`}>
                    {message || t('loginHint')}
                  </p>
                </div>
                <Button type="submit" fullWidth size="lg" isPending={busy}>
                  {busy ? t('loggingInConsole') : t('loginSubmit')}
                  {!busy ? <ArrowRight size={17} /> : null}
                </Button>
              </div>
            </form>
          </Card>
        </section>
      </main>
    </div>
  )
}
