import { useEffect, useMemo, useRef, useState } from 'react'
import {
  Area,
  AreaChart,
  ReferenceArea,
  ReferenceLine,
  ResponsiveContainer,
  XAxis,
  YAxis,
} from 'recharts'
import { motionDuration, prefersReducedMotion } from '@/lib/motion'

export function InflightDepthChart({
  series,
  bands,
  backpressure,
  label,
}: {
  series: number[]
  bands: Array<{ from: number; until: number }>
  backpressure: number
  label: string
}) {
  // Continuous glide: keep a stable x domain (0..n-1) while values scroll;
  // ease Y peak so scale changes don’t jump. Tick cadence ~260ms → linear ~560ms morph.
  const rawPeak = Math.max(backpressure, ...series, 1)
  const peakRef = useRef(rawPeak)
  const [peak, setPeak] = useState(rawPeak)
  useEffect(() => {
    if (prefersReducedMotion()) {
      peakRef.current = rawPeak
      setPeak(rawPeak)
      return
    }
    const from = peakRef.current
    const to = rawPeak
    if (Math.abs(to - from) < 0.01) return
    let raf = 0
    const t0 = performance.now()
    const dur = motionDuration(0.56) * 1000
    const tick = (now: number) => {
      const t = Math.min(1, (now - t0) / dur)
      const e = 1 - Math.pow(1 - t, 3)
      const v = from + (to - from) * e
      peakRef.current = v
      setPeak(v)
      if (t < 1) raf = requestAnimationFrame(tick)
    }
    raf = requestAnimationFrame(tick)
    return () => cancelAnimationFrame(raf)
  }, [rawPeak])

  const data = useMemo(
    () => series.map((v, i) => ({ i, v, bp: backpressure })),
    [series, backpressure],
  )
  const bandRows = useMemo(
    () =>
      bands.map((band, idx) => {
        const n = Math.max(1, series.length - 1)
        return {
          key: idx,
          x1: band.from * n,
          x2: Math.max(band.from * n + 0.35, band.until * n),
        }
      }),
    [bands, series.length],
  )
  const yMax = Math.max(peak, backpressure, 1)

  return (
    <div className="h-11 w-full overflow-hidden" role="img" aria-label={label} data-depth-chart>
      <ResponsiveContainer width="100%" height="100%">
        <AreaChart data={data} margin={{ top: 4, right: 2, bottom: 2, left: 2 }}>
          <YAxis hide domain={[0, yMax]} allowDataOverflow />
          <XAxis hide dataKey="i" type="number" domain={[0, Math.max(0, series.length - 1)]} />
          {bandRows.map((b) => (
            <ReferenceArea
              key={b.key}
              x1={b.x1}
              x2={b.x2}
              y1={0}
              y2={yMax}
              fill="var(--il-warn, var(--warning))"
              fillOpacity={0.14}
              ifOverflow="extendDomain"
            />
          ))}
          <ReferenceLine
            y={backpressure}
            stroke="var(--il-faint, var(--foreground))"
            strokeOpacity={0.35}
            strokeDasharray="3 3"
            ifOverflow="extendDomain"
          />
          <defs>
            <linearGradient id="il-depth-fill" x1="0" y1="0" x2="0" y2="1">
              <stop offset="0%" stopColor="var(--il-depth, var(--chart-1))" stopOpacity={0.28} />
              <stop offset="100%" stopColor="var(--il-depth, var(--chart-1))" stopOpacity={0.04} />
            </linearGradient>
          </defs>
          <Area
            type="monotone"
            dataKey="v"
            stroke="var(--il-depth, var(--chart-1))"
            strokeOpacity={0.95}
            strokeWidth={1.85}
            fill="url(#il-depth-fill)"
            fillOpacity={1}
            isAnimationActive={!prefersReducedMotion()}
            animationDuration={Math.round(motionDuration(0.56) * 1000)}
            animationEasing="linear"
            dot={false}
            activeDot={false}
          />
        </AreaChart>
      </ResponsiveContainer>
    </div>
  )
}
