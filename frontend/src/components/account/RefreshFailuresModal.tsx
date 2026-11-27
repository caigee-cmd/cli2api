import { useState } from 'react'
import { Button, Chip, Modal } from '@heroui/react'
import { ProviderMark } from '@/components/ProviderMark'
import { copyText } from '@/lib/clipboard'

type Translate = (key: string, vars?: Record<string, string | number>) => string

/** One failed account in a bulk refresh, already resolved to a display name. */
export type RefreshFailure = {
  accountId: string
  name: string
  provider: string
  region?: string
  status?: string
  kind?: string
  httpStatus?: number
  /** Cleaned, human-readable reason (no HTML page dump, no internal id). */
  message: string
}

function reasonBadge(failure: RefreshFailure): string {
  if (failure.httpStatus) return String(failure.httpStatus)
  if (failure.status) return failure.status
  if (failure.kind) return failure.kind
  return ''
}

/**
 * Lists every account that failed a bulk refresh with its own reason, so a
 * partial failure names the accounts instead of showing one opaque upstream
 * error. The toast count alone cannot answer "which ones and why".
 */
export function RefreshFailuresModal({
  failures,
  t,
  onClose,
}: {
  failures: RefreshFailure[] | null
  t: Translate
  onClose: () => void
}) {
  const [copied, setCopied] = useState(false)
  const open = Boolean(failures && failures.length)

  async function onCopy() {
    if (!failures?.length) return
    const lines = failures.map((failure) => {
      const badge = reasonBadge(failure)
      const head = `${failure.name} (${failure.provider}${failure.region ? `/${failure.region}` : ''})${badge ? ` [${badge}]` : ''}`
      return `${head}: ${failure.message || t('refreshFailuresUnknownReason')}`
    })
    const ok = await copyText(lines.join('\n'))
    if (!ok) return
    setCopied(true)
    window.setTimeout(() => setCopied(false), 1100)
  }

  return (
    <Modal.Root isOpen={open} onOpenChange={(next: boolean) => { if (!next) onClose() }}>
      <Modal.Backdrop variant="blur">
        <Modal.Container size="lg" scroll="inside">
          <Modal.Dialog>
            <Modal.Header className="px-5 pt-5">
              <Modal.Heading className="text-section font-semibold">
                {t('refreshFailuresTitle', { n: failures?.length ?? 0 })}
              </Modal.Heading>
              <p className="mt-1 text-caption font-normal leading-5 text-muted">{t('refreshFailuresHint')}</p>
            </Modal.Header>
            <Modal.Body className="space-y-2 px-5 pb-2">
              {(failures || []).map((failure) => {
                const badge = reasonBadge(failure)
                return (
                  <div key={failure.accountId} className="rounded-lg border border-divider bg-surface-secondary px-3 py-2">
                    <div className="flex flex-wrap items-center gap-2.5">
                      <ProviderMark provider={failure.provider} size={16} />
                      <span className="min-w-0 flex-1 truncate text-body">{failure.name}</span>
                      {badge ? <Chip size="sm" variant="soft" color="danger">{badge}</Chip> : null}
                    </div>
                    <p className="mt-1 break-words text-caption text-foreground/75">
                      {failure.message || t('refreshFailuresUnknownReason')}
                    </p>
                  </div>
                )
              })}
            </Modal.Body>
            <Modal.Footer className="justify-end px-5 pb-5">
              <Button variant="secondary" onPress={() => void onCopy()}>
                {copied ? t('copied') : t('copy')}
              </Button>
              <Button variant="secondary" onPress={onClose}>{t('close')}</Button>
            </Modal.Footer>
          </Modal.Dialog>
        </Modal.Container>
      </Modal.Backdrop>
    </Modal.Root>
  )
}
