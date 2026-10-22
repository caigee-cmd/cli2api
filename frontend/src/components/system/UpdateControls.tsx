import { Button } from '@heroui/react'
import { ArrowCircleUp } from '@phosphor-icons/react'
import type { SystemUpdateController } from '@/hooks/useSystemUpdate'
import { useI18n } from '@/hooks/useI18n'

/** Current/latest versions, download → apply buttons and live status. */
export function UpdateControls({ update }: { update: SystemUpdateController }) {
  const { t } = useI18n()
  const { info, submitting, reloadIn, elapsed, readyToApply, busy, justUpdated, targetVersion, newerThanPrepared, updateStateText, canPrepare, canApply, canCancel, primaryLabel, statusHint, prepareUpdate, confirmUpdate, cancelPreparedUpdate } = update
  return (
    <>
        <div className="grid overflow-hidden rounded-lg bg-surface-secondary sm:grid-cols-[1fr_auto_1fr]">
          <div className="p-4">
            <div className="text-caption font-medium text-muted">{t('currentVersion')}</div>
            <div className="mono mt-2 text-section font-semibold">{info?.current_version || '—'}</div>
          </div>
          <div className="hidden items-center border-x border-separator px-4 text-muted sm:flex">
            <ArrowCircleUp size={18} />
          </div>
          <div className="border-t border-separator p-4 sm:border-t-0">
            <div className="text-caption font-medium text-muted">{t('latestVersion')}</div>
            <div className="mono mt-2 text-section font-semibold">{info?.next_version || '—'}</div>
          </div>
        </div>

        <div className="mt-5 flex flex-wrap items-center justify-end gap-3">
          {canCancel ? (
            <Button size="sm" variant="ghost" isDisabled={submitting} onPress={() => void cancelPreparedUpdate()}>
              {readyToApply ? t('discardPreparedImage') : t('cancelUpdate')}
            </Button>
          ) : null}
          <Button isDisabled={(!canPrepare && !canApply) || reloadIn != null} isPending={submitting || busy || reloadIn != null} onPress={() => void (canApply ? confirmUpdate() : prepareUpdate())}>
            <ArrowCircleUp size={16} />
            {primaryLabel}
          </Button>
        </div>
        {!info?.agent?.available && !busy ? <p className="mt-3 text-right text-caption text-muted">{t('updateUnavailableHint')}</p> : null}
        {info?.agent?.available && !info.agent.staged_update && !busy ? <p className="mt-3 text-right text-caption text-muted">{t('updateLegacyOneShotHint')}</p> : null}
        {readyToApply || busy || reloadIn != null || justUpdated ? (
          <div className={`mt-4 rounded-lg border px-3 py-3 ${readyToApply || justUpdated ? 'border-separator bg-surface-secondary' : 'border-separator bg-surface-secondary'}`} role="status" aria-live="polite">
            {updateStateText ? <p className="text-caption font-medium text-foreground">{updateStateText}{targetVersion ? ` · ${targetVersion}` : ''}{elapsed ? ` · ${t('updateElapsed', { seconds: elapsed })}` : ''}</p> : null}
            <p className="mt-1 text-caption leading-5 text-muted">{statusHint}</p>
            {newerThanPrepared ? <p className="mt-1 text-caption leading-5 text-warning">{t('updateNewerReleaseHint', { latest: info?.next_version || '' })}</p> : null}
            {info?.update?.error || info?.agent?.error ? <p className="mt-1 text-caption leading-5 text-danger">{info?.update?.error || info?.agent?.error}</p> : null}
          </div>
        ) : null}
    </>
  )
}
