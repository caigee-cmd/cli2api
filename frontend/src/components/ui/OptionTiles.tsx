import type { ReactNode } from 'react'
import { Button, Label, Radio, RadioGroup, Tooltip } from '@heroui/react'
import { Info } from '@phosphor-icons/react'

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
  className?: string
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
  className = '',
}: Props<T>) {
  return (
    <RadioGroup
      aria-label={ariaLabel}
      value={value}
      onChange={(next) => {
        if (typeof next === 'string' && next) onChange(next as T)
      }}
      className={`grid gap-2 ${columnClass[columns]} ${className}`.trim()}
    >
      {options.map((option) => (
        <div
          key={option.value}
          data-selected={option.value === value || undefined}
          className="flex items-center gap-1 rounded-xl border border-border bg-surface-secondary p-1.5 data-selected:border-accent data-selected:bg-accent-soft"
        >
          <Radio
            value={option.value}
            isDisabled={option.disabled}
            className="my-auto min-h-0 min-w-0 flex-1 justify-center border-0 bg-transparent px-1.5 py-0"
          >
            <Radio.Content className="w-full items-center gap-2.5">
              {option.icon ? <span className="grid shrink-0 place-items-center self-center">{option.icon}</span> : null}
              <Radio.Control className="self-center">
                <Radio.Indicator />
              </Radio.Control>
              <div className="min-w-0 flex-1 self-center">
                <Label className="block truncate leading-5">{option.label}</Label>
              </div>
            </Radio.Content>
          </Radio>
          {option.hint ? (
            <Tooltip>
              <Tooltip.Trigger>
                <Button
                  isIconOnly
                  size="sm"
                  variant="ghost"
                  aria-label={option.hint}
                  className="shrink-0 text-muted"
                >
                  <Info size={14} />
                </Button>
              </Tooltip.Trigger>
              <Tooltip.Content>
                <p className="max-w-xs text-xs leading-5">{option.hint}</p>
              </Tooltip.Content>
            </Tooltip>
          ) : null}
        </div>
      ))}
    </RadioGroup>
  )
}
