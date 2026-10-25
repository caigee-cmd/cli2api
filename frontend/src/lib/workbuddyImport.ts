import { importAccount } from '@/api/overview'

export type WorkbuddyImportResult = {
  object?: string
  succeeded?: number
  failed?: number
  errors?: Array<{ index?: number; name?: string; message?: string }>
  id?: string
}

export type WorkbuddyImportOptions = {
  region: 'cn' | 'global'
  name?: string
  enabled?: boolean
  max_inflight?: number
  priority?: number
  drop_system_prompt?: boolean
  proxy_url?: string
}

const CN_DOMAIN_MARKERS = ['codebuddy.cn']
const GLOBAL_DOMAIN_MARKERS = ['workbuddy.ai', 'workbuddy.com']

function collectDomains(bundle: Record<string, unknown>): string[] {
  const out: string[] = []
  const push = (value: unknown) => {
    if (typeof value === 'string' && value.trim()) out.push(value.trim().toLowerCase())
  }
  push(bundle.domain)
  const accounts = bundle.accounts
  if (Array.isArray(accounts)) {
    for (const entry of accounts) {
      if (entry && typeof entry === 'object') push((entry as Record<string, unknown>).domain)
    }
  }
  return out
}

function domainLooksCN(domain: string) {
  return CN_DOMAIN_MARKERS.some((marker) => domain.includes(marker))
}

function domainLooksGlobal(domain: string) {
  return GLOBAL_DOMAIN_MARKERS.some((marker) => domain.includes(marker))
}

/** True when the package clearly targets Global and has no CN domain. */
export function looksLikeWorkbuddyGlobalOnly(bundle: Record<string, unknown>): boolean {
  const domains = collectDomains(bundle)
  if (domains.length === 0) return false
  const hasCN = domains.some(domainLooksCN)
  const hasGlobal = domains.some(domainLooksGlobal)
  return hasGlobal && !hasCN
}

/** Loose check that the JSON is a WorkBuddy credential or hub package. */
export function isLikelyWorkbuddyBundle(bundle: Record<string, unknown>): boolean {
  const format = typeof bundle.format === 'string' ? bundle.format : ''
  if (format === 'workbuddy-accounts' || format === 'workbuddy-oauth-v1') return true
  if (Array.isArray(bundle.accounts)) return true
  if (typeof bundle.accessToken === 'string' || typeof bundle.access_token === 'string') return true
  if (bundle.account && typeof bundle.account === 'object') return true
  if (bundle.auth && typeof bundle.auth === 'object') return true
  return false
}

/**
 * Normalize a parsed WorkBuddy bundle the same way AddAccountModal does:
 * keep/force workbuddy-accounts for hub packs; otherwise fill workbuddy-oauth-v1.
 */
export function normalizeWorkbuddyImportBundle(
  bundle: Record<string, unknown>,
  options: WorkbuddyImportOptions,
): Record<string, unknown> {
  const next: Record<string, unknown> = { ...bundle }
  const isWorkbuddyAccounts =
    next.format === 'workbuddy-accounts' || Array.isArray(next.accounts)
  if (!next.format) {
    next.format = isWorkbuddyAccounts ? 'workbuddy-accounts' : 'workbuddy-oauth-v1'
  } else if (isWorkbuddyAccounts) {
    next.format = 'workbuddy-accounts'
  }
  return {
    ...next,
    name: options.name?.trim() || next.name,
    enabled: options.enabled ?? true,
    provider: 'workbuddy',
    region: options.region,
    max_inflight: options.max_inflight ?? 4,
    priority: options.priority ?? 50,
    drop_system_prompt: options.drop_system_prompt ?? true,
    ...(options.proxy_url != null && options.proxy_url !== ''
      ? { proxy_url: options.proxy_url }
      : {}),
  }
}

export function summarizeWorkbuddyImportResult(
  result: WorkbuddyImportResult,
  t: (key: string) => string,
): { succeeded: number; failed: number; message: string } {
  const succeeded = typeof result?.succeeded === 'number'
    ? result.succeeded
    : (result?.id || result ? 1 : 0)
  const failed = typeof result?.failed === 'number' ? result.failed : 0
  const failureSummary = (result?.errors || [])
    .slice(0, 3)
    .map((item) => item.message || `index ${item.index}`)
    .filter(Boolean)
    .join('; ')
  let message: string
  if (failed > 0) {
    message = t('accountImportPartial')
      .replace('{ok}', String(succeeded))
      .replace('{fail}', String(failed))
      + (failureSummary ? ` — ${failureSummary}` : '')
  } else if (succeeded > 1) {
    message = t('accountImportBatch').replace('{n}', String(succeeded))
  } else {
    message = t('accountImported')
  }
  return { succeeded, failed, message }
}

/** Fixed WorkBuddy CN import used by AccountsPage batch file picker. */
export async function importWorkbuddyCNBundle(
  bundle: Record<string, unknown>,
  options: Omit<WorkbuddyImportOptions, 'region'> = {},
): Promise<WorkbuddyImportResult> {
  const payload = normalizeWorkbuddyImportBundle(bundle, { ...options, region: 'cn' })
  return importAccount(payload) as Promise<WorkbuddyImportResult>
}
