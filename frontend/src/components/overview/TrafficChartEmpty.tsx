export function TrafficChartEmpty({ emptyLabel }: { emptyLabel: string }) {
  return (
    <div className="grid min-h-72 place-items-center rounded-lg border border-dashed border-border px-4 text-sm text-muted">
      {emptyLabel}
    </div>
  )
}
