import type { AccountBundle } from '@/api/overview'

export const ACCOUNT_BUNDLE_FORMAT = 'cli2api-accounts'

export function isAccountBundle(value: unknown): value is AccountBundle {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return false
  const record = value as Record<string, unknown>
  return (
    typeof record.format === 'string' &&
    record.format.trim().toLowerCase() === ACCOUNT_BUNDLE_FORMAT &&
    Array.isArray(record.accounts)
  )
}

export function parseAccountBundleText(text: string): AccountBundle {
  let parsed: unknown
  try {
    parsed = JSON.parse(text)
  } catch {
    throw new Error('bad_json')
  }
  if (!isAccountBundle(parsed)) throw new Error('bad_bundle')
  return parsed as AccountBundle
}

export function downloadAccountBundle(bundle: AccountBundle) {
  const text = JSON.stringify(bundle, null, 2)
  const blob = new Blob([text], { type: 'application/json' })
  const url = URL.createObjectURL(blob)
  const stamp = (bundle.exported_at || new Date().toISOString())
    .replace(/[:.]/g, '-')
    .slice(0, 19)
  const anchor = document.createElement('a')
  anchor.href = url
  anchor.download = `cli2api-accounts-${stamp}.json`
  document.body.appendChild(anchor)
  anchor.click()
  anchor.remove()
  URL.revokeObjectURL(url)
}
