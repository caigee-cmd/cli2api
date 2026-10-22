import { api } from './client'

export type LiveAccount = { id: string; in_flight: number; max_inflight: number; enabled: boolean }

export type LiveSnapshot = {
  time: string
  in_flight: number
  capacity: number
  rpm: number
  accounts: LiveAccount[]
}

export function fetchLive() {
  return api<LiveSnapshot>('/api/live')
}
