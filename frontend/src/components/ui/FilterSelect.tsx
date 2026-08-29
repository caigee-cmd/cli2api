import { Label, ListBox, Select } from '@heroui/react'

const ALL_VALUE = '__all__'

export type FilterSelectOption = {
  id: string
  label: string
}

export function FilterSelect({
  value,
  onChange,
  ariaLabel,
  options,
  className,
}: {
  value: string
  onChange: (value: string) => void
  ariaLabel: string
  options: FilterSelectOption[]
  className?: string
}) {
  const items = options.map((option) => ({
    id: option.id || ALL_VALUE,
    label: option.label,
  }))
  const selectedId = value || ALL_VALUE
  const selected = items.find((item) => item.id === selectedId)

  return (
    <Select
      className={['w-fit', className].filter(Boolean).join(' ')}
      aria-label={ariaLabel}
      value={selectedId}
      onChange={(next) => {
        if (typeof next !== 'string') return
        onChange(next === ALL_VALUE ? '' : next)
      }}
    >
      <Select.Trigger className="h-8 min-h-8 min-w-36 items-center">
        <Select.Value className="min-w-0 truncate">
          {({ defaultChildren, isPlaceholder }) => {
            if (isPlaceholder || !selected) return defaultChildren
            return <span className="truncate">{selected.label}</span>
          }}
        </Select.Value>
        <Select.Indicator />
      </Select.Trigger>
      <Select.Popover className="max-h-72 min-w-36 rounded-lg">
        <ListBox>
          {items.map((option) => (
            <ListBox.Item key={option.id} id={option.id} textValue={option.label} className="rounded-lg">
              <Label className="block truncate">{option.label}</Label>
              <ListBox.ItemIndicator />
            </ListBox.Item>
          ))}
        </ListBox>
      </Select.Popover>
    </Select>
  )
}
