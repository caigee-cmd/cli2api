import { Menu, RefreshCw } from 'lucide-react'
import { Button, ButtonGroup } from '@heroui/react'
import { useI18n } from '@/hooks/useI18n'
import { useOverview } from '@/hooks/useOverview'

export function AppHeader({
  title,
  kicker,
  desc,
  onMenu,
}: {
  title: string
  kicker: string
  desc: string
  onMenu: () => void
}) {
  const { lang, setLang, t } = useI18n()
  const { loading, refresh } = useOverview()

  return (
    <header className="mb-6 flex flex-col gap-4 border-b border-white/10 pb-5 sm:flex-row sm:items-end sm:justify-between">
      <div className="min-w-0">
        <div className="mb-2 flex items-center gap-2 lg:hidden">
          <Button isIconOnly size="sm" variant="secondary" onPress={onMenu}>
            <Menu size={16} />
          </Button>
          <span className="text-xs uppercase tracking-[0.08em] text-emerald-400">{t('menu')}</span>
        </div>
        <p className="mb-1 text-xs font-semibold uppercase tracking-[0.08em] text-emerald-400">{kicker}</p>
        <h1 className="truncate text-3xl font-semibold tracking-tight text-white sm:text-4xl">{title}</h1>
        <p className="mt-2 max-w-2xl text-sm text-zinc-400">{desc}</p>
      </div>

      <div className="flex flex-wrap items-center gap-2">
        <ButtonGroup>
          <Button
            size="sm"
            variant={lang === 'zh' ? 'primary' : 'secondary'}
            onPress={() => setLang('zh')}
          >
            中文
          </Button>
          <Button
            size="sm"
            variant={lang === 'en' ? 'primary' : 'secondary'}
            onPress={() => setLang('en')}
          >
            EN
          </Button>
        </ButtonGroup>
        <Button
          size="sm"
          variant="secondary"
          isPending={loading}
          onPress={() => void refresh()}
        >
          <RefreshCw size={14} />
          {loading ? t('refreshing') : t('refresh')}
        </Button>
      </div>
    </header>
  )
}
