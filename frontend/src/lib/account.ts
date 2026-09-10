import type { AccountQuota, Overview } from '@/api/types'

export type AccountRow = NonNullable<Overview['accounts']>[number]
export type AccountState = 'disabled' | 'quota_exhausted' | 'cooling' | 'hot' | 'ready' | 'login' | 'loading' | 'starting' | 'unavailable' | 'dead' | 'auth_failed'
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

export function accountState(account: AccountRow): AccountState {
  if (!account.enabled) return 'disabled'
  if (account.status === 'quota_exhausted' || account.quota?.exceeded) return 'quota_exhausted'
  if (account.status === 'cooling' || cooldownLabel(account.down_until || account.cooldown_until)) return 'cooling'
  if (account.status === 'dead' || account.runtime_state === 'dead') return 'dead'
  if (account.status === 'auth_failed' || account.runtime_state === 'auth_failed') return 'auth_failed'
  if (account.status === 'login_required') return 'login'
  if ((account.status === 'starting' || account.runtime_state === 'starting') && !account.hot && !account.ready) {
    return account.quota ? 'starting' : 'loading'
  }
  if (account.status === 'ready') return account.hot ? 'hot' : 'ready'
  if (account.hot) return 'hot'
  if (account.ready) return 'ready'
  if (account.status === 'error' || account.status === 'offline') return 'unavailable'
  return 'unavailable'
}

export function isAvailable(account: AccountRow) {
  const state = accountState(account)
  return state === 'hot' || state === 'ready'
}

export function runtimeSegments(state: AccountState) {
  if (state === 'hot') return 12
  if (state === 'ready') return 9
  if (state === 'cooling') return 5
  if (state === 'login') return 3
  if (state === 'loading') return 6
  if (state === 'starting') return 6
  if (state === 'unavailable') return 2
  if (state === 'auth_failed') return 3
  if (state === 'dead') return 2
  return 1
}

export function runtimeTone(state: AccountState): 'ok' | 'warn' | 'danger' | 'muted' {
  if (state === 'hot' || state === 'ready') return 'ok'
  if (state === 'cooling' || state === 'quota_exhausted') return 'warn'
  if (state === 'loading' || state === 'starting') return 'warn'
  if (state === 'auth_failed' || state === 'unavailable' || state === 'dead') return 'danger'
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

export function quotaTone(quota: AccountQuota): QuotaTone {
  const percentage = quota.percentage ?? 0
  if (quota.exceeded) return 'danger'
  if (percentage >= 80) return 'warn'
  return 'ok'
}
