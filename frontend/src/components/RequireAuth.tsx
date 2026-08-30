import { Navigate, Outlet, useLocation } from 'react-router-dom'
import { useOverview } from '@/hooks/useOverview'
import { useApiKey } from '@/hooks/useApiKey'
import { isUnauthorized } from '@/api/client'

export function RequireAuth() {
  const { overview, error } = useOverview()
  const { apiKey } = useApiKey()
  const location = useLocation()

  if (!apiKey) {
    return <Navigate to="/login" replace state={{ from: location.pathname }} />
  }

  if (!overview && isUnauthorized(error)) {
    return <Navigate to="/login" replace state={{ from: location.pathname }} />
  }

  return <Outlet />
}
