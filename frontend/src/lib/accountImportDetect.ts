/** Detect credential / hub package shape for Accounts page batch import. */

export type ImportRegion = 'cn' | 'global'

export type ImportDetectConfidence = 'high' | 'medium' | 'low'

export type ImportDetectResult = {
  /** Normalized credential format when known. */
  format: string | null
  provider: string | null
  /** Suggested region; null when unknown or mixed. */
  region: ImportRegion | null
  /** Whether region must be chosen by the user before import. */
  regionAmbiguous: boolean
  confidence: ImportDetectConfidence
  warnings: string[]
  /** This PR only imports WorkBuddy; others stay detected for future use. */
  batchImportSupported: boolean
}

const FORMAT_TO_PROVIDER: Record<string, string> = {
  'workbuddy-accounts': 'workbuddy',
  'workbuddy-oauth-v1': 'workbuddy',
  'qoder-native-v1': 'qoder',
  'trae-oauth-v1': 'trae',
  'devin-session-v1': 'devin',
  'command-key-v1': 'command',
  'codex-oauth-v1': 'codex',
  'zhipu-key-v1': 'zhipu',
  'qwenwork-oauth-v1': 'qwenwork',
  'meta-oauth-v1': 'meta',
  'meta-key-v1': 'meta',
}

const CN_DOMAIN_MARKERS = ['codebuddy.cn', 'workbuddy.cn']
const GLOBAL_DOMAIN_MARKERS = ['workbuddy.ai', 'workbuddy.com']

function isPlainObject(value: unknown): value is Record<string, unknown> {
  return Boolean(value) && typeof value === 'object' && !Array.isArray(value)
}

/** Normalize format / exportType aliases (e.g. WorkBuddy-accounts → workbuddy-accounts). */
function normalizeFormatAlias(raw: string): string {
  const trimmed = raw.trim()
  if (!trimmed) return ''
  const lower = trimmed.toLowerCase()
  if (lower === 'workbuddy-accounts') return 'workbuddy-accounts'
  return lower
}

function readFormatHint(bundle: Record<string, unknown>): string {
  if (typeof bundle.format === 'string' && bundle.format.trim()) {
    return normalizeFormatAlias(bundle.format)
  }
  if (typeof bundle.exportType === 'string' && bundle.exportType.trim()) {
    return normalizeFormatAlias(bundle.exportType)
  }
  return ''
}

function collectDomains(bundle: Record<string, unknown>): string[] {
  const out: string[] = []
  const push = (value: unknown) => {
    if (typeof value === 'string' && value.trim()) out.push(value.trim().toLowerCase())
  }
  push(bundle.domain)
  if (isPlainObject(bundle.account)) push(bundle.account.domain)
  if (isPlainObject(bundle.auth)) push(bundle.auth.domain)
  if (Array.isArray(bundle.accounts)) {
    for (const entry of bundle.accounts) {
      if (!isPlainObject(entry)) continue
      push(entry.domain)
      if (isPlainObject(entry.account)) push(entry.account.domain)
      if (isPlainObject(entry.auth)) push(entry.auth.domain)
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

function hasTokenField(entry: Record<string, unknown>) {
  return (
    typeof entry.accessToken === 'string'
    || typeof entry.access_token === 'string'
    || typeof entry.refreshToken === 'string'
    || typeof entry.refresh_token === 'string'
  )
}

function looksLikeWorkbuddyAccountsEntry(entry: unknown): boolean {
  if (!isPlainObject(entry)) return false
  if (hasTokenField(entry) || typeof entry.domain === 'string' || typeof entry.uid === 'string') {
    return true
  }
  // Official client export: nested { account, auth }.
  if (isPlainObject(entry.account) || isPlainObject(entry.auth)) {
    const account = isPlainObject(entry.account) ? entry.account : null
    const auth = isPlainObject(entry.auth) ? entry.auth : null
    if (auth && (hasTokenField(auth) || typeof auth.domain === 'string')) return true
    if (account && (typeof account.uid === 'string' || typeof account.domain === 'string')) return true
  }
  return false
}

function detectWorkbuddyRegion(bundle: Record<string, unknown>): {
  region: ImportRegion | null
  ambiguous: boolean
  warning?: string
} {
  const domains = collectDomains(bundle)
  if (domains.length === 0) {
    return { region: 'cn', ambiguous: true, warning: 'region_unknown' }
  }
  const hasCN = domains.some(domainLooksCN)
  const hasGlobal = domains.some(domainLooksGlobal)
  if (hasCN && hasGlobal) {
    return { region: null, ambiguous: true, warning: 'region_mixed' }
  }
  if (hasCN) return { region: 'cn', ambiguous: false }
  if (hasGlobal) return { region: 'global', ambiguous: false }
  // Domains present but neither marker matched — ask the user.
  return { region: 'cn', ambiguous: true, warning: 'region_unknown' }
}

function detectByShape(bundle: Record<string, unknown>): {
  format: string | null
  provider: string | null
  confidence: ImportDetectConfidence
} {
  if (Array.isArray(bundle.accounts) && bundle.accounts.some(looksLikeWorkbuddyAccountsEntry)) {
    return { format: 'workbuddy-accounts', provider: 'workbuddy', confidence: 'medium' }
  }
  if (
    (typeof bundle.user_blob === 'string' || typeof bundle.userBlob === 'string')
    && (typeof bundle.machine_id === 'string' || typeof bundle.machineId === 'string')
  ) {
    return { format: 'qoder-native-v1', provider: 'qoder', confidence: 'medium' }
  }
  if (typeof bundle.session_token === 'string' || typeof bundle.sessionToken === 'string') {
    return { format: 'devin-session-v1', provider: 'devin', confidence: 'low' }
  }
  if (hasTokenField(bundle) || (isPlainObject(bundle.account) && hasTokenField(bundle.account)) || isPlainObject(bundle.auth)) {
    return { format: 'workbuddy-oauth-v1', provider: 'workbuddy', confidence: 'low' }
  }
  if (
    typeof bundle.dca_token === 'string'
    || typeof bundle.dcaToken === 'string'
    || (typeof bundle.access_token === 'string' && String(bundle.access_token).startsWith('dca:'))
    || (typeof bundle.accessToken === 'string' && String(bundle.accessToken).startsWith('dca:'))
  ) {
    return { format: 'meta-oauth-v1', provider: 'meta', confidence: 'medium' }
  }
  if (typeof bundle.api_key === 'string' || typeof bundle.apiKey === 'string') {
    // Ambiguous between zhipu / command / meta — leave provider null unless more hints.
    if (typeof bundle.organization === 'string' || typeof bundle.project === 'string') {
      return { format: 'zhipu-key-v1', provider: 'zhipu', confidence: 'low' }
    }
    if (
      typeof bundle.base_url === 'string' && String(bundle.base_url).includes('meta.ai')
      || typeof bundle.baseUrl === 'string' && String(bundle.baseUrl).includes('meta.ai')
      || typeof bundle.email === 'string'
    ) {
      return { format: 'meta-key-v1', provider: 'meta', confidence: 'low' }
    }
    return { format: 'command-key-v1', provider: 'command', confidence: 'low' }
  }
  return { format: null, provider: null, confidence: 'low' }
}

/**
 * Inspect a parsed JSON object and return provider/format/region hints.
 * Pure: no I/O. Callers toast on parse failure before invoking this.
 */
export function detectImportBundle(raw: unknown): ImportDetectResult {
  if (!isPlainObject(raw)) {
    return {
      format: null,
      provider: null,
      region: null,
      regionAmbiguous: true,
      confidence: 'low',
      warnings: ['not_object'],
      batchImportSupported: false,
    }
  }

  const warnings: string[] = []
  const formatHint = readFormatHint(raw)
  // Raw (pre-normalize) field for unknown_format warning when neither format nor exportType maps.
  const rawFormatField = typeof raw.format === 'string' ? raw.format.trim() : ''
  const rawExportType = typeof raw.exportType === 'string' ? raw.exportType.trim() : ''
  const rawHintForUnknown = rawFormatField || rawExportType

  let format: string | null = formatHint || null
  let provider: string | null = format ? (FORMAT_TO_PROVIDER[format] ?? null) : null
  let confidence: ImportDetectConfidence = format && provider ? 'high' : 'low'

  if (!provider) {
    const shaped = detectByShape(raw)
    format = format || shaped.format
    provider = shaped.provider
    confidence = formatHint && !FORMAT_TO_PROVIDER[formatHint] ? 'low' : shaped.confidence
    if (rawHintForUnknown && !FORMAT_TO_PROVIDER[formatHint]) {
      warnings.push('unknown_format')
    }
  }

  // Hub packs with accounts[] should stay on workbuddy-accounts even if format was oauth-v1.
  if (provider === 'workbuddy' && Array.isArray(raw.accounts)) {
    format = 'workbuddy-accounts'
  }

  let region: ImportRegion | null = null
  let regionAmbiguous = false
  if (provider === 'workbuddy') {
    const wb = detectWorkbuddyRegion(raw)
    region = wb.region
    regionAmbiguous = wb.ambiguous
    if (wb.warning) warnings.push(wb.warning)
  }

  const batchImportSupported = provider === 'workbuddy'

  return {
    format,
    provider,
    region,
    regionAmbiguous,
    confidence,
    warnings,
    batchImportSupported,
  }
}

export function parseImportBundleText(text: string): Record<string, unknown> {
  let parsed: unknown
  try {
    parsed = JSON.parse(text)
  } catch {
    throw new Error('bad_json')
  }
  if (!isPlainObject(parsed)) throw new Error('bad_json')
  return parsed
}
