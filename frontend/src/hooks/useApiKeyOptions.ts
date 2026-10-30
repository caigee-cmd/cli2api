import { useEffect, useMemo, useState } from 'react'
import { fetchAPIKeys, type APIKeyRecord } from '@/api/keys'
import type { FilterSelectOption } from '@/components/ui/FilterSelect'

/**
 * Named console API keys as filter options (id -> name). Usage and logs filter
 * request_logs.api_key_id, so the option id is the key id and the label is the
 * operator-facing name. Deleted keys keep their historical rows, so a value
 * that is no longer in the list still resolves to itself.
 */
export function useApiKeyOptions() {
  const [keys, setKeys] = useState<APIKeyRecord[]>([])

  useEffect(() => {
    let cancelled = false
    void fetchAPIKeys()
      .then((result) => { if (!cancelled) setKeys(result.data || []) })
      .catch(() => { /* the filter just stays empty */ })
    return () => { cancelled = true }
  }, [])

  const nameById = useMemo(() => {
    const names = new Map<string, string>()
    for (const key of keys) names.set(key.id, key.name || key.id)
    return names
  }, [keys])

  const options = useMemo<FilterSelectOption[]>(
    () => keys.map((key) => ({ id: key.id, label: key.name || key.id })),
    [keys],
  )

  return { keys, nameById, options }
}
