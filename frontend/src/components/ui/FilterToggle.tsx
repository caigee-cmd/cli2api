import type { ReactNode } from 'react'
import { ToggleButton, ToggleButtonGroup } from '@heroui/react'

export type FilterToggleOption = {
  id: string
  label: string
  icon?: ReactNode
}

export function FilterToggle({
  value,
  onChange,
  options,
  ariaLabel,
  className,
}: {
  value: string
  onChange: (value: string) => void
  options: FilterToggleOption[]
  ariaLabel: string
  className?: string
}) {
  return (
    <ToggleButtonGroup
      aria-label={ariaLabel}
      className={className}
      size="sm"
      selectionMode="single"
      disallowEmptySelection
      selectedKeys={new Set([value])}
      onSelectionChange={(keys) => {
        const next = [...keys][0]
        if (typeof next === 'string' && next !== value) onChange(next)
      }}
    >
      {options.map((option) => (
        <ToggleButton key={option.id} id={option.id}>
          {option.icon}
          {option.label}
        </ToggleButton>
      ))}
    </ToggleButtonGroup>
  )
}
