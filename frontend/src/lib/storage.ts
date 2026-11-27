// Storage can be unavailable (blocked cookies, hardened browser policy, some
// private modes). A thrown SecurityError during a provider initializer would
// otherwise blank the console before any error boundary can render.
function storage(): Storage | null {
  try {
    return typeof window === 'undefined' ? null : window.localStorage
  } catch {
    return null
  }
}

export function getStorageItem(key: string): string | null {
  try {
    return storage()?.getItem(key) ?? null
  } catch {
    return null
  }
}

export function setStorageItem(key: string, value: string): void {
  try {
    storage()?.setItem(key, value)
  } catch {
    /* persistence is optional; the in-memory state still applies */
  }
}

export function removeStorageItem(key: string): void {
  try {
    storage()?.removeItem(key)
  } catch {
    /* already unavailable */
  }
}
