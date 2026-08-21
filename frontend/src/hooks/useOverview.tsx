import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from 'react'
import { fetchOverview } from '@/api/overview'
import type { Overview } from '@/api/types'

type OverviewContextValue = {
  overview: Overview | null
  loading: boolean
  error: string | null
  refresh: () => Promise<void>
  setOverview: (next: Overview | null) => void
}

const OverviewContext = createContext<OverviewContextValue | null>(null)

export function OverviewProvider({ children }: { children: ReactNode }) {
  const [overview, setOverview] = useState<Overview | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const refresh = useCallback(async () => {
    setLoading(true)
    try {
      const data = await fetchOverview()
      setOverview(data)
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void refresh()
  }, [refresh])

  const value = useMemo(() => ({ overview, loading, error, refresh, setOverview }), [overview, loading, error, refresh])
  return <OverviewContext.Provider value={value}>{children}</OverviewContext.Provider>
}

export function useOverview() {
  const ctx = useContext(OverviewContext)
  if (!ctx) throw new Error('useOverview must be used within OverviewProvider')
  return ctx
}
