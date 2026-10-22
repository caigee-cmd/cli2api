import type { ReactNode } from 'react'
import { Label } from '@heroui/react'

type Props = {
  label: string
  /** Optional helper text rendered under the control, aligned with the control column. */
  hint?: ReactNode
  /** Label column width. Defaults to 7rem, wide enough for CJK labels + trailing colon. */
  labelWidthClass?: string
  htmlFor?: string
  /** 'start' top-aligns the label for tall, multi-line controls. */
  align?: 'center' | 'start'
  children: ReactNode
}

const defaultLabelWidthClass = 'w-28'

/**
 * One label + one control per row: label fixed on the left, control fills
 * the rest. Keeps every form in the console (account create/edit, system
 * settings, key create) on the same alignment grid.
 */
export function FormRow({ label, hint, labelWidthClass = defaultLabelWidthClass, htmlFor, align = 'center', children }: Props) {
  return (
    <div>
      <div className={`flex gap-3 ${align === 'start' ? 'items-start' : 'items-center'}`}>
        <Label htmlFor={htmlFor} className={`${labelWidthClass} shrink-0 ${align === 'start' ? 'pt-2' : ''} text-body font-medium text-foreground`}>{label}</Label>
        <div className="min-w-0 flex-1 [&_[data-slot=input]]:w-full [&_[data-slot=textarea]]:w-full">{children}</div>
      </div>
      {hint ? <p className="mt-1.5 pl-[7.75rem] text-caption leading-5 text-muted">{hint}</p> : null}
    </div>
  )
}
