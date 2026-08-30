import type { ReactNode } from 'react'
import { Description, Label, Radio, RadioGroup } from '@heroui/react'

type Option<T extends string> = {
  value: T
  label: string
  hint?: string
  icon?: ReactNode
  disabled?: boolean
}

type Props<T extends string> = {
  options: Array<Option<T>>
  value: T
  onChange: (value: T) => void
  ariaLabel: string
  columns?: 1 | 2 | 3
}

const columnClass = {
  1: 'grid-cols-1',
  2: 'grid-cols-1 sm:grid-cols-2',
  3: 'grid-cols-1 sm:grid-cols-2 lg:grid-cols-3',
} as const

export function OptionTiles<T extends string>({
  options,
  value,
  onChange,
  ariaLabel,
  columns = 2,
}: Props<T>) {
  return (
    <RadioGroup
      aria-label={ariaLabel}
      value={value}
      onChange={(next) => {
        if (typeof next === 'string' && next) onChange(next as T)
      }}
      className={`grid gap-2 ${columnClass[columns]}`}
    >
      {options.map((option) => (
        <Radio
          key={option.value}
          value={option.value}
          isDisabled={option.disabled}
          className="rounded-xl border border-border bg-surface-secondary p-3 data-selected:border-accent data-selected:bg-accent-soft"
        >
          <Radio.Content className="w-full items-start">
            {option.icon ? <span className="mt-0.5 shrink-0">{option.icon}</span> : null}
            <Radio.Control>
              <Radio.Indicator />
            </Radio.Control>
            <div className="min-w-0 flex-1">
              <Label className="block truncate">{option.label}</Label>
              {option.hint ? <Description className="mt-0.5">{option.hint}</Description> : null}
            </div>
          </Radio.Content>
        </Radio>
      ))}
    </RadioGroup>
  )
}
