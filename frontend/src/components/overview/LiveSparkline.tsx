import type { LiveSample } from '@/hooks/useLive'

/** Bare ~60s concurrency trace; no axes, no animation, no chart library. */
export function LiveSparkline({ samples, capacity, label }: { samples: LiveSample[]; capacity: number; label: string }) {
  const width = 120
  const height = 24
  const data = samples.length ? samples : [{ at: 0, value: 0 }]
  const peak = Math.max(1, capacity, ...data.map((sample) => sample.value))
  const points = data.map((sample, index) => {
    const x = data.length === 1 ? 0 : (index / (data.length - 1)) * width
    const y = height - 1 - (sample.value / peak) * (height - 2)
    return `${x.toFixed(2)},${y.toFixed(2)}`
  })
  const line = `M${points.join(' L')}`
  const area = `M0 ${height} L${points.join(' L')} L${width} ${height} Z`

  return (
    <svg
      viewBox={`0 0 ${width} ${height}`}
      className="h-6 w-[120px] shrink-0"
      role="img"
      aria-label={label}
      preserveAspectRatio="none"
    >
      <path d={area} fill="var(--foreground)" fillOpacity={0.08} />
      <path
        d={line}
        fill="none"
        stroke="var(--foreground)"
        strokeOpacity={0.7}
        strokeWidth={1.25}
        vectorEffect="non-scaling-stroke"
      />
    </svg>
  )
}
