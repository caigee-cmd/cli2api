import { Accordion, Button, Chip } from '@heroui/react'
import { ReleaseNotes } from '@/components/ReleaseNotes'
import { extractReleaseNotes } from '@/lib/releaseNotes'
import { useI18n } from '@/hooks/useI18n'
import type { Lang } from '@/i18n/messages'
import type { VersionHistoryRelease } from '@/lib/versionHistory'

function formatReleaseDate(value: string | undefined, lang: Lang) {
  if (!value) return ''
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return ''
  return date.toLocaleDateString(lang === 'zh' ? 'zh-CN' : 'en-US', { year: 'numeric', month: 'short', day: 'numeric' })
}

export function VersionHistory({
  releases,
  currentVersion,
  nextVersion,
  rollbackTags,
  canRollback,
  submitting,
  onRestore,
}: {
  releases: VersionHistoryRelease[]
  currentVersion: string
  nextVersion?: string
  rollbackTags: string[]
  canRollback: boolean
  submitting: boolean
  onRestore: (version: string) => void
}) {
  const { t, lang } = useI18n()
  if (!releases.length) return null

  const featured = nextVersion && nextVersion !== currentVersion ? nextVersion : currentVersion
  const defaultExpanded = releases.some((release) => release.tag_name === featured)
    ? featured
    : releases[0]?.tag_name
  const rollbackSet = new Set(rollbackTags)

  return (
    <div className="mt-5">
      <div className="mb-2">
        <p className="text-xs font-medium text-foreground">{t('versionHistory')}</p>
        <p className="mt-1 text-xs leading-5 text-muted">{t('versionHistoryHint')}</p>
      </div>
      <Accordion className="rounded-lg border border-separator" hideSeparator defaultExpandedKeys={defaultExpanded ? [defaultExpanded] : []}>
        {releases.map((release) => {
          const tag = release.tag_name
          const isCurrent = tag === currentVersion
          const isLatest = Boolean(nextVersion && tag === nextVersion && nextVersion !== currentVersion)
          const canRestore = canRollback && rollbackSet.has(tag) && !isCurrent
          const published = formatReleaseDate(release.published_at, lang)
          return (
            <Accordion.Item key={tag} id={tag}>
              <Accordion.Heading>
                <Accordion.Trigger className="items-center gap-3 px-3 py-2.5 text-left">
                  <span className="flex min-w-0 flex-1 items-center gap-2">
                    <span className="mono text-sm font-semibold">{tag}</span>
                    {isCurrent ? <Chip size="sm" variant="soft" color="success">{t('versionCurrentBadge')}</Chip> : null}
                    {isLatest ? <Chip size="sm" variant="soft" color="warning">{t('versionLatestBadge')}</Chip> : null}
                    {published ? <span className="truncate text-xs text-muted">{published}</span> : null}
                  </span>
                  <Accordion.Indicator />
                </Accordion.Trigger>
              </Accordion.Heading>
              <Accordion.Panel>
                <Accordion.Body className="px-3 pb-3">
                  <div className="release-notes-box release-notes-box--compact">
                    <ReleaseNotes markdown={extractReleaseNotes(release.body, lang)} emptyLabel={t('updateNoNotes')} />
                  </div>
                  {canRestore ? (
                    <div className="mt-3 flex justify-end">
                      <Button size="sm" variant="ghost" isDisabled={submitting} onPress={() => onRestore(tag)}>
                        {t('restoreThisVersion')}
                      </Button>
                    </div>
                  ) : null}
                </Accordion.Body>
              </Accordion.Panel>
            </Accordion.Item>
          )
        })}
      </Accordion>
    </div>
  )
}
