import { createContext, useContext, useMemo, useState, type ReactNode } from 'react'

const STORAGE_KEY = 'cli2api_key'

type ApiKeyContextValue = {
  apiKey: string
  setApiKey: (value: string) => void
}

const ApiKeyContext = createContext<ApiKeyContextValue | null>(null)

export function ApiKeyProvider({ children }: { children: ReactNode }) {
  const [apiKey, setApiKeyState] = useState(() => localStorage.getItem(STORAGE_KEY) || '')

  const value = useMemo<ApiKeyContextValue>(() => ({
    apiKey,
    setApiKey: (next) => {
      const value = String(next || '')
      localStorage.setItem(STORAGE_KEY, value)
      setApiKeyState(value)
    },
  }), [apiKey])

  return <ApiKeyContext.Provider value={value}>{children}</ApiKeyContext.Provider>
}

export function useApiKey() {
  const ctx = useContext(ApiKeyContext)
  if (!ctx) throw new Error('useApiKey must be used within ApiKeyProvider')
  return ctx
}

export function readStoredApiKey() {
  return (localStorage.getItem(STORAGE_KEY) || '').trim()
}
