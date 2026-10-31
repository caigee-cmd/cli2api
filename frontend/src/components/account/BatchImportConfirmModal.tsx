import { useEffect, useState } from 'react'
import { Button, Modal } from '@heroui/react'
import { X } from '@phosphor-icons/react'
import { FilterToggle } from '@/components/ui/FilterToggle'
import { ProviderMark } from '@/components/ProviderMark'
import type { ImportDetectResult, ImportRegion } from '@/lib/accountImportDetect'
import { accountProviderFamilyLabel } from '@/lib/provider'

type Props = {
  isOpen: boolean
  detection: ImportDetectResult | null
  fileName?: string
  isPending?: boolean
  t: (key: string, vars?: Record<string, string | number>) => string
  onClose: () => void
  onConfirm: (region: ImportRegion) => void
}

function warningText(code: string, t: Props['t']) {
  if (code === 'region_mixed') return t('batchImportRegionMixed')
  if (code === 'region_unknown') return t('batchImportRegionUnknown')
  if (code === 'unknown_format') return t('batchImportUnknownFormat')
  return ''
}

export function BatchImportConfirmModal({
  isOpen,
  detection,
  fileName,
  isPending,
  t,
  onClose,
  onConfirm,
}: Props) {
  const [region, setRegion] = useState<ImportRegion>('cn')

  useEffect(() => {
    if (!detection) return
    if (detection.region === 'cn' || detection.region === 'global') {
      setRegion(detection.region)
    } else {
      setRegion('cn')
    }
  }, [detection])

  const supported = Boolean(detection?.batchImportSupported)
  const provider = detection?.provider
  const formatLabel = detection?.format || t('batchImportFormatUnknown')
  const providerLabel = provider
    ? accountProviderFamilyLabel(provider, t)
    : t('batchImportProviderUnknown')

  const warnings = (detection?.warnings || [])
    .map((code) => warningText(code, t))
    .filter(Boolean)

  return (
    <Modal.Root
      isOpen={isOpen}
      onOpenChange={(open: boolean) => {
        if (!open && !isPending) onClose()
      }}
    >
      <Modal.Backdrop variant="blur" isDismissable={!isPending}>
        <Modal.Container placement="center" size="md" scroll="inside" className="mx-3 w-full max-w-[min(100%,32rem)] sm:mx-0">
          <Modal.Dialog>
            <Modal.Header className="items-start justify-between gap-4 px-6 pt-6">
              <div className="min-w-0">
                <Modal.Heading className="text-section font-semibold">{t('batchImportConfirmTitle')}</Modal.Heading>
                <p className="mt-1 break-all text-caption leading-5 text-muted">
                  {fileName
                    ? t('batchImportConfirmHint', { file: fileName })
                    : t('batchImportConfirmHintNoFile')}
                </p>
              </div>
              <Modal.CloseTrigger
                isDisabled={isPending}
                aria-label={t('close')}
                className="grid size-8 shrink-0 place-items-center rounded-lg text-muted transition-colors hover:bg-surface-secondary hover:text-foreground"
              >
                <X size={16} />
              </Modal.CloseTrigger>
            </Modal.Header>
            <Modal.Body className="space-y-4 px-6 pb-2">
              <div className="rounded-xl bg-surface-secondary px-3.5 py-3">
                <div className="flex items-center gap-2.5">
                  {provider ? <ProviderMark provider={provider} size={18} /> : null}
                  <div className="min-w-0">
                    <div className="text-body font-medium text-foreground">{providerLabel}</div>
                    <div className="mono mt-0.5 truncate text-caption text-muted">{formatLabel}</div>
                  </div>
                </div>
              </div>

              {supported ? (
                <div className="space-y-2">
                  <div className="text-body font-medium text-muted">{t('batchImportRegion')}</div>
                  <FilterToggle
                    value={region}
                    onChange={(next) => {
                      if (next === 'cn' || next === 'global') setRegion(next)
                    }}
                    ariaLabel={t('batchImportRegion')}
                    options={[
                      { id: 'cn', label: t('accountTypeWorkBuddyCN') },
                      { id: 'global', label: t('accountTypeWorkBuddyGlobal') },
                    ]}
                  />
                  {detection?.regionAmbiguous ? (
                    <p className="text-caption leading-5 text-muted">{t('batchImportRegionPickHint')}</p>
                  ) : null}
                </div>
              ) : (
                <p className="text-body leading-6 text-muted">{t('batchImportProviderUnsupported')}</p>
              )}

              {warnings.length ? (
                <ul className="space-y-1 text-caption leading-5 text-muted">
                  {warnings.map((line) => (
                    <li key={line}>{line}</li>
                  ))}
                </ul>
              ) : null}
            </Modal.Body>
            <Modal.Footer className="flex-col-reverse gap-2 px-6 pb-6 sm:flex-row sm:justify-end">
              <Button size="sm" variant="ghost" className="w-full sm:w-auto" isDisabled={isPending} onPress={onClose}>
                {supported ? t('cancel') : t('close')}
              </Button>
              {supported ? (
                <Button
                  size="sm"
                  className="w-full sm:w-auto"
                  isPending={isPending}
                  onPress={() => onConfirm(region)}
                >
                  {t('batchImportConfirmAction')}
                </Button>
              ) : null}
            </Modal.Footer>
          </Modal.Dialog>
        </Modal.Container>
      </Modal.Backdrop>
    </Modal.Root>
  )
}
