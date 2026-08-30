import { Chip, Modal } from '@heroui/react'
import { X } from '@phosphor-icons/react'
import type { ModelInfo } from '@/api/types'

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

export function ModelDetailsModal({ model, t, onClose }: Props) {
  if (!model) return null
  const options = model.reasoning_options || []
  const windowDev = model.catalog_context_length || model.default_context_length || model.context_length
  const windowMax = model.catalog_context_length_max
  return (
    <Modal.Root isOpen onOpenChange={(next: boolean) => { if (!next) onClose() }}>
      <Modal.Backdrop variant="blur">
        <Modal.Container size="md" scroll="inside">
          <Modal.Dialog>
            <Modal.Header className="items-start justify-between gap-4 px-5 pt-5">
              <div>
                <div className="text-base font-semibold tracking-[-0.015em]">{model.display_name || model.id}</div>
                <div className="mono mt-1 text-[11px] text-muted">{model.id}</div>
              </div>
              <Modal.CloseTrigger aria-label={t('close')} className="grid size-8 place-items-center rounded-lg text-muted hover:bg-surface-secondary">
                <X size={16} />
              </Modal.CloseTrigger>
            </Modal.Header>
            <Modal.Body className="space-y-4 px-5 pb-5">
              <section>
                <div className="text-xs font-medium text-muted">{t('contextWindowCol')}</div>
                <div className="mt-1 text-sm">{formatTokens(windowDev)}{windowMax && windowMax !== windowDev ? ` → ${formatTokens(windowMax)}` : ''}</div>
                {model.prompt_max_tokens ? <div className="mt-1 text-[11px] text-muted">{t('promptMaxTokens')}: {formatTokens(model.prompt_max_tokens)}</div> : null}
                {model.max_output_tokens ? <div className="text-[11px] text-muted">{t('maxOutputTokens')}: {formatTokens(model.max_output_tokens)}</div> : null}
              </section>
              <section>
                <div className="text-xs font-medium text-muted">{t('reasoningLevels')}</div>
                {options.length ? (
                  <div className="mt-2 flex flex-wrap gap-1.5">
                    {options.map((level) => (
                      <Chip key={level} size="sm" variant="soft" color={level === (model.reasoning_effort || model.reasoning_default) ? 'success' : 'default'}>
                        {level}{level === model.reasoning_default ? ` · ${t('defaultValue')}` : ''}{level === model.reasoning_effort && level !== model.reasoning_default ? ` · ${t('custom')}` : ''}
                      </Chip>
                    ))}
                  </div>
                ) : (
                  <div className="mt-1 text-sm text-muted">
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
