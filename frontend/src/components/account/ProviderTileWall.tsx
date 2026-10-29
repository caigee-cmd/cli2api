import { useMemo, useState, type ReactNode } from 'react'
import { Skeleton } from '@heroui/react'
import { ProviderMark } from '@/components/ProviderMark'
import { SearchBar } from '@/components/ui/SearchBar'
import { useI18n } from '@/hooks/useI18n'

export type ProviderTileOption = {
  id: string
  provider: string
  label: string
  /** Short line under the title (region / endpoint style). */
  subtitle?: string
  /** Full description for the native tooltip. */
  hint?: string
}

type SectionId = 'subscriptions' | 'experimental'

type Section = {
  id: SectionId
  titleKey: string
  subtitleKey: string
  tiles: ProviderTileOption[]
}

/** Stable subscription families shown first in the Magpie-style picker. */
const SUBSCRIPTION_IDS = new Set([
  'qoder-global',
  'qoder-cn',
  'workbuddy-global',
  'workbuddy-cn',
  'trae-cn',
])

const SUBSCRIPTION_ORDER = [
  'qoder-global',
  'qoder-cn',
  'workbuddy-global',
  'workbuddy-cn',
  'trae-cn',
]

function sortByKnownOrder(tiles: ProviderTileOption[], order: string[]) {
  const rank = new Map(order.map((id, index) => [id, index]))
  return [...tiles].sort((a, b) => {
    const left = rank.get(a.id) ?? Number.MAX_SAFE_INTEGER
    const right = rank.get(b.id) ?? Number.MAX_SAFE_INTEGER
    if (left !== right) return left - right
    return a.label.localeCompare(b.label)
  })
}

function matchesQuery(tile: ProviderTileOption, query: string) {
  if (!query) return true
  const haystack = `${tile.label} ${tile.subtitle || ''} ${tile.hint || ''} ${tile.provider} ${tile.id}`.toLowerCase()
  return haystack.includes(query)
}

export function ProviderTileWallSkeleton({ ariaLabel }: { ariaLabel: string }) {
  return (
    <div className="space-y-8" aria-busy="true" aria-label={ariaLabel}>
      {Array.from({ length: 2 }, (_, section) => (
        <div key={section} className="space-y-3">
          <div className="space-y-1.5">
            <Skeleton className="h-3 w-24 rounded-lg" />
            <Skeleton className="h-3 w-40 rounded-lg" />
          </div>
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            {Array.from({ length: section === 0 ? 5 : 6 }, (_, index) => (
              <div key={index} className="card flex flex-row items-center gap-3.5 rounded-xl bg-surface px-4 py-3.5">
                <Skeleton className="size-10 shrink-0 rounded-lg" />
                <div className="min-w-0 flex-1 space-y-1.5">
                  <Skeleton className="h-3.5 w-28 rounded-lg" />
                  <Skeleton className="h-3 w-36 rounded-lg" />
                </div>
              </div>
            ))}
          </div>
        </div>
      ))}
    </div>
  )
}

function ProviderTile({
  tile,
  disabled,
  onSelect,
}: {
  tile: ProviderTileOption
  disabled?: boolean
  onSelect: (id: string) => void
}) {
  return (
    <button
      type="button"
      disabled={disabled}
      title={tile.hint || tile.subtitle || tile.label}
      onClick={() => onSelect(tile.id)}
      className="card flex min-w-0 flex-row items-center gap-3 rounded-xl bg-surface px-3.5 py-3 text-left transition-[transform,opacity,background-color] duration-200 ease-out hover:bg-surface-secondary active:translate-y-px active:scale-[0.98] motion-reduce:transition-none motion-reduce:active:translate-y-0 motion-reduce:active:scale-100 disabled:cursor-not-allowed disabled:opacity-50 disabled:active:translate-y-0 disabled:active:scale-100"
    >
      <span className="grid size-9 shrink-0 place-items-center rounded-lg text-foreground [&_svg]:size-[18px]">
        <ProviderMark provider={tile.provider} size={18} />
      </span>
      <span className="min-w-0 flex-1">
        <span className="block truncate text-body font-medium leading-5 text-foreground">{tile.label}</span>
        {tile.subtitle ? (
          <span className="mt-0.5 block truncate text-caption leading-4 text-muted">{tile.subtitle}</span>
        ) : null}
      </span>
    </button>
  )
}

function SectionBlock({
  title,
  subtitle,
  tiles,
  disabled,
  onSelect,
}: {
  title: string
  subtitle: string
  tiles: ProviderTileOption[]
  disabled?: boolean
  onSelect: (id: string) => void
}) {
  if (!tiles.length) return null
  return (
    <section className="space-y-3.5">
      <div className="space-y-0.5">
        <h3 className="text-[10px] font-semibold tracking-[0.12em] uppercase text-muted">{title}</h3>
        {subtitle ? <p className="text-caption leading-5 text-muted">{subtitle}</p> : null}
      </div>
      <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
        {tiles.map((tile) => (
          <ProviderTile key={tile.id} tile={tile} disabled={disabled} onSelect={onSelect} />
        ))}
      </div>
    </section>
  )
}

export function ProviderTileWall({
  options,
  onSelect,
  disabled,
  empty,
  ariaLabel,
}: {
  options: ProviderTileOption[]
  onSelect: (id: string) => void
  disabled?: boolean
  empty?: ReactNode
  ariaLabel: string
}) {
  const { t } = useI18n()
  const [query, setQuery] = useState('')
  const normalized = query.trim().toLowerCase()

  const sections = useMemo(() => {
    const subscriptions = sortByKnownOrder(
      options.filter((tile) => SUBSCRIPTION_IDS.has(tile.id)),
      SUBSCRIPTION_ORDER,
    )
    const experimental = options
      .filter((tile) => !SUBSCRIPTION_IDS.has(tile.id))
      .sort((a, b) => a.label.localeCompare(b.label))

    const built: Section[] = [
      {
        id: 'subscriptions',
        titleKey: 'providerGroupSubscriptions',
        subtitleKey: 'providerGroupSubscriptionsHint',
        tiles: subscriptions.filter((tile) => matchesQuery(tile, normalized)),
      },
      {
        id: 'experimental',
        titleKey: 'providerGroupExperimental',
        subtitleKey: 'providerGroupExperimentalHint',
        tiles: experimental.filter((tile) => matchesQuery(tile, normalized)),
      },
    ]
    return built
  }, [normalized, options])

  const visibleCount = sections.reduce((sum, section) => sum + section.tiles.length, 0)

  return (
    <div className="space-y-8" role="listbox" aria-label={ariaLabel}>
      <SearchBar
        value={query}
        onChange={setQuery}
        placeholder={t('providerFindPlaceholder')}
        ariaLabel={t('providerFindPlaceholder')}
        className="max-w-sm"
      />
      {visibleCount === 0 ? (
        empty || <p className="rounded-lg bg-surface-secondary/45 px-3.5 py-3 text-caption leading-5 text-muted">{t('providerFindEmpty')}</p>
      ) : (
        sections.map((section) => (
          <SectionBlock
            key={section.id}
            title={t(section.titleKey)}
            subtitle={t(section.subtitleKey)}
            tiles={section.tiles}
            disabled={disabled}
            onSelect={onSelect}
          />
        ))
      )}
    </div>
  )
}
