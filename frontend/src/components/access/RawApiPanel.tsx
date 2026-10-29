import { CopyIcon } from '@/components/motion/CopyIcon'
import { EmptyPanel } from '@/components/ui/EmptyPanel'
import { FilterToggle } from '@/components/ui/FilterToggle'
import { PageAlert } from '@/components/ui/PageAlert'
import { CompactSelect, type ComposerOption } from '@/components/access/ChatComposer'
import type { RawApiFormat } from '@/api/overview'
import { Button, Chip, Skeleton, TextArea } from '@heroui/react'
import { CheckCircle, Clock, PaperPlaneTilt } from '@phosphor-icons/react'
import { useI18n } from '@/hooks/useI18n'

type RequestState = 'idle' | 'loading' | 'success' | 'error'

export function RawApiPanel({
  format,
  onFormatChange,
  requestPath,
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
  accountValue,
  onAccountChange,
  accountOptions,
  modelValue,
  onModelChange,
  modelOptions,
  modelsLoading,
  disabled,
}: {
  format: RawApiFormat
  onFormatChange: (value: RawApiFormat) => void
  requestPath: string
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
  accountValue: string
  onAccountChange: (value: string) => void
  accountOptions: ComposerOption[]
  modelValue: string
  onModelChange: (value: string) => void
  modelOptions: ComposerOption[]
  modelsLoading?: boolean
  disabled?: boolean
}) {
  const { t } = useI18n()

  return (
    <div className="space-y-4">
      <p className="px-1 text-caption leading-5 text-muted">{t('rawFormatHint')}</p>

      <div className="access-raw-panel overflow-hidden rounded-2xl">
        <div className="grid xl:grid-cols-[minmax(420px,.92fr)_minmax(0,1.08fr)]">
          <div className="xl:border-r xl:border-[color-mix(in_oklab,var(--foreground)_6%,transparent)]">
            <div className="px-4 py-3.5 sm:px-5">
              <h3 className="text-body font-semibold">{t('requestBuilder')}</h3>
              <p className="mt-0.5 text-caption text-muted">POST {requestPath}</p>
            </div>

            <div className="space-y-4 px-4 pb-4 sm:px-5 sm:pb-5">
              <div className="space-y-2">
                <div className="text-caption font-medium text-muted">{t('rawFormatLabel')}</div>
                <FilterToggle
                  className="access-format-chips"
                  ariaLabel={t('rawFormatLabel')}
                  value={format}
                  onChange={(value) => onFormatChange(value as RawApiFormat)}
                  options={[
                    { id: 'chat', label: t('rawFormatChat') },
                    { id: 'messages', label: t('rawFormatMessages') },
                    { id: 'responses', label: t('rawFormatResponses') },
                  ]}
                />
              </div>

              {/* Same CompactSelect sizing / tone as Chat composer chips */}
              <div className="flex flex-wrap items-center gap-1">
                <CompactSelect
                  ariaLabel={t('account')}
                  value={accountValue}
                  onChange={onAccountChange}
                  options={accountOptions}
                  isDisabled={disabled || requestState === 'loading'}
                  tone="muted"
                  className="w-auto max-w-[8.5rem] min-w-0"
                />
                <CompactSelect
                  ariaLabel={t('model')}
                  value={modelValue}
                  onChange={onModelChange}
                  placeholder={modelsLoading ? t('requesting') : t('model')}
                  options={modelOptions}
                  isDisabled={disabled || requestState === 'loading' || modelsLoading || !modelOptions.length}
                  className="w-auto max-w-[10.5rem] min-w-0"
                />
              </div>

              <div className="space-y-2.5">
                <div className="flex items-center justify-between gap-4">
                  <div className="text-caption font-medium text-muted">{t('prompt')}</div>
                  <span className="mono text-caption text-muted">{prompt.length}</span>
                </div>
                <TextArea
                  fullWidth
                  rows={9}
                  className="min-h-52 resize-y rounded-xl border-0 bg-[var(--field-background)] px-3.5 py-3 text-body leading-[1.65] shadow-[inset_0_0_0_1px_color-mix(in_oklab,var(--field-border)_78%,var(--foreground))] focus-visible:ring-0"
                  value={prompt}
                  onChange={(event) => onPromptChange(event.target.value)}
                  placeholder={t('promptPlaceholder')}
                  aria-label={t('prompt')}
                  disabled={disabled || requestState === 'loading'}
                />
                <p className="text-caption leading-5 text-muted/70">{t('promptHelper')}</p>
              </div>

              <Button
                fullWidth
                isPending={requestState === 'loading'}
                isDisabled={!canSend || disabled}
                onPress={onTest}
              >
                <PaperPlaneTilt size={15} />
                {requestState === 'loading' ? t('requesting') : t('sendRequest')}
              </Button>
            </div>
          </div>

          <div
            className="access-raw-panel__inspector flex min-h-[440px] min-w-0 flex-col"
            aria-live="polite"
            aria-busy={requestState === 'loading'}
          >
            <div className="flex min-h-12 items-center justify-between gap-4 px-4 py-3 sm:px-5">
              <div>
                <h3 className="text-body font-semibold">{t('responseInspector')}</h3>
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

            <div className="min-h-0 flex-1 px-4 pb-4 sm:px-5 sm:pb-5">
              {requestState === 'idle' ? (
                <EmptyPanel
                  className="min-h-64"
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
                  <div className="mb-3 flex items-center gap-2 text-caption font-medium text-success">
                    <CheckCircle size={14} weight="fill" />
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

      <section className="access-raw-panel overflow-hidden rounded-2xl">
        <div className="flex items-center justify-between gap-4 px-4 py-3 sm:px-5">
          <div className="min-w-0">
            <div className="text-body font-semibold">{t('curlExample')}</div>
            <div className="mt-0.5 truncate text-caption text-muted">{t('curlGeneratedHint')}</div>
          </div>
          <Button size="sm" variant="ghost" onPress={onCopyCurl}>
            <CopyIcon copied={copiedCurl} />
            {copiedCurl ? t('copied') : t('copy')}
          </Button>
        </div>
        <pre className="mono max-h-72 overflow-auto whitespace-pre-wrap px-4 pb-4 text-caption leading-6 text-muted sm:px-5 sm:pb-5">
          {curl}
        </pre>
      </section>
    </div>
  )
}
