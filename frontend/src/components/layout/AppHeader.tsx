import { ArrowClockwise, Globe, List, Moon, SignOut, Sun } from '@phosphor-icons/react'
import { ShellIconButton, ShellTooltip } from '@/components/ui/ShellControls'
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
    <header className="relative z-20 shrink-0 border-b border-separator bg-background">
      <div className="mx-auto flex w-full max-w-[1480px] items-center gap-4 px-4 py-2 sm:px-6 lg:px-8">
        <ShellIconButton label={t('menu')} className="lg:hidden" onClick={onMenu}>
          <List size={17} />
        </ShellIconButton>

        <div className="min-w-0 flex-1">
          <div className="flex min-w-0 items-baseline gap-3">
            <span className="status-dot translate-y-[-1px]" data-state={ready ? 'ok' : 'danger'} />
            <h1 className="truncate text-title font-semibold">{title}</h1>
            <p className="hidden truncate text-body text-muted xl:block">{desc}</p>
          </div>
        </div>

        <div className="flex items-center gap-1">
          <ShellTooltip label={t('refresh')}>
            <ShellIconButton label={t('refresh')} pending={loading} onClick={() => void refresh().catch(() => undefined)}>
              <ArrowClockwise size={15} />
            </ShellIconButton>
          </ShellTooltip>
          <ShellTooltip label={lang === 'zh' ? 'English' : '中文'}>
            <ShellIconButton label={lang === 'zh' ? 'English' : '中文'} onClick={() => setLang(lang === 'zh' ? 'en' : 'zh')}>
              <Globe size={15} />
            </ShellIconButton>
          </ShellTooltip>
          <ShellTooltip label={theme === 'dark' ? 'Light mode' : 'Dark mode'}>
            <ShellIconButton label={theme === 'dark' ? 'Light mode' : 'Dark mode'} onClick={toggleTheme}>
              {theme === 'dark' ? <Sun size={15} /> : <Moon size={15} />}
            </ShellIconButton>
          </ShellTooltip>
          <ShellTooltip label={t('signOut')}>
            <ShellIconButton
              label={t('signOut')}
              onClick={() => {
                signOut()
                navigate('/login', { replace: true })
              }}
            >
              <SignOut size={15} />
            </ShellIconButton>
          </ShellTooltip>
        </div>
      </div>
    </header>
  )
}
