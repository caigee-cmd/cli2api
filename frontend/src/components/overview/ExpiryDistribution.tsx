import { useMemo, useState } from 'react'
import { Button, Card, Table } from '@heroui/react'
import type { AccountRow } from '@/lib/account'
import { formatQuotaAmount } from '@/lib/account'

// ExpiryDistribution renders a per-account credit-expiry view built from the
// per-package detail that /api/overview already returns (quota.packages[]).
// It answers what the flat account cards cannot:
//   1. 按自然日聚合的「积分到期分布」——每天有多少积分会失效，颜色区分账号；
//   2. 「到期提醒」——每个账号最近一批何时到期、还剩多少、日均要耗多少才能用完
//      （FEFO：上游按失效时刻先后优先扣减）。
// Everything is derived on the client from quota.packages; no extra request.

type Translate = (key: string, vars?: Record<string, string | number>) => string

type Props = {
  accounts: AccountRow[]
  t: Translate
}

const DAY_MS = 24 * 60 * 60 * 1000

// Categorical identity palette — one hue per account, stable by index so the
// same account keeps the same colour across chart, legend and table. Defined
// as --chart-N tokens in index.css so light/dark pick their own lightness.
const SERIES = 8
function seriesColor(index: number) {
  return `var(--chart-${(index % SERIES) + 1})`
}

type Segment = { id: string; name: string; color: string; remain: number }
type DayBucket = { day: string; label: string; ts: number; segments: Segment[]; total: number }

type AccountExpiry = {
  id: string
  name: string
  color: string
  remain: number
  expiredRemain: number // remain sitting on packs whose end day is already past
  soonest: number // unix sec of soonest *future* pack with remain>0; 0 = none
  soonestRemain: number // credits expiring on the soonest *day* (all packs that day)
  daysLeft: number // whole days from today (local midnight); -1 = none
  dailyBurn: number
  packs: number
}

function localDayStart(d: Date) {
  return new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime()
}

function pad(n: number) {
  return n < 10 ? `0${n}` : String(n)
}

function dayKey(ts: number) {
  const d = new Date(ts)
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`
}

export function ExpiryDistribution({ accounts, t }: Props) {
  const [expanded, setExpanded] = useState(false)

  const { buckets, rows, todayStart, maxTotal } = useMemo(() => {
    const todayStart = localDayStart(new Date())
    const colorById = new Map<string, string>()
    accounts.forEach((a, i) => colorById.set(a.id, seriesColor(i)))

    const byDay = new Map<string, DayBucket>()
    const rows: AccountExpiry[] = []

    for (const account of accounts) {
      const packages = account.quota?.packages ?? []
      const color = colorById.get(account.id) || seriesColor(0)
      const name = account.name || account.id

      // Packages that still hold credits and carry a real expiry date. Already-
      // expired packs with remain>0 go into a dedicated "expired" bucket so they
      // stay visible instead of looking like a future day on the histogram.
      const live = packages.filter((p) => (p.remain ?? 0) > 0 && (p.ends_at ?? 0) > 0)

      let soonest = 0
      let soonestDayTotal = 0 // same-account credits expiring on the soonest day
      let remain = 0
      let expiredRemain = 0
      for (const p of live) {
        const ends = (p.ends_at as number) * 1000
        const amount = p.remain ?? 0
        remain += amount
        const dayTs = localDayStart(new Date(ends))
        const expired = dayTs < todayStart
        if (expired) expiredRemain += amount
        else if (soonest === 0 || ends < soonest) soonest = ends

        const key = expired ? '__expired__' : dayKey(dayTs)
        let bucket = byDay.get(key)
        if (!bucket) {
          bucket = expired
            ? { day: key, label: '!', ts: todayStart - DAY_MS, segments: [], total: 0 }
            : (() => {
                const d = new Date(dayTs)
                return { day: key, label: `${d.getMonth() + 1}/${d.getDate()}`, ts: dayTs, segments: [], total: 0 }
              })()
          byDay.set(key, bucket)
        }
        const existing = bucket.segments.find((s) => s.id === account.id)
        if (existing) existing.remain += amount
        else bucket.segments.push({ id: account.id, name, color, remain: amount })
        bucket.total += amount
      }

      // Credits expiring on the soonest day (sum every pack that shares that
      // local day) — matches the "该日到期" bar for this account on the chart.
      if (soonest) {
        const soonestKey = dayKey(localDayStart(new Date(soonest)))
        for (const p of live) {
          if (dayKey(localDayStart(new Date((p.ends_at as number) * 1000))) === soonestKey) {
            soonestDayTotal += p.remain ?? 0
          }
        }
      }

      const daysLeft = soonest
        ? Math.max(0, Math.round((localDayStart(new Date(soonest)) - todayStart) / DAY_MS))
        : -1
      rows.push({
        id: account.id,
        name,
        color,
        remain,
        expiredRemain,
        soonest: soonest ? Math.floor(soonest / 1000) : 0,
        soonestRemain: soonestDayTotal,
        daysLeft,
        dailyBurn: soonest ? soonestDayTotal / Math.max(daysLeft, 1) : 0,
        packs: live.length,
      })
    }

    rows.sort((a, b) => {
      if (a.soonest && b.soonest) return a.soonest - b.soonest
      if (a.soonest) return -1
      if (b.soonest) return 1
      return b.remain - a.remain
    })

    const buckets = [...byDay.values()].sort((a, b) => a.ts - b.ts)
    const maxTotal = buckets.reduce((m, b) => Math.max(m, b.total), 0)
    return { buckets, rows, todayStart, maxTotal }
  }, [accounts])

  if (!rows.some((r) => r.packs > 0)) return null

  const shownBuckets = expanded ? buckets : buckets.slice(0, 8)

  return (
    <Card className="overflow-hidden p-0" data-gsap-reveal>
      <div className="flex flex-col gap-3 border-b border-separator px-4 py-4 sm:flex-row sm:flex-wrap sm:items-center sm:justify-between sm:px-5">
        <div className="min-w-0">
          <h3 className="font-semibold">{t('expiryDistTitle')}</h3>
          <p className="mt-0.5 text-xs text-muted">{t('expiryDistHint')}</p>
        </div>
        {buckets.length > 8 ? (
          <Button size="sm" variant="ghost" onPress={() => setExpanded((v) => !v)}>
            {expanded ? t('expiryDistCollapse') : t('expiryDistExpand', { n: buckets.length })}
          </Button>
        ) : null}
      </div>

      <div className="space-y-4 p-4 sm:space-y-3 sm:p-5">
        {/* 按自然日聚合的到期分布：每天一根横条，整条宽度按当日总量相对最大日
            缩放（真正的直方图），条内分段 = 账号，段色区分账号。 */}
        <div className="space-y-2.5 sm:space-y-1.5">
          {shownBuckets.map((bucket) => {
            const isToday = bucket.ts === todayStart
            const scale = maxTotal > 0 ? (bucket.total / maxTotal) * 100 : 0
            return (
              <div key={bucket.day} className="flex flex-col gap-1.5 sm:grid sm:grid-cols-[52px_minmax(0,1fr)_auto] sm:items-center sm:gap-2">
                <div className="flex items-baseline justify-between gap-3 sm:contents">
                  <span className={`mono text-caption ${bucket.day === '__expired__' ? 'font-semibold text-danger' : isToday ? 'font-semibold text-foreground' : 'text-muted'}`}>
                    {bucket.day === '__expired__' ? t('expiryDistExpired') : bucket.label}
                  </span>
                  <span className="mono text-caption text-foreground sm:hidden">{formatQuotaAmount(bucket.total)}</span>
                </div>
                <div
                  className="h-2.5 w-full rounded-full bg-surface-tertiary"
                  role="img"
                  aria-label={`${bucket.day} ${t('expiryDistDayTotal', { n: formatQuotaAmount(bucket.total) })}`}
                  title={bucket.day}
                >
                  <div className="flex h-full items-stretch overflow-hidden rounded-full" style={{ width: `${scale}%` }}>
                    {bucket.segments.map((seg) => (
                      <span
                        key={seg.id}
                        style={{ width: `${(seg.remain / bucket.total) * 100}%`, backgroundColor: seg.color }}
                        title={`${seg.name} · ${formatQuotaAmount(seg.remain)}`}
                      />
                    ))}
                  </div>
                </div>
                <span className="mono hidden text-caption text-foreground sm:inline">{formatQuotaAmount(bucket.total)}</span>
              </div>
            )
          })}
        </div>

        {/* 图例：账号 → 颜色 */}
        <div className="flex flex-wrap gap-x-3 gap-y-1.5 pt-1">
          {rows
            .filter((r) => r.remain > 0)
            .map((r) => (
              <span key={r.id} className="inline-flex items-center gap-1.5 text-caption text-muted">
                <i className="size-2 rounded-[3px]" style={{ backgroundColor: r.color }} />
                <span className="max-w-[120px] truncate">{r.name}</span>
              </span>
            ))}
        </div>

        {/* 到期提醒：最近一批 / 距到期 / 该日到期 / 日均需耗 */}
        <div className="divide-y divide-separator md:hidden">
          {rows
            .filter((r) => r.packs > 0)
            .map((r) => {
              const urgent = r.daysLeft >= 0 && r.daysLeft <= 2
              const soon = r.daysLeft >= 0 && r.daysLeft <= 7
              const soonestDate = r.soonest ? new Date(r.soonest * 1000) : null
              const daysTone = r.expiredRemain > 0 && r.daysLeft < 0 ? 'font-semibold text-danger' : urgent ? 'font-semibold text-danger' : soon ? 'text-warning' : 'text-muted'
              const daysValue = r.daysLeft >= 0
                ? t('expiryDistDays', { n: r.daysLeft })
                : r.expiredRemain > 0
                  ? t('expiryDistExpired')
                  : '—'
              return (
                <article key={r.id} className="space-y-3 py-3.5">
                  <span className="inline-flex items-center gap-1.5">
                    <i className="size-2 shrink-0 rounded-[3px]" style={{ backgroundColor: r.color }} />
                    <span className="truncate text-body font-medium text-foreground">{r.name}</span>
                  </span>
                  <div className="grid grid-cols-2 gap-x-4 gap-y-3">
                    <div className="space-y-1">
                      <div className="text-caption text-muted">{t('expiryDistColSoonest')}</div>
                      <div className="mono text-body text-muted">{soonestDate ? `${soonestDate.getMonth() + 1}/${soonestDate.getDate()}` : '—'}</div>
                    </div>
                    <div className="space-y-1">
                      <div className="text-caption text-muted">{t('expiryDistColDaysLeft')}</div>
                      <div className={`mono text-body ${daysTone}`}>{daysValue}</div>
                    </div>
                    <div className="space-y-1">
                      <div className="text-caption text-muted">{t('expiryDistColBatch')}</div>
                      <div className="mono text-body text-foreground">{formatQuotaAmount(r.soonestRemain)}</div>
                    </div>
                    <div className="space-y-1">
                      <div className="text-caption text-muted">{t('expiryDistColDaily')}</div>
                      <div className="mono text-body text-foreground">{r.soonest ? formatQuotaAmount(r.dailyBurn) : '—'}</div>
                    </div>
                  </div>
                </article>
              )
            })}
        </div>

        <Table aria-label={t('expiryDistTitle')} className="hidden md:block">
          <Table.ScrollContainer className="min-w-0 overflow-x-auto">
            <Table.Content className="min-w-[32rem]">
              <Table.Header>
                <Table.Column isRowHeader>{t('expiryDistColAccount')}</Table.Column>
                <Table.Column><div className="text-center">{t('expiryDistColSoonest')}</div></Table.Column>
                <Table.Column><div className="text-center">{t('expiryDistColDaysLeft')}</div></Table.Column>
                <Table.Column><div className="text-center">{t('expiryDistColBatch')}</div></Table.Column>
                <Table.Column><div className="text-center">{t('expiryDistColDaily')}</div></Table.Column>
              </Table.Header>
              <Table.Body>
                {rows
                  .filter((r) => r.packs > 0)
                  .map((r) => {
                    const urgent = r.daysLeft >= 0 && r.daysLeft <= 2
                    const soon = r.daysLeft >= 0 && r.daysLeft <= 7
                    const soonestDate = r.soonest ? new Date(r.soonest * 1000) : null
                    return (
                      <Table.Row key={r.id}>
                        <Table.Cell>
                          <span className="inline-flex items-center gap-1.5">
                            <i className="size-2 shrink-0 rounded-[3px]" style={{ backgroundColor: r.color }} />
                            <span className="max-w-[150px] truncate text-foreground">{r.name}</span>
                          </span>
                        </Table.Cell>
                        <Table.Cell>
                          <div className="mono text-center text-muted">
                            {soonestDate ? `${soonestDate.getMonth() + 1}/${soonestDate.getDate()}` : '—'}
                          </div>
                        </Table.Cell>
                        <Table.Cell>
                          <div className={`mono text-center ${r.expiredRemain > 0 && r.daysLeft < 0 ? 'font-semibold text-danger' : urgent ? 'font-semibold text-danger' : soon ? 'text-warning' : 'text-muted'}`}>
                            {r.daysLeft >= 0
                              ? t('expiryDistDays', { n: r.daysLeft })
                              : r.expiredRemain > 0
                                ? t('expiryDistExpired')
                                : '—'}
                          </div>
                        </Table.Cell>
                        <Table.Cell>
                          <div className="mono text-center text-foreground">{formatQuotaAmount(r.soonestRemain)}</div>
                        </Table.Cell>
                        <Table.Cell>
                          <div className="mono text-center text-foreground">
                            {r.soonest ? formatQuotaAmount(r.dailyBurn) : '—'}
                          </div>
                        </Table.Cell>
                      </Table.Row>
                    )
                  })}
              </Table.Body>
            </Table.Content>
          </Table.ScrollContainer>
        </Table>

        <p className="text-caption text-muted">{t('expiryDistFoot')}</p>
      </div>
    </Card>
  )
}
