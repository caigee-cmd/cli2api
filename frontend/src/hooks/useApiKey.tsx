import { createContext, useContext, useMemo, useState, type ReactNode } from 'react'
import { getStorageItem, removeStorageItem, setStorageItem } from '@/lib/storage'

const STORAGE_KEY = 'cli2api_key'

type ApiKeyContextValue = {
  apiKey: string
  setApiKey: (value: string) => void
  signOut: () => void
}

const ApiKeyContext = createContext<ApiKeyContextValue | null>(null)

export function ApiKeyProvider({ children }: { children: ReactNode }) {
  const [apiKey, setApiKeyState] = useState(() => getStorageItem(STORAGE_KEY) || '')

  const value = useMemo<ApiKeyContextValue>(() => ({
    apiKey,
    setApiKey: (next) => {
      const value = String(next || '').trim()
      if (value) setStorageItem(STORAGE_KEY, value)
      else removeStorageItem(STORAGE_KEY)
      setApiKeyState(value)
    },
    signOut: () => {
      removeStorageItem(STORAGE_KEY)
      setApiKeyState('')
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
  return (getStorageItem(STORAGE_KEY) || '').trim()
}
