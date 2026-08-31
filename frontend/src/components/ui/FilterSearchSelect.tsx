import { useEffect, useMemo, useState } from 'react'
import { Autocomplete, Label, ListBox, SearchField } from '@heroui/react'
import type { FilterSelectOption } from '@/components/ui/FilterSelect'

const ALL_VALUE = '__all__'

function matchesOption(option: FilterSelectOption, query: string) {
  const needle = query.trim().toLowerCase()
  if (!needle) return true
  return option.label.toLowerCase().includes(needle) || option.id.toLowerCase().includes(needle)
}

export function FilterSearchSelect({
  value,
  onChange,
  ariaLabel,
  options,
  allLabel,
  searchPlaceholder,
  emptyLabel,
  loadOptions,
  className,
}: {
  value: string
  onChange: (value: string) => void
  ariaLabel: string
  options: FilterSelectOption[]
  allLabel: string
  searchPlaceholder: string
  emptyLabel: string
  loadOptions?: (query: string) => Promise<FilterSelectOption[]>
  className?: string
}) {
  const [query, setQuery] = useState('')
  const [remote, setRemote] = useState<FilterSelectOption[]>([])
  const [loading, setLoading] = useState(false)

  useEffect(() => {
    if (!loadOptions) return
    let cancelled = false
    const timer = window.setTimeout(() => {
      setLoading(true)
      void loadOptions(query.trim())
        .then((items) => {
          if (!cancelled) setRemote(items)
        })
        .catch(() => {
          if (!cancelled) setRemote([])
        })
        .finally(() => {
          if (!cancelled) setLoading(false)
        })
    }, query.trim() ? 280 : 0)
    return () => {
      cancelled = true
      window.clearTimeout(timer)
    }
  }, [loadOptions, query])

  const selectedId = value || ALL_VALUE
  const items = useMemo(() => {
    const source = loadOptions ? remote : options.filter((option) => matchesOption(option, query))
    const map = new Map<string, FilterSelectOption>()
    if (!query.trim()) map.set(ALL_VALUE, { id: ALL_VALUE, label: allLabel })
    for (const option of source) {
      if (!option.id || option.id === ALL_VALUE) continue
      map.set(option.id, option)
    }
    if (value) {
      const selected = options.find((option) => option.id === value)
        || remote.find((option) => option.id === value)
        || { id: value, label: value }
      map.set(value, selected)
    }
    return [...map.values()]
  }, [allLabel, loadOptions, options, query, remote, value])

  const selected = items.find((item) => item.id === selectedId) || (value ? { id: value, label: value } : { id: ALL_VALUE, label: allLabel })

  return (
    <Autocomplete
      className={['w-fit', className].filter(Boolean).join(' ')}
      aria-label={ariaLabel}
      value={selectedId}
      onChange={(next) => {
        if (typeof next !== 'string') return
        onChange(next === ALL_VALUE ? '' : next)
        setQuery('')
      }}
      onClear={() => {
        onChange('')
        setQuery('')
      }}
    >
      <Autocomplete.Trigger className="min-w-44 items-center">
        <Autocomplete.Value className="min-w-0 truncate">
          {({ defaultChildren, isPlaceholder }) => {
            if (isPlaceholder) return defaultChildren
            return <span className="truncate">{selected.label}</span>
          }}
        </Autocomplete.Value>
        {value ? <Autocomplete.ClearButton /> : null}
        <Autocomplete.Indicator />
      </Autocomplete.Trigger>
      <Autocomplete.Popover className="min-w-56">
        <Autocomplete.Filter inputValue={query} onInputChange={setQuery} filter={() => true}>
          <SearchField aria-label={searchPlaceholder} className="px-2 pt-2" autoFocus>
            <SearchField.Group>
              <SearchField.SearchIcon />
              <SearchField.Input placeholder={searchPlaceholder} />
              <SearchField.ClearButton />
            </SearchField.Group>
          </SearchField>
          <ListBox>
            {items.length === 0 ? (
              <ListBox.Item id="__empty__" textValue={emptyLabel} isDisabled>
                <Label className="block truncate text-muted">{loading ? searchPlaceholder : emptyLabel}</Label>
              </ListBox.Item>
            ) : (
              items.map((option) => (
                <ListBox.Item key={option.id} id={option.id} textValue={option.label}>
                  <Label className="block truncate">{option.label}</Label>
                  {option.id !== ALL_VALUE && option.id !== option.label ? (
                    <span className="mono mt-0.5 block truncate text-[10px] text-muted">{option.id}</span>
                  ) : null}
                  <ListBox.ItemIndicator />
                </ListBox.Item>
              ))
            )}
          </ListBox>
        </Autocomplete.Filter>
      </Autocomplete.Popover>
    </Autocomplete>
  )
}
