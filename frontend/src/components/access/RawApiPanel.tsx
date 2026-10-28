import { CopyIcon } from '@/components/motion/CopyIcon'
import { EmptyPanel } from '@/components/ui/EmptyPanel'
import { PageAlert } from '@/components/ui/PageAlert'
import { Button, Chip, Skeleton, TextArea } from '@heroui/react'
import { BracketsCurly, CheckCircle, Clock, PaperPlaneTilt } from '@phosphor-icons/react'
import { useI18n } from '@/hooks/useI18n'

type RequestState = 'idle' | 'loading' | 'success' | 'error'

export function RawApiPanel({
  chatPath,
  prompt,
  onPromptChange,
  onTest,
  requestState,
  elapsedMs,
  output,
  responseStatus,
  curl,
  copiedCurl,
  onCopyCurl,
  canSend,
}: {
  chatPath: string
  prompt: string
  onPromptChange: (value: string) => void
  onTest: () => void
  requestState: RequestState
  elapsedMs: number | null
  output: string
  responseStatus: string
  curl: string
  copiedCurl: boolean
  onCopyCurl: () => void
  canSend: boolean
}) {
  const { t } = useI18n()

  return (
    <div className="space-y-4">
      <p className="px-1 text-caption leading-5 text-muted">{t('nativeApiHint')}</p>

      <div className="overflow-hidden rounded-3xl bg-surface shadow-surface">
        <div className="grid xl:grid-cols-[minmax(440px,.92fr)_minmax(0,1.08fr)]">
          <div className="xl:border-r xl:border-separator/50">
            <div className="px-5 py-4 sm:px-6">
              <h3 className="font-semibold">{t('requestBuilder')}</h3>
              <p className="mt-0.5 text-caption text-muted">POST {chatPath}</p>
            </div>

            <div className="space-y-5 px-5 pb-5 sm:px-6 sm:pb-6">
              <div className="space-y-3">
                <div className="flex items-center justify-between gap-4">
                  <div className="text-body font-medium text-muted">{t('prompt')}</div>
                  <span className="mono text-caption text-muted">{prompt.length}</span>
                </div>
                <TextArea
                  fullWidth
                  rows={9}
                  className="min-h-56 resize-y px-4 py-3 text-body leading-6"
                  value={prompt}
                  onChange={(event) => onPromptChange(event.target.value)}
                  placeholder={t('promptPlaceholder')}
                  aria-label={t('prompt')}
                />
                <p className="text-caption leading-5 text-muted">{t('promptHelper')}</p>
              </div>

              <Button
                fullWidth
                isPending={requestState === 'loading'}
                isDisabled={!canSend}
                onPress={onTest}
              >
                <PaperPlaneTilt size={16} />
                {requestState === 'loading' ? t('requesting') : t('sendRequest')}
              </Button>
            </div>
          </div>

          <div
            className="flex min-h-[480px] min-w-0 flex-col bg-surface-secondary/40"
            aria-live="polite"
            aria-busy={requestState === 'loading'}
          >
            <div className="flex min-h-14 items-center justify-between gap-4 px-5 py-3.5 sm:px-6">
              <div>
                <h3 className="font-semibold">{t('responseInspector')}</h3>
                <p className="mt-0.5 text-caption text-muted">{t('responseInspectorHint')}</p>
              </div>
              <div className="flex items-center gap-2">
                {elapsedMs !== null ? (
                  <span className="mono flex items-center gap-1.5 text-caption text-muted">
                    <Clock size={12} />
                    {elapsedMs} ms
                  </span>
                ) : null}
                <Chip
                  size="sm"
                  variant="soft"
                  color={requestState === 'success' ? 'success' : requestState === 'error' ? 'danger' : undefined}
                >
                  {responseStatus}
                </Chip>
              </div>
            </div>

            <div className="min-h-0 flex-1 px-5 pb-5 sm:px-6 sm:pb-6">
              {requestState === 'idle' ? (
                <EmptyPanel
                  className="min-h-72"
                  icon={<BracketsCurly size={21} />}
                  title={t('responseEmptyTitle')}
                  hint={t('responseEmptyHint')}
                />
              ) : requestState === 'loading' ? (
                <div className="space-y-3 pt-1">
                  <Skeleton className="h-3 w-32 rounded-lg" />
                  <Skeleton className="h-3 w-full rounded-lg" />
                  <Skeleton className="h-3 w-[88%] rounded-lg" />
                  <Skeleton className="h-3 w-[72%] rounded-lg" />
                  <Skeleton className="mt-7 h-3 w-[92%] rounded-lg" />
                  <Skeleton className="h-3 w-[64%] rounded-lg" />
                </div>
              ) : requestState === 'error' ? (
                <div className="space-y-3">
                  <PageAlert status="danger" title={t('requestFailed')} description={output} />
                </div>
              ) : (
                <div>
                  <div className="mb-4 flex items-center gap-2 text-caption font-medium text-success">
                    <CheckCircle size={15} weight="fill" />
                    {t('responseReceived')}
                  </div>
                  <pre className="mono overflow-x-auto whitespace-pre-wrap break-words text-caption leading-6 text-muted">
                    {output}
                  </pre>
                </div>
              )}
            </div>
          </div>
        </div>
      </div>

      <section className="overflow-hidden rounded-3xl bg-surface shadow-surface">
        <div className="flex items-center justify-between gap-4 px-5 py-3.5">
          <div className="min-w-0">
            <div className="text-body font-semibold">{t('curlExample')}</div>
            <div className="mt-0.5 truncate text-caption text-muted">{t('curlGeneratedHint')}</div>
          </div>
          <Button size="sm" variant="ghost" onPress={onCopyCurl}>
            <CopyIcon copied={copiedCurl} />
            {copiedCurl ? t('copied') : t('copy')}
          </Button>
        </div>
        <pre className="mono max-h-80 overflow-auto whitespace-pre-wrap px-5 pb-5 text-caption leading-6 text-muted">
          {curl}
        </pre>
      </section>
    </div>
  )
}
