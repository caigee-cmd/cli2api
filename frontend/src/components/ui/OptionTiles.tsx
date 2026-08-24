import type { ReactNode } from 'react'
import { Check } from '@phosphor-icons/react'

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
  compact?: boolean
}

const columnClass = {
  1: 'grid-cols-1',
  2: 'grid-cols-1 sm:grid-cols-2',
  3: 'grid-cols-1 sm:grid-cols-2 lg:grid-cols-3',
} as const

/**
 * Flat, radio-style option group used instead of dropdown selects so the
 * surface keeps a stable height and every choice stays visible.
 */
export function OptionTiles<T extends string>({ options, value, onChange, ariaLabel, columns = 2, compact = false }: Props<T>) {
  return (
    <div role="radiogroup" aria-label={ariaLabel} className={`grid gap-2 ${columnClass[columns]}`}>
      {options.map((option) => {
        const selected = option.value === value
        return (
          <button
            key={option.value}
            type="button"
            role="radio"
            aria-checked={selected}
            disabled={option.disabled}
            onClick={() => onChange(option.value)}
            className={[
              'group relative flex min-w-0 items-start gap-2.5 rounded-lg border text-left transition-all duration-200 ease-[cubic-bezier(0.16,1,0.3,1)]',
              compact ? 'px-3 py-2.5' : 'px-3.5 py-3',
              'active:scale-[0.985]',
              selected
                ? 'border-[var(--app-accent-line,var(--app-line))] bg-[var(--app-surface)] shadow-sm'
                : 'border-[var(--app-line)] bg-[var(--app-surface-muted)]/45 hover:border-[var(--app-line-strong,var(--app-line))] hover:bg-[var(--app-surface-muted)]',
              option.disabled ? 'cursor-not-allowed opacity-45' : 'cursor-pointer',
            ].join(' ')}
          >
            {option.icon ? <span className={`${option.hint ? 'mt-0.5' : ''} shrink-0 leading-none`}>{option.icon}</span> : null}
            <span className="min-w-0 flex-1">
              <span className={`block truncate text-sm font-medium ${option.hint ? '' : 'leading-5 '}${selected ? 'text-[var(--app-ink)]' : 'text-[var(--app-fg)]'}`}>{option.label}</span>
              {option.hint ? <span className="mt-0.5 block text-xs leading-5 text-[var(--app-faint)]">{option.hint}</span> : null}
            </span>
            <span
              className={[
                option.hint ? 'mt-0.5 ' : '',
                'grid size-4 shrink-0 place-items-center rounded-full border transition-all duration-200',
                selected ? 'border-[var(--app-accent,var(--app-ink))] bg-[var(--app-accent,var(--app-ink))] text-[var(--app-surface)]' : 'border-[var(--app-line-strong,var(--app-line))] text-transparent',
              ].join(' ')}
            >
              <Check size={10} weight="bold" />
            </span>
          </button>
        )
      })}
    </div>
  )
}
