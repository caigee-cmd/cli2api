import { SignOut, List, Moon, ArrowClockwise, Sun } from '@phosphor-icons/react'
import { Button, ButtonGroup, Tooltip } from '@heroui/react'
import { useNavigate } from 'react-router-dom'
import { useI18n } from '@/hooks/useI18n'
import { useOverview } from '@/hooks/useOverview'
import { useApiKey } from '@/hooks/useApiKey'
import { useTheme } from '@/hooks/useTheme'

export function AppHeader({
  title,
  desc,
  onMenu,
}: {
  title: string
  desc: string
  onMenu: () => void
}) {
  const { lang, setLang, t } = useI18n()
  const { loading, refresh, overview } = useOverview()
  const { signOut } = useApiKey()
  const { theme, toggleTheme } = useTheme()
  const navigate = useNavigate()
  const ready = Boolean(overview?.worker?.ok)

  return (
    <header className="relative z-20 shrink-0 border-b border-[var(--app-line)] bg-[var(--app-bg)]">
      <div className="mx-auto flex w-full max-w-[1480px] items-center gap-4 px-4 py-3 sm:px-6 lg:px-8">
        <Button isIconOnly size="sm" variant="ghost" className="lg:hidden" onPress={onMenu} aria-label={t('menu')}>
          <List size={17} />
        </Button>

        <div className="min-w-0 flex-1">
          <div className="flex min-w-0 items-baseline gap-3">
            <span className="status-dot translate-y-[-1px]" data-state={ready ? 'ok' : 'danger'} />
            <h1 className="truncate text-xl font-semibold tracking-[-0.025em] sm:text-2xl">{title}</h1>
            <p className="hidden truncate text-sm text-[var(--app-muted)] xl:block">{desc}</p>
          </div>
        </div>

        <div className="flex items-center gap-1 sm:gap-1.5">
          <ButtonGroup className="hidden sm:flex">
            <Button size="sm" variant={lang === 'zh' ? 'primary' : 'secondary'} onPress={() => setLang('zh')}>中</Button>
            <Button size="sm" variant={lang === 'en' ? 'primary' : 'secondary'} onPress={() => setLang('en')}>EN</Button>
          </ButtonGroup>
          <Tooltip>
            <Tooltip.Trigger>
              <Button isIconOnly size="sm" variant="secondary" onPress={toggleTheme} aria-label={theme === 'dark' ? 'Light mode' : 'Dark mode'}>
                {theme === 'dark' ? <Sun size={16} /> : <Moon size={16} />}
              </Button>
            </Tooltip.Trigger>
            <Tooltip.Content>{theme === 'dark' ? 'Light mode' : 'Dark mode'}</Tooltip.Content>
          </Tooltip>
          <Tooltip>
            <Tooltip.Trigger>
              <Button isIconOnly size="sm" variant="secondary" isPending={loading} onPress={() => void refresh().catch(() => undefined)} aria-label={t('refresh')}>
                <ArrowClockwise size={16} />
              </Button>
            </Tooltip.Trigger>
            <Tooltip.Content>{t('refresh')}</Tooltip.Content>
          </Tooltip>
          <Tooltip>
            <Tooltip.Trigger>
              <Button
                isIconOnly
                size="sm"
                variant="ghost"
                onPress={() => {
                  signOut()
                  navigate('/login', { replace: true })
                }}
                aria-label={t('signOut')}
              >
                <SignOut size={16} />
              </Button>
            </Tooltip.Trigger>
            <Tooltip.Content>{t('signOut')}</Tooltip.Content>
          </Tooltip>
        </div>
      </div>
    </header>
  )
}
