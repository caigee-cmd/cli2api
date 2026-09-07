export type VersionHistoryRelease = {
  tag_name: string
  name?: string
  body?: string
  published_at?: string
  html_url?: string
}

function dedupeReleases(releases: VersionHistoryRelease[]) {
  const seen = new Set<string>()
  const items: VersionHistoryRelease[] = []
  for (const release of releases) {
    const tag = release.tag_name?.trim()
    if (!tag || seen.has(tag)) continue
    seen.add(tag)
    items.push({ ...release, tag_name: tag })
  }
  return items
}

export function buildVersionHistory(
  currentVersion: string,
  nextVersion: string | undefined,
  latest: VersionHistoryRelease | undefined,
  recent: VersionHistoryRelease[] | undefined,
  rollback: VersionHistoryRelease[] | undefined,
) {
  if (recent?.length) return dedupeReleases(recent)
  return dedupeReleases([
    ...(latest ? [latest] : nextVersion ? [{ tag_name: nextVersion }] : []),
    ...(currentVersion ? [{ tag_name: currentVersion }] : []),
    ...(rollback || []),
  ])
}
