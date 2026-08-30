import type { ReactNode } from 'react'
import { EmptyState } from '@heroui/react'

export function EmptyPanel({
  title,
  hint,
  action,
  icon,
  className,
}: {
  title: string
  hint?: string
  action?: ReactNode
  icon?: ReactNode
  className?: string
}) {
  return (
    <div className={`grid min-h-56 place-items-center px-6 py-12 text-center ${className || ''}`}>
      <div className="max-w-sm">
        {icon ? <div className="mb-3 flex justify-center text-muted">{icon}</div> : null}
        <EmptyState className="p-0 text-sm font-medium text-foreground">{title}</EmptyState>
        {hint ? <p className="mt-1 text-xs leading-5 text-muted">{hint}</p> : null}
        {action ? <div className="mt-5 flex justify-center">{action}</div> : null}
      </div>
    </div>
  )
}
