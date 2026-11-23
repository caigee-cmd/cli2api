import { useSyncExternalStore } from 'react'

const EMPTY_SUBSCRIBE = () => () => {}
const listeners = new Set<() => void>()
let current = Date.now()
let timer: number | null = null

function tick() {
  current = Date.now()
  for (const listener of listeners) listener()
}

function subscribe(listener: () => void) {
  if (listeners.size === 0) current = Date.now()
  listeners.add(listener)
  if (timer === null) timer = window.setInterval(tick, 1000)
  return () => {
    listeners.delete(listener)
    if (listeners.size === 0 && timer !== null) {
      window.clearInterval(timer)
      timer = null
    }
  }
}

function getSnapshot() {
  return current
}

/** One shared one-second clock for every mounted countdown, instead of one timer per card. */
export function useSharedClock(enabled = true) {
  return useSyncExternalStore(enabled ? subscribe : EMPTY_SUBSCRIBE, getSnapshot, getSnapshot)
}
