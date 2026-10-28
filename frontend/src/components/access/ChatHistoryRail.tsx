import { useEffect, useRef } from 'react'
import { Button } from '@heroui/react'
import { CaretLeft, Plus, SidebarSimple } from '@phosphor-icons/react'
import { useI18n } from '@/hooks/useI18n'
import { duration, gsap, prefersReducedMotion } from '@/lib/motion'

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
      { autoAlpha: 0 },
      { autoAlpha: 1, duration: duration.fast, ease: 'power2.out' },
    )
  }, [collapsed])

  if (collapsed) {
    return (
      <div className="access-hist-rail flex w-9 shrink-0 flex-col items-center gap-1 border-r py-2">
        <Button
          isIconOnly
          size="sm"
          variant="ghost"
          className="size-8 min-w-8 rounded-[10px] text-muted"
          aria-label={t('chatHistoryExpand')}
          onPress={() => onCollapsedChange(false)}
        >
          <SidebarSimple size={15} />
        </Button>
        <Button
          isIconOnly
          size="sm"
          variant="ghost"
          className="size-8 min-w-8 rounded-[10px] text-muted"
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
      className="access-hist-rail flex w-[13rem] shrink-0 flex-col border-r"
      aria-label={t('chatHistoryTitle')}
    >
      <div className="flex items-center gap-1 px-2 py-2">
        <div className="min-w-0 flex-1 truncate px-1.5 text-caption font-medium text-muted">
          {t('chatHistoryTitle')}
        </div>
        <Button
          isIconOnly
          size="sm"
          variant="ghost"
          className="size-8 min-w-8 rounded-[10px] text-muted"
          aria-label={t('chatHistoryCollapse')}
          onPress={() => onCollapsedChange(true)}
        >
          <CaretLeft size={14} />
        </Button>
      </div>

      <div className="px-2 pb-1.5">
        <Button
          size="sm"
          variant="ghost"
          className="h-8 w-full justify-start gap-1.5 rounded-[10px] px-2.5 text-caption font-medium text-muted hover:bg-[var(--sidebar-hover)] hover:text-foreground"
          onPress={onNew}
        >
          <Plus size={13} />
          {t('chatHistoryNew')}
        </Button>
      </div>

      <div className="min-h-0 flex-1 space-y-0.5 overflow-y-auto px-1.5 pb-2.5">
        {!items.length ? (
          <p className="px-2.5 py-6 text-center text-caption leading-5 text-muted/70">{t('chatHistoryEmpty')}</p>
        ) : (
          items.map((item) => {
            const active = item.id === activeId
            return (
              <button
                key={item.id}
                type="button"
                data-active={active ? 'true' : undefined}
                className={`access-hist-item flex h-8 w-full items-center rounded-[10px] px-2.5 text-left text-caption leading-5 ${
                  active ? 'font-medium text-foreground' : 'font-normal text-muted'
                }`}
                onClick={() => onSelect(item.id)}
              >
                <span className="line-clamp-1 tracking-tight">{item.title || t('chatHistoryUntitled')}</span>
              </button>
            )
          })
        )}
      </div>
    </aside>
  )
}
