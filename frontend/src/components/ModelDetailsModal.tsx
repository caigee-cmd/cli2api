import { Chip, Modal } from '@heroui/react'
import { X } from '@phosphor-icons/react'
import type { ReactNode } from 'react'
import type { ModelInfo } from '@/api/types'
import { modelIsFree } from '@/lib/format'
import { accountProviderLabel, tieredTraeCaps } from '@/lib/provider'

type Translate = (key: string, vars?: Record<string, string | number>) => string

type Props = {
  model: ModelInfo | null
  t: Translate
  onClose: () => void
}

function formatTokens(value?: number) {
  if (!value) return '—'
  if (value >= 1_000_000) return `${(value / 1_000_000).toString().replace(/\.0$/, '')}M`
  if (value >= 1000) return `${Math.round(value / 1000)}k`
  return String(value)
}

function DetailRow({ label, value }: { label: string; value: ReactNode }) {
  return (
    <div className="flex items-start justify-between gap-4 py-1.5">
      <span className="text-caption text-muted">{label}</span>
      <span className="min-w-0 text-right text-caption font-medium">{value}</span>
    </div>
  )
}

export function ModelDetailsModal({ model, t, onClose }: Props) {
  if (!model) return null
  // Show exactly the resolved, catalog-declared levels the gateway accepts.
  // Do not invent missing tiers from a global vocabulary.
  const options = model.reasoning_options || []
  const windowDev = model.catalog_context_length || model.default_context_length || model.context_length || 0
  const windowMax = model.catalog_context_length_max
  // Requests always use the model's own largest declared window, so the
  // details view shows that single effective value rather than a toggle.
  const effectiveWindow = windowMax && windowMax > windowDev ? windowMax : windowDev
  const free = modelIsFree(model)
  const provider = String(model.provider || model.owned_by || 'qoder').trim().toLowerCase()
  const region = String(model.region || model.regions?.[0] || '').trim().toLowerCase()
  const providerLabel = accountProviderLabel(provider, region || undefined, t)
  const traeTier = provider === 'trae' ? tieredTraeCaps(model) : undefined
  const imageText = model.supports_images === true
    ? t('imageInputDeclared')
    : model.supports_images === false
      ? t('imageInputUnsupported')
      : t('imageInputNotDeclared')
  const promptMax = traeTier?.prompt_max_tokens || model.prompt_max_tokens
  const outputMax = traeTier?.max_output_tokens || model.max_output_tokens
  return (
    <Modal.Root isOpen onOpenChange={(next: boolean) => { if (!next) onClose() }}>
      <Modal.Backdrop variant="blur">
        <Modal.Container size="md" scroll="inside">
          <Modal.Dialog>
            <Modal.Header className="items-start justify-between gap-4 px-5 pt-5">
              <div className="min-w-0">
                <div className="flex flex-wrap items-center gap-2">
                  <div className="truncate text-section">{model.display_name || model.id}</div>
                  {free ? <Chip size="sm" variant="soft" color="success">{t('modelFree')}</Chip> : null}
                </div>
                <div className="mono mt-1 truncate text-caption text-muted">{model.id}</div>
              </div>
              <Modal.CloseTrigger aria-label={t('close')} className="grid size-8 shrink-0 place-items-center rounded-lg text-muted hover:bg-surface-secondary">
                <X size={16} />
              </Modal.CloseTrigger>
            </Modal.Header>
            <Modal.Body className="space-y-3 px-5 pb-5">
              <section className="divide-y divide-separator rounded-lg border border-separator px-3 py-1">
                <DetailRow label={t('providerCol')} value={providerLabel} />

                <DetailRow label={t('contextWindowCol')} value={<span className="mono">{formatTokens(effectiveWindow)}</span>} />
                {promptMax ? <DetailRow label={t('promptMaxTokens')} value={<span className="mono">{formatTokens(promptMax)}</span>} /> : null}
                {outputMax ? <DetailRow label={t('maxOutputTokens')} value={<span className="mono">{formatTokens(outputMax)}</span>} /> : null}
                <DetailRow label={t('imageInput')} value={imageText} />
              </section>

              <section>
                <div className="mb-1 text-caption font-medium text-muted">{t('reasoningLevels')}</div>
                {options.length ? (
                  <div className="flex flex-wrap gap-1.5">
                    {options.map((level) => (
                      <Chip key={level} size="sm" variant="soft" color="default">
                        <span className="mono">{level}</span>
                      </Chip>
                    ))}
                  </div>
                ) : (
                  <div className="text-caption text-muted">
                    {model.reasoning_type ? t('reasoningFixed', { type: model.reasoning_type }) : t('noReasoningLevels')}
                  </div>
                )}
              </section>
            </Modal.Body>
          </Modal.Dialog>
        </Modal.Container>
      </Modal.Backdrop>
    </Modal.Root>
  )
}

export { formatTokens }
