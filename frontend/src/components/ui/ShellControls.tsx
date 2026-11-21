import type { ButtonHTMLAttributes, ReactNode } from 'react'
import { Spinner } from '@phosphor-icons/react'

/**
 * Shell controls are deliberately dependency-free. The app frame renders on
 * every page, so importing HeroUI here made its shared runtime part of the
 * entry chunk even though the shell only needs a button, chip and tooltip.
 */

type IconButtonProps = Omit<ButtonHTMLAttributes<HTMLButtonElement>, 'aria-label' | 'title'> & {
  label: string
  pending?: boolean
}

export function ShellIconButton({
  label,
  pending = false,
  disabled = false,
  className = '',
  children,
  ...rest
}: IconButtonProps) {
  return (
    <button
      type="button"
      aria-label={label}
      disabled={disabled || pending}
      className={`grid size-8 shrink-0 place-items-center rounded-lg text-muted transition-colors hover:bg-surface-secondary hover:text-foreground disabled:cursor-not-allowed disabled:opacity-50 ${className}`}
      {...rest}
    >
      {pending ? <Spinner size={14} className="animate-spin motion-reduce:animate-none" /> : children}
    </button>
  )
}

type TooltipProps = {
  label: string
  children: ReactNode
  side?: 'top' | 'bottom'
}

export function ShellTooltip({ label, children, side = 'bottom' }: TooltipProps) {
  return (
    <span className="group/tip relative inline-flex">
      {children}
      <span
        role="tooltip"
        className={`pointer-events-none absolute left-1/2 z-[70] -translate-x-1/2 whitespace-nowrap rounded-lg bg-overlay px-2 py-1 text-caption font-medium text-foreground opacity-0 shadow-overlay transition-opacity duration-150 group-hover/tip:opacity-100 group-focus-within/tip:opacity-100 ${side === 'top' ? 'bottom-full mb-1.5' : 'top-full mt-1.5'}`}
      >
        {label}
      </span>
    </span>
  )
}

type ChipProps = {
  children: ReactNode
  tone?: 'neutral' | 'success' | 'warning'
}

export function ShellChip({ children, tone = 'neutral' }: ChipProps) {
  const toneClass = tone === 'success'
    ? 'text-success'
    : tone === 'warning'
      ? 'text-warning'
      : 'text-muted'
  return (
    <span className={`inline-flex h-5 shrink-0 items-center rounded-full bg-surface-secondary px-2 text-caption font-medium ${toneClass}`}>
      {children}
    </span>
  )
}

export function ShellSeparator({ className = '' }: { className?: string }) {
  return <div role="separator" className={`h-px w-full bg-separator ${className}`} />
}
