import { useLayoutEffect, useRef, type ReactNode } from 'react'
import gsap from 'gsap'

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
  3: 'grid-cols-1 sm:grid-cols-3',
} as const

export function OptionTiles<T extends string>({
  options,
  value,
  onChange,
  ariaLabel,
  columns = 2,
  className = '',
}: Props<T>) {
  const root = useRef<HTMLDivElement>(null)

  useLayoutEffect(() => {
    const group = root.current
    if (!group) return
    const context = gsap.context(() => {
      const media = gsap.matchMedia()
      media.add('(prefers-reduced-motion: reduce)', () => {
        gsap.set('[data-option-tile]', { autoAlpha: 1, y: 0 })
      })
      media.add('(prefers-reduced-motion: no-preference)', () => {
        gsap.fromTo(
          '[data-option-tile]',
          { autoAlpha: 0, y: 8 },
          { autoAlpha: 1, y: 0, duration: 0.32, stagger: 0.035, ease: 'power3.out', overwrite: true },
        )
      })
    }, group)
    return () => context.revert()
  }, [options.length])

  return (
    <div
      ref={root}
      role="radiogroup"
      aria-label={ariaLabel}
      className={`grid gap-3 ${columnClass[columns]} ${className}`.trim()}
    >
      {options.map((option) => {
        const selected = option.value === value
        return (
          <button
            key={option.value}
            type="button"
            role="radio"
            aria-checked={selected}
            data-option-tile=""
            data-selected={selected || undefined}
            disabled={option.disabled}
            title={option.hint}
            onClick={() => onChange(option.value)}
            className="flex h-16 min-w-0 items-center gap-3 rounded-2xl border border-border bg-surface px-4 text-left transition-colors hover:bg-surface-secondary disabled:cursor-not-allowed disabled:opacity-50 data-selected:border-accent data-selected:bg-accent-soft"
          >
            {option.icon ? (
              <span className="grid size-8 shrink-0 place-items-center rounded-lg bg-surface-secondary text-foreground [&_svg]:size-[18px]">
                {option.icon}
              </span>
            ) : null}
            <span className="min-w-0 flex-1 truncate text-sm font-medium leading-5 text-foreground">{option.label}</span>
          </button>
        )
      })}
    </div>
  )
}
