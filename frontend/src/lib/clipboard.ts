// copyText copies `value` to the clipboard, falling back to a hidden textarea
// + execCommand when the async Clipboard API is unavailable. navigator.clipboard
// is only defined in secure contexts, so the console served over plain http
// (e.g. http://192.168.101.1:3010) would otherwise silently fail to copy.
export async function copyText(value: string): Promise<boolean> {
  if (!value) return false
  try {
    if (typeof navigator !== 'undefined' && navigator.clipboard && window.isSecureContext) {
      await navigator.clipboard.writeText(value)
      return true
    }
  } catch {
    // fall through to the legacy path
  }
  try {
    const area = document.createElement('textarea')
    area.value = value
    area.setAttribute('readonly', '')
    area.style.position = 'fixed'
    area.style.top = '-1000px'
    area.style.opacity = '0'
    document.body.appendChild(area)
    area.focus()
    area.select()
    const ok = document.execCommand('copy')
    document.body.removeChild(area)
    return ok
  } catch {
    return false
  }
}
