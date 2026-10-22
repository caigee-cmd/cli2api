import { Area, AreaChart, ResponsiveContainer, YAxis } from 'recharts'
import type { LiveSample } from '@/hooks/useLive'

/** Bare ~60s concurrency trace; no axes, no animation. */
export function LiveSparkline({ samples, capacity, label }: { samples: LiveSample[]; capacity: number; label: string }) {
  const data = samples.length ? samples : [{ at: 0, value: 0 }]
  const peak = Math.max(1, capacity, ...data.map((s) => s.value))
  return (
    <div className="h-10 w-full" role="img" aria-label={label}>
      <ResponsiveContainer width="100%" height="100%">
        <AreaChart data={data} margin={{ top: 2, right: 0, bottom: 0, left: 0 }}>
          <YAxis hide domain={[0, peak]} />
          <Area
            type="monotone"
            dataKey="value"
            stroke="var(--foreground)"
            strokeOpacity={0.7}
            strokeWidth={1.25}
            fill="var(--foreground)"
            fillOpacity={0.08}
            isAnimationActive={false}
            dot={false}
          />
        </AreaChart>
      </ResponsiveContainer>
    </div>
  )
}
