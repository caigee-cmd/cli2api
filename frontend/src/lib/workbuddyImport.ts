import { importAccount } from '@/api/overview'
import type { ImportRegion } from '@/lib/accountImportDetect'

export type WorkbuddyImportResult = {
  object?: string
  succeeded?: number
  failed?: number
  errors?: Array<{ index?: number; name?: string; message?: string }>
  id?: string
}

export type WorkbuddyImportOptions = {
  region: ImportRegion
  name?: string
  enabled?: boolean
  max_inflight?: number
  priority?: number
  drop_system_prompt?: boolean
  proxy_url?: string
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

/** Import a WorkBuddy pack with the user-confirmed region (CN or Global). */
export async function importWorkbuddyBundle(
  bundle: Record<string, unknown>,
  options: WorkbuddyImportOptions,
): Promise<WorkbuddyImportResult> {
  const payload = normalizeWorkbuddyImportBundle(bundle, options)
  return importAccount(payload) as Promise<WorkbuddyImportResult>
}
