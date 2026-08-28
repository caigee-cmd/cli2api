import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from 'react'
import { fetchOverview } from '@/api/overview'
import { isUnauthorized } from '@/api/client'
import { useApiKey } from '@/hooks/useApiKey'
import type { Overview } from '@/api/types'

type RefreshOptions = {
  silent?: boolean
}

type OverviewContextValue = {
  overview: Overview | null
  loading: boolean
  error: string | null
  refresh: (keyOverride?: string, options?: RefreshOptions) => Promise<Overview>
  setOverview: (next: Overview | null) => void
}

const OverviewContext = createContext<OverviewContextValue | null>(null)

export function OverviewProvider({ children }: { children: ReactNode }) {
  const { apiKey, signOut } = useApiKey()
  const [overview, setOverview] = useState<Overview | null>(null)
  const [loading, setLoading] = useState(Boolean(apiKey))
  const [error, setError] = useState<string | null>(null)

  const refresh = useCallback(async (keyOverride?: string, options?: RefreshOptions) => {
    const key = keyOverride ?? apiKey
    if (!key) {
      setOverview(null)
      setError(null)
      setLoading(false)
      throw new Error('missing_api_key')
    }
    const silent = Boolean(options?.silent)
    if (!silent) setLoading(true)
    try {
      const data = await fetchOverview(key, { refreshQuota: !silent })
      setOverview(data)
      setError(null)
      return data
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err)
      if (!silent) setOverview(null)
      setError(msg)
      if (isUnauthorized(err)) signOut()
      throw err
    } finally {
      if (!silent) setLoading(false)
    }
  }, [apiKey, signOut])

  useEffect(() => {
    if (!apiKey) {
      setOverview(null)
      setLoading(false)
      setError(null)
      return
    }
    void refresh().catch(() => undefined)
  }, [apiKey, refresh])

  const value = useMemo(
    () => ({ overview, loading, error, refresh, setOverview }),
    [overview, loading, error, refresh],
  )
  return <OverviewContext.Provider value={value}>{children}</OverviewContext.Provider>
}

export function useOverview() {
  const ctx = useContext(OverviewContext)
  if (!ctx) throw new Error('useOverview must be used within OverviewProvider')
  return ctx
}
