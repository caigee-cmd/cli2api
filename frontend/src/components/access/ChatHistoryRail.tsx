import { useEffect, useRef } from 'react'
import { Button } from '@heroui/react'
import { CaretLeft, Plus, SidebarSimple } from '@phosphor-icons/react'
import { useI18n } from '@/hooks/useI18n'
import { duration, ease, gsap, prefersReducedMotion } from '@/lib/motion'

export type ChatHistoryItem = {
  id: string
  title: string
  updatedAt: number
}

export function ChatHistoryRail({
  collapsed,
  onCollapsedChange,
  items,
  activeId,
  onSelect,
  onNew,
}: {
  collapsed: boolean
  onCollapsedChange: (collapsed: boolean) => void
  items: ChatHistoryItem[]
  activeId: string
  onSelect: (id: string) => void
  onNew: () => void
}) {
  const { t } = useI18n()
  const railRef = useRef<HTMLElement>(null)

  useEffect(() => {
    const el = railRef.current
    if (!el || prefersReducedMotion()) return
    if (collapsed) return
    gsap.fromTo(
      el,
      { autoAlpha: 0, x: -8 },
      { autoAlpha: 1, x: 0, duration: duration.fast, ease: ease.out },
    )
  }, [collapsed])

  if (collapsed) {
    return (
      <div className="flex w-10 shrink-0 flex-col items-center gap-2 border-r border-separator/40 py-3">
        <Button
          isIconOnly
          size="sm"
          variant="ghost"
          className="size-7 min-w-7"
          aria-label={t('chatHistoryExpand')}
          onPress={() => onCollapsedChange(false)}
        >
          <SidebarSimple size={15} />
        </Button>
        <Button
          isIconOnly
          size="sm"
          variant="ghost"
          className="size-7 min-w-7"
          aria-label={t('chatHistoryNew')}
          onPress={onNew}
        >
          <Plus size={14} />
        </Button>
      </div>
    )
  }

  return (
    <aside
      ref={railRef}
      className="flex w-[13.5rem] shrink-0 flex-col border-r border-separator/40 bg-surface-secondary/30"
      aria-label={t('chatHistoryTitle')}
    >
      <div className="flex items-center gap-1 px-2 py-2.5">
        <div className="min-w-0 flex-1 truncate px-1 text-caption font-medium text-muted">
          {t('chatHistoryTitle')}
        </div>
        <Button
          isIconOnly
          size="sm"
          variant="ghost"
          className="size-7 min-w-7"
          aria-label={t('chatHistoryCollapse')}
          onPress={() => onCollapsedChange(true)}
        >
          <CaretLeft size={14} />
        </Button>
      </div>

      <div className="px-2 pb-2">
        <Button
          size="sm"
          variant="secondary"
          className="w-full justify-start gap-1.5"
          onPress={onNew}
        >
          <Plus size={13} />
          {t('chatHistoryNew')}
        </Button>
      </div>

      <div className="min-h-0 flex-1 space-y-0.5 overflow-y-auto px-1.5 pb-3">
        {!items.length ? (
          <p className="px-2 py-3 text-caption leading-5 text-muted">{t('chatHistoryEmpty')}</p>
        ) : (
          items.map((item) => {
            const active = item.id === activeId
            return (
              <button
                key={item.id}
                type="button"
                className={`w-full rounded-lg px-2.5 py-2 text-left text-caption leading-5 transition-colors ${
                  active
                    ? 'bg-surface text-foreground shadow-surface'
                    : 'text-muted hover:bg-surface/70 hover:text-foreground'
                }`}
                onClick={() => onSelect(item.id)}
              >
                <span className="line-clamp-2 font-medium">{item.title || t('chatHistoryUntitled')}</span>
              </button>
            )
          })
        )}
      </div>
    </aside>
  )
}
