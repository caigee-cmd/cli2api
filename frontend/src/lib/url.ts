export function originBase() {
  return `${location.protocol}//${location.host}`
}

export function absUrl(pathOrUrl?: string | null) {
  if (!pathOrUrl) return originBase()
  if (/^https?:\/\//i.test(pathOrUrl)) return pathOrUrl
  if (pathOrUrl.startsWith('/')) return `${originBase()}${pathOrUrl}`
  return `${originBase()}/${pathOrUrl}`
}
