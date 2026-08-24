export function isQoderGlobalProvider(provider?: string) {
  const value = String(provider || '').toLowerCase()
  return !value || value === 'qoder' || value === 'qoder-global'
}
