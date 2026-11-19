import { useOverview } from '@/hooks/useOverview'

/**
 * Single source of truth for whether the cross-provider model pool feature is
 * enabled. The flag already travels with the overview summary, so both the
 * sidebar navigation and the route guard read the same value without adding
 * another API call or duplicated state.
 *
 * `loading` is returned so callers can stay conservative while the summary is
 * in flight: navigation is hidden until the flag is known.
 */
export function useCrossProviderPool() {
  const { overview, loading } = useOverview()
  const enabled = Boolean(overview?.proxy?.cross_provider_model_pool)
  return { enabled, loading }
}
