import { Button, Modal } from '@heroui/react'
import { X } from '@phosphor-icons/react'
import type { AccountBundle } from '@/api/overview'

type Props = {
  isOpen: boolean
  bundle: AccountBundle | null
  fileName?: string
  isPending?: boolean
  t: (key: string, vars?: Record<string, string | number>) => string
  onClose: () => void
  onConfirm: () => void
}

/** Confirmation dialog for a full console account migration bundle. */
export function AccountBundleImportModal({
  isOpen,
  bundle,
  fileName,
  isPending,
  t,
  onClose,
  onConfirm,
}: Props) {
  const count = bundle?.accounts?.length ?? 0
  const withCredentials = bundle?.accounts?.filter((entry) => Boolean(entry.credential)).length ?? 0

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
                <Modal.Heading className="text-section font-semibold">{t('bundleImportConfirmTitle')}</Modal.Heading>
                <p className="mt-1 break-all text-caption leading-5 text-muted">
                  {fileName ? t('bundleImportConfirmHint', { file: fileName }) : t('bundleImportConfirmHintNoFile')}
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
            <Modal.Body className="space-y-3 px-6 pb-2">
              <div className="rounded-xl bg-surface-secondary px-3.5 py-3 text-body leading-6 text-foreground">
                <div className="mono text-lg font-semibold">{count}</div>
                <div className="text-muted">{t('bundleImportAccountCount')}</div>
              </div>
              <p className="text-body leading-6 text-muted">{t('bundleImportCountDetail', { credentials: withCredentials })}</p>
              <p className="text-body leading-6 text-muted">{t('bundleImportAdditiveHint')}</p>
              <p className="text-caption leading-5 text-muted">{t('bundleExportHint')}</p>
            </Modal.Body>
            <Modal.Footer className="flex-col-reverse gap-2 px-6 pb-6 sm:flex-row sm:justify-end">
              <Button size="sm" variant="ghost" className="w-full sm:w-auto" isDisabled={isPending} onPress={onClose}>
                {t('cancel')}
              </Button>
              <Button
                size="sm"
                className="w-full sm:w-auto"
                isPending={isPending}
                isDisabled={count === 0}
                onPress={onConfirm}
              >
                {t('bundleImportConfirmAction')}
              </Button>
            </Modal.Footer>
          </Modal.Dialog>
        </Modal.Container>
      </Modal.Backdrop>
    </Modal.Root>
  )
}
