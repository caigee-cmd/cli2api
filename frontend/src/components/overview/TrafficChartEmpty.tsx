export function TrafficChartEmpty({ emptyLabel }: { emptyLabel: string }) {
  return (
    <div className="grid min-h-56 place-items-center rounded-lg bg-surface-secondary px-4 text-body text-muted">
      {emptyLabel}
    </div>
  )
}
