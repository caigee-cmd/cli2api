import { Component, type ErrorInfo, type ReactNode } from 'react'
import { Button } from '@heroui/react'
import { WarningCircle } from '@phosphor-icons/react'
import { useI18n } from '@/hooks/useI18n'

type BoundaryProps = {
  title: string
  description: string
  detailsLabel: string
  reloadLabel: string
  children: ReactNode
}

type BoundaryState = {
  error: Error | null
}

class Boundary extends Component<BoundaryProps, BoundaryState> {
  state: BoundaryState = { error: null }

  static getDerivedStateFromError(error: unknown): BoundaryState {
    return { error: error instanceof Error ? error : new Error(String(error)) }
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    // Keep the full stack in the browser console for a bug report; the
    // fallback only shows the short message.
    console.error('cli2api render error', error, info.componentStack)
  }

  render() {
    if (!this.state.error) return this.props.children
    return (
      <div className="grid min-h-dvh place-items-center bg-background px-4 py-10 text-foreground">
        <div role="alert" className="card w-full max-w-xl p-6">
          <div className="flex items-start gap-3.5">
            <span className="grid size-10 shrink-0 place-items-center rounded-full bg-danger/10 text-danger">
              <WarningCircle size={20} />
            </span>
            <div className="min-w-0">
              <h1 className="text-title font-semibold">{this.props.title}</h1>
              <p className="mt-1.5 text-body leading-6 text-muted">{this.props.description}</p>
            </div>
          </div>
          <details className="mt-5">
            <summary className="cursor-pointer select-none text-caption text-muted">{this.props.detailsLabel}</summary>
            <pre className="mt-2 max-h-44 overflow-auto whitespace-pre-wrap break-words rounded-lg bg-surface-secondary p-3 font-mono text-caption text-muted">
              {this.state.error.message}
            </pre>
          </details>
          <div className="mt-5 flex justify-end">
            <Button variant="secondary" onPress={() => window.location.reload()}>
              {this.props.reloadLabel}
            </Button>
          </div>
        </div>
      </div>
    )
  }
}

/** Last line of defence: keep a render exception from blanking the console. */
export function AppErrorBoundary({ children }: { children: ReactNode }) {
  const { t } = useI18n()
  return (
    <Boundary
      title={t('appErrorTitle')}
      description={t('appErrorHint')}
      detailsLabel={t('appErrorDetails')}
      reloadLabel={t('appErrorReload')}
    >
      {children}
    </Boundary>
  )
}
