import type { AccountQuota, AccountQuotaWindow, Overview } from '@/api/types'

export type AccountRow = NonNullable<Overview['accounts']>[number]
export type AccountState = 'disabled' | 'quota_exhausted' | 'cooling' | 'hot' | 'ready' | 'login' | 'loading' | 'starting' | 'unavailable' | 'dead'
export type QuotaTone = 'ok' | 'warn' | 'danger'

export function cooldownLabel(until?: string | null) {
  if (!until) return ''
  const milliseconds = Date.parse(until) - Date.now()
  if (!Number.isFinite(milliseconds) || milliseconds <= 0) return ''
  const seconds = Math.ceil(milliseconds / 1000)
  return seconds < 60 ? `${seconds}s` : `${Math.ceil(seconds / 60)}m`
}

export function modelCooldownEntries(account: AccountRow) {
  return Object.entries(account.model_cooldowns ?? {})
    .filter(([, until]) => Boolean(cooldownLabel(until)))
    .sort(([left], [right]) => left.localeCompare(right))
}

// accountState resolves the single badge an account card shows.
//
// The pool's live ready/hot/runtime_state and the stored status column come
// from different places and drift apart: the status column is only rewritten
// when a probe or a credential renewal runs, so it can both lag behind a
// recovery and survive an outage. The ladder below settles that explicitly
// instead of letting the first matching field win:
//
//   disabled        the operator turned it off; nothing else matters
//   dead            the daemon crashed and is restarting (process problem)
//   login           the credential is rejected; no amount of balance helps,
//                   so this outranks quota and cooldown
//   quota_exhausted  live quota snapshot says exhausted (a stored
//                   quota_exhausted status is only trusted while no live
//                   snapshot has arrived)
//   cooling         temporary cooldown after a classified failure
//   starting/loading coming up for the first time
//   hot/ready       serving
//   unavailable     anything else that cannot route right now
//
// A probe that fails for a non-auth reason is reported by the pool as runtime
// "auth_failed" too, so runtime auth_failed only means "login" when the
// reported error kind is actually auth.
export function accountState(account: AccountRow): AccountState {
  if (!account.enabled) return 'disabled'
  const status = String(account.status || '').toLowerCase()
  const runtime = String(account.runtime_state || '').toLowerCase()
  const errorKind = String(account.last_error_kind || '').toLowerCase()
  const cooldown = cooldownLabel(account.down_until || account.cooldown_until)
  const serving = Boolean(account.ready || account.hot) && runtime !== 'dead'
  const quotaExhausted = Boolean(account.quota?.exceeded)
    || (status === 'quota_exhausted' && !account.quota)

  if (runtime === 'dead' || status === 'dead') return 'dead'
  if (status === 'login_required') return 'login'
  if (runtime === 'auth_failed' && errorKind === 'auth' && !serving) return 'login'
  if (quotaExhausted) return 'quota_exhausted'
  if (status === 'cooling' || cooldown) return 'cooling'
  if ((status === 'starting' || runtime === 'starting') && !account.hot && !account.ready) {
    return account.quota ? 'starting' : 'loading'
  }
  if (account.hot) return 'hot'
  if (account.ready) return 'ready'
  return 'unavailable'
}

export function isAvailable(account: AccountRow) {
  const state = accountState(account)
  return state === 'hot' || state === 'ready'
}

// checkedInToday mirrors the backend's CheckedInLocalDay: a check-in only
// counts for today when it succeeded (or was already done) and landed on the
// account's local calendar day. The timezone is the provider check-in policy
// zone (undefined falls back to the browser zone). Used by the account card
// badge and the batch "check in all" skip logic.
export function checkedInToday(account: AccountRow, timezone?: string) {
  const status = account.last_checkin_status
  if ((status !== 'success' && status !== 'already') || !account.last_checkin_at) return false
  const recorded = new Date(account.last_checkin_at)
  if (Number.isNaN(recorded.getTime())) return false
  const dayOf = (value: Date) => new Intl.DateTimeFormat('en-CA', {
    timeZone: timezone || undefined,
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
  }).format(value)
  return dayOf(recorded) === dayOf(new Date())
}

// cleanAccountError turns a verbatim probe/quota error into something a card or
// a toast can show. Upstreams in front of nginx answer with an HTML error page,
// and the probe prefixes the internal account id that the row already carries.
// Keep the human-readable summary: status line, title or first sentence.
export function cleanAccountError(message?: string | null, accountId?: string): string {
  if (!message) return ''
  let text = String(message)
  if (accountId) {
    const escaped = accountId.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
    text = text
      .replace(new RegExp(`fetch quota account=${escaped}:\\s*`, 'gi'), '')
      .replace(new RegExp(`probe account=${escaped}:\\s*`, 'gi'), '')
      .replace(new RegExp(`\\baccount=${escaped}\\b:?\\s*`, 'gi'), '')
  }
  text = text.replace(/\bfetch quota account=[^:\s]+:\s*/gi, '')
  text = text.replace(/\bprobe account=[^:\s]+:\s*/gi, '')
  const titled = text.match(/<title>([^<]+)<\/title>/i)
  text = titled ? titled[1] : text.replace(/<[^>]*>/g, ' ')
  text = text
    .replace(/&nbsp;/gi, ' ')
    .replace(/&amp;/gi, '&')
    .replace(/&lt;/gi, '<')
    .replace(/&gt;/gi, '>')
    .replace(/&quot;/gi, '"')
    .replace(/&#39;/gi, "'")
    .replace(/\s+/g, ' ')
    .trim()
  if (text.length > 240) text = `${text.slice(0, 237)}…`
  return text
}

export function runtimeSegments(state: AccountState) {
  if (state === 'hot') return 12
  if (state === 'ready') return 9
  if (state === 'cooling') return 5
  if (state === 'login') return 3
  if (state === 'loading') return 6
  if (state === 'starting') return 6
  if (state === 'unavailable') return 2
  if (state === 'dead') return 2
  return 1
}

export function runtimeTone(state: AccountState): 'ok' | 'warn' | 'danger' | 'muted' {
  if (state === 'hot' || state === 'ready') return 'ok'
  if (state === 'cooling' || state === 'quota_exhausted') return 'warn'
  if (state === 'loading' || state === 'starting') return 'warn'
  if (state === 'unavailable' || state === 'dead') return 'danger'
  if (state === 'login') return 'danger'
  return 'muted'
}

export function formatQuotaAmount(value: number | undefined) {
  if (value == null || !Number.isFinite(value)) return '—'
  if (Math.abs(value) >= 1000) return `${(value / 1000).toFixed(value % 1000 === 0 ? 0 : 1)}k`
  return String(Math.round(value * 100) / 100)
}

export function quotaUsedRatio(quota: AccountQuota) {
  const percentage = quota.percentage ?? 0
  if (!Number.isFinite(percentage)) return 0
  return Math.min(1, Math.max(0, percentage / 100))
}

export function quotaTone(quota: Pick<AccountQuota, 'percentage' | 'exceeded'>): QuotaTone {
  const percentage = quota.percentage ?? 0
  if (quota.exceeded) return 'danger'
  if (percentage >= 80) return 'warn'
  return 'ok'
}

export function quotaWindows(quota: AccountQuota) {
  return (quota.windows ?? []).filter((window) => Boolean(window.id))
}

const codexPlanLabels: Record<string, string> = {
  free: 'Free',
  go: 'Go',
  plus: 'Plus',
  pro: 'Pro',
  pro_plus: 'Pro+',
  proplus: 'Pro+',
  prolite: 'Pro Lite',
  promax: 'Pro Max',
  team: 'Team',
  business: 'Business',
  enterprise: 'Enterprise',
  power: 'Power',
  edu: 'Edu',
  edu_plus: 'Edu Plus',
  edu_pro: 'Edu Pro',
  education: 'Education',
}

// codexPlanLabel turns an upstream plan_type into the subscription name shown
// on the account card. Unknown values stay readable; empty stays hidden.
export function codexPlanLabel(plan: string | undefined) {
  const raw = plan?.trim().toLowerCase() ?? ''
  if (!raw || raw === 'unknown' || raw === 'guest') return ''
  return codexPlanLabels[raw] ?? raw
    .replace(/[_-]+/g, ' ')
    .replace(/\bplus\b/gi, '+')
    .replace(/\s+\+/g, '+')
    .replace(/\b\w/g, (char) => char.toUpperCase())
}

export function quotaWindowLabel(window: AccountQuotaWindow, t: (key: string) => string) {
  if (window.id === 'daily') return t('quotaDaily')
  if (window.id === 'weekly') return t('quotaWeekly')
  if (window.id === 'monthly') return t('quotaMonthly')
  if (window.id === 'fiveHour') return t('quotaFiveHour')
  if (window.id === 'weeklyLimit') return t('quotaWeeklyLimit')
  if (window.id === 'monthlyLimit') return t('quotaMonthlyLimit')
  return window.label || t('quota')
}

export function quotaResetLabel(resetAt: string | undefined, t: (key: string, vars?: Record<string, string | number>) => string) {
  if (!resetAt) return ''
  const milliseconds = Date.parse(resetAt) - Date.now()
  if (!Number.isFinite(milliseconds) || milliseconds <= 0) return t('quotaResetsSoon')
  const minutes = Math.max(1, Math.ceil(milliseconds / 60000))
  if (minutes < 60) return t('quotaResetsInMinutes', { n: minutes })
  return t('quotaResetsInHours', { n: Math.round(minutes / 60) })
}

// quotaExpiryLabel renders the soonest package expiry, e.g.
// "1,500 credits expire on 10/1". returns '' when the provider did not report
// an expiry.
export function quotaExpiryLabel(
  quota: { expires_at?: number; expiring_remain?: number; unit?: string },
  t: (key: string, vars?: Record<string, string | number>) => string,
) {
  if (!quota.expires_at || quota.expires_at <= 0 || !Number.isFinite(quota.expires_at)) return ''
  const date = new Date(quota.expires_at * 1000)
  if (Number.isNaN(date.getTime())) return ''
  const day = `${date.getFullYear()}/${date.getMonth() + 1}/${date.getDate()}`
  const amount = quota.expiring_remain && quota.expiring_remain > 0
    ? `${formatQuotaAmount(quota.expiring_remain)} `
    : ''
  return t('quotaExpiresOn', { amount, unit: quota.unit || 'credits', date: day })
}
