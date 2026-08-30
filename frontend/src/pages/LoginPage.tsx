import { useRef, useState } from 'react'
import { Navigate, useLocation, useNavigate } from 'react-router-dom'
import { Button, Card, InputGroup, Label, TextField, Toolbar, Tooltip } from '@heroui/react'
import { ArrowRight, Eye, EyeSlash, Globe, Moon, Sun } from '@phosphor-icons/react'
import { BrandMark } from '@/components/BrandMark'
import { PageAlert } from '@/components/ui/PageAlert'
import { useI18n } from '@/hooks/useI18n'
import { useApiKey } from '@/hooks/useApiKey'
import { useOverview } from '@/hooks/useOverview'
import { useTheme } from '@/hooks/useTheme'
import { isUnauthorized } from '@/api/client'
import { useGsapReveal } from '@/hooks/useGsapReveal'

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
  const pageRef = useRef<HTMLElement>(null)
  useGsapReveal(pageRef, 'login')

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
    <div className="relative min-h-dvh overflow-hidden bg-background text-foreground">
      <div className="absolute inset-x-0 top-0 h-px bg-border" />
      <main ref={pageRef} className="relative z-10 mx-auto grid min-h-dvh w-full max-w-[1480px] lg:grid-cols-[minmax(0,1.22fr)_minmax(420px,.78fr)]">
        <section className="flex min-h-[52vh] flex-col border-separator px-5 py-6 sm:px-10 sm:py-8 lg:min-h-dvh lg:border-r lg:px-14 lg:py-10 xl:px-20">
          <header className="flex items-center justify-between gap-4">
            <div className="flex items-center gap-3">
              <BrandMark size={36} />
              <div>
                <div className="font-semibold tracking-[-0.015em]">CLI2API</div>
                <div className="text-xs text-muted">{t('controlPlane')}</div>
              </div>
            </div>
            <Toolbar isAttached>
              <Tooltip>
                <Tooltip.Trigger>
                  <Button isIconOnly size="sm" variant="ghost" onPress={() => setLang(lang === 'zh' ? 'en' : 'zh')} aria-label={lang === 'zh' ? 'English' : '中文'}>
                    <Globe size={15} />
                  </Button>
                </Tooltip.Trigger>
                <Tooltip.Content>{lang === 'zh' ? 'English' : '中文'}</Tooltip.Content>
              </Tooltip>
              <Tooltip>
                <Tooltip.Trigger>
                  <Button isIconOnly size="sm" variant="ghost" onPress={toggleTheme} aria-label={theme === 'dark' ? 'Light mode' : 'Dark mode'}>
                    {theme === 'dark' ? <Sun size={15} /> : <Moon size={15} />}
                  </Button>
                </Tooltip.Trigger>
                <Tooltip.Content>{theme === 'dark' ? 'Light mode' : 'Dark mode'}</Tooltip.Content>
              </Tooltip>
            </Toolbar>
          </header>

          <div className="my-auto max-w-3xl py-16 lg:py-24">
            <p data-gsap-reveal className="mb-5 mono text-xs text-muted">:3010</p>
            <h1 data-gsap-reveal className="max-w-3xl text-[clamp(2.25rem,4vw,3.6rem)] font-semibold leading-[0.98] tracking-[-0.045em] text-balance">
              {t('loginTitle')}
            </h1>
            <p data-gsap-reveal className="mt-6 max-w-xl text-base leading-7 text-muted sm:text-lg">{t('loginLead')}</p>

            <p data-gsap-reveal className="mt-10 max-w-xl border-t border-separator pt-5 text-sm leading-6 text-muted">
              {t('loginHint')}
            </p>
          </div>

          <footer className="hidden items-center justify-between text-xs text-muted lg:flex">
            <span>{t('loginHint')}</span>
            <span>{t('routesHint')}</span>
          </footer>
        </section>

        <section className="flex items-center px-5 py-10 sm:px-10 lg:px-12 xl:px-16">
          <Card data-gsap-reveal className="w-full p-0">
            <form onSubmit={(event) => void onSubmit(event)}>
              <Card.Header className="border-b border-separator px-6 py-6 sm:px-8">
                <div className="flex min-w-0 items-center gap-3">
                  <BrandMark size={28} />
                  <div>
                    <Card.Title className="text-xl tracking-[-0.015em]">{t('loginFormTitle')}</Card.Title>
                    <Card.Description className="mt-1">{t('loginDesc')}</Card.Description>
                  </div>
                </div>
              </Card.Header>
              <Card.Content className="space-y-5 px-6 py-7 sm:px-8 sm:py-8">
                <TextField name="console-password" className="w-full" isInvalid={Boolean(message)}>
                  <Label>{t('loginPassword')}</Label>
                  <InputGroup>
                    <InputGroup.Input
                      id="console-password"
                      type={showPassword ? 'text' : 'password'}
                      value={password}
                      onChange={(event) => setPassword(event.target.value)}
                      placeholder={t('loginPasswordPh')}
                      autoComplete="current-password"
                      autoFocus
                      aria-label={t('loginPassword')}
                      spellCheck={false}
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
                        {showPassword ? <EyeSlash size={16} /> : <Eye size={16} />}
                      </Button>
                    </InputGroup.Suffix>
                  </InputGroup>
                </TextField>
                {message ? <PageAlert title={message} /> : <p className="text-xs text-muted">{t('loginHint')}</p>}
                <Button type="submit" fullWidth size="lg" isPending={busy}>
                  {busy ? t('loggingInConsole') : t('loginSubmit')}
                  {!busy ? <ArrowRight size={17} /> : null}
                </Button>
              </Card.Content>
            </form>
          </Card>
        </section>
      </main>
    </div>
  )
}
