// Tiny toast store (after arc's toast-stack). Reusing an id updates that toast
// in place, so a "loading" toast can turn into its result without stacking.
export type ToastType = 'loading' | 'success' | 'error' | 'info'

export type ToastInput = {
  id?: string
  type?: ToastType
  title: string
  description?: string
  /** Milliseconds before it closes; loading toasts wait for an update. */
  duration?: number
}

export type ToastItem = Required<Pick<ToastInput, 'id' | 'type' | 'title'>> & {
  description?: string
  version: number
  leaving: boolean
}

const DEFAULT_DURATION: Record<ToastType, number> = { loading: Infinity, success: 3200, error: 6000, info: 4000 }
const MAX_TOASTS = 4

let items: ToastItem[] = []
let sequence = 0
const listeners = new Set<() => void>()
const timers = new Map<string, number>()

function emit() {
  for (const listener of listeners) listener()
}

export function subscribeToasts(listener: () => void) {
  listeners.add(listener)
  return () => { listeners.delete(listener) }
}

export function getToasts() {
  return items
}

export function dismissToast(id: string) {
  const timer = timers.get(id)
  if (timer) window.clearTimeout(timer)
  timers.delete(id)
  if (!items.some((item) => item.id === id && !item.leaving)) return
  items = items.map((item) => item.id === id ? { ...item, leaving: true } : item)
  emit()
}

/** Called by the Toaster once a leaving toast finished animating out. */
export function removeToast(id: string) {
  items = items.filter((item) => item.id !== id)
  emit()
}

export function toast(input: ToastInput): string {
  const id = input.id ?? `toast-${++sequence}`
  const type = input.type ?? 'info'
  const existing = items.find((item) => item.id === id)
  const next: ToastItem = {
    id,
    type,
    title: input.title,
    description: input.description,
    version: (existing?.version ?? 0) + 1,
    leaving: false,
  }
  items = existing ? items.map((item) => item.id === id ? next : item) : [...items, next]
  const visible = items.filter((item) => !item.leaving)
  if (visible.length > MAX_TOASTS) dismissToast(visible[0].id)
  const old = timers.get(id)
  if (old) window.clearTimeout(old)
  timers.delete(id)
  const ms = input.duration ?? DEFAULT_DURATION[type]
  if (Number.isFinite(ms)) timers.set(id, window.setTimeout(() => dismissToast(id), ms))
  emit()
  return id
}
