import { useMemo, useState } from 'react'
import { Button, Checkbox, SearchField } from '@heroui/react'
import { CheckSquare, Square, X } from '@phosphor-icons/react'

type Translate = (key: string, vars?: Record<string, string | number>) => string

type Props = {
  /** Model IDs in the account catalog; null while loading. */
  catalog: string[] | null
  selected: string[]
  onChange: (next: string[]) => void
  isDisabled?: boolean
  t: Translate
}

/**
 * Searchable multi-select for an account's enabled models. Selected models are
 * listed first, but the order is only recomputed when the catalog loads or the
 * search changes, so rows never jump while being clicked.
 */
export function ModelPicker({ catalog, selected, onChange, isDisabled, t }: Props) {
  const [query, setQuery] = useState('')
  const [snapshot, setSnapshot] = useState<string[]>(selected)
  const selectedSet = useMemo(() => new Set(selected), [selected])

  const visible = useMemo(() => {
    const pinned = new Set(snapshot)
    const q = query.trim().toLowerCase()
    const items = (catalog || []).filter((id) => !q || id.toLowerCase().includes(q))
    return [...items.filter((id) => pinned.has(id)), ...items.filter((id) => !pinned.has(id))]
  }, [catalog, query, snapshot])

  const offline = useMemo(() => {
    if (!catalog) return []
    const known = new Set(catalog)
    return selected.filter((id) => !known.has(id))
  }, [catalog, selected])

  const total = catalog?.length ?? 0
  const selectedInCatalog = selected.filter((id) => !offline.includes(id)).length

  function updateQuery(next: string) {
    setQuery(next)
    setSnapshot(selected)
  }
  function toggle(id: string, on: boolean) {
    onChange(on ? [...selected, id] : selected.filter((item) => item !== id))
  }
  function selectAllVisible() {
    const next = new Set(selected)
    visible.forEach((id) => next.add(id))
    onChange([...next])
  }
  function clearVisible() {
    const hide = new Set(visible)
    onChange(selected.filter((id) => !hide.has(id)))
  }

  return (
    <div className="space-y-2">
      <div className="flex flex-wrap items-center gap-2">
        <SearchField aria-label={t('modelPickerSearch')} value={query} onChange={updateQuery} isDisabled={isDisabled} className="min-w-0 flex-1 basis-48">
          <SearchField.Group>
            <SearchField.SearchIcon />
            <SearchField.Input placeholder={t('modelPickerSearch')} />
            <SearchField.ClearButton />
          </SearchField.Group>
        </SearchField>
        <span className="mono shrink-0 text-caption text-muted">{t('modelPickerCount', { selected: selectedInCatalog, total })}</span>
        <div className="flex shrink-0 items-center gap-1">
          <Button size="sm" variant="ghost" isDisabled={isDisabled || visible.length === 0} onPress={selectAllVisible}>
            <CheckSquare size={14} />{t('modelPickerSelectAll')}
          </Button>
          <Button size="sm" variant="ghost" isDisabled={isDisabled || selected.length === 0} onPress={clearVisible}>
            <Square size={14} />{t('modelPickerClear')}
          </Button>
        </div>
      </div>

      <div className="max-h-[300px] overflow-y-auto overscroll-contain rounded-lg bg-surface-secondary p-1">
        {catalog === null ? (
          <p className="px-2.5 py-2 text-caption text-muted">{t('refreshing')}</p>
        ) : null}
        {offline.map((id) => (
          <div key={`offline-${id}`} className="flex items-center gap-2.5 rounded-md px-2.5 py-1.5">
            <span className="mono min-w-0 flex-1 truncate text-body text-muted line-through">{id}</span>
            <span className="shrink-0 text-caption text-warning">{t('modelPickerOffline')}</span>
            <Button isIconOnly size="sm" variant="ghost" isDisabled={isDisabled} aria-label={t('modelPickerRemove', { model: id })} onPress={() => toggle(id, false)}>
              <X size={14} />
            </Button>
          </div>
        ))}
        {visible.map((id) => (
          <Checkbox
            key={id}
            isSelected={selectedSet.has(id)}
            isDisabled={isDisabled}
            onChange={(on) => toggle(id, on)}
            className="flex w-full cursor-pointer rounded-md px-2.5 py-1.5 transition-colors hover:bg-default"
          >
            <Checkbox.Content className="flex min-w-0 flex-1 items-center gap-2.5">
              <Checkbox.Control><Checkbox.Indicator /></Checkbox.Control>
              <span className="mono min-w-0 truncate text-body text-foreground">{id}</span>
            </Checkbox.Content>
          </Checkbox>
        ))}
        {catalog !== null && visible.length === 0 ? (
          <p className="px-2.5 py-2 text-caption text-muted">{total === 0 ? t('noAccountModels') : t('modelPickerNoMatch')}</p>
        ) : null}
      </div>
    </div>
  )
}
