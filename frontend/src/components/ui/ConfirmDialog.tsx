import { AlertDialog, Button } from '@heroui/react'

export function ConfirmDialog({
  isOpen,
  title,
  description,
  confirmLabel,
  cancelLabel,
  closeLabel,
  isPending,
  status = 'danger',
  confirmVariant = 'danger',
  onClose,
  onConfirm,
}: {
  isOpen: boolean
  title: string
  description: string
  confirmLabel: string
  cancelLabel: string
  closeLabel: string
  isPending?: boolean
  status?: 'default' | 'accent' | 'success' | 'warning' | 'danger'
  confirmVariant?: 'danger' | 'primary'
  onClose: () => void
  onConfirm: () => void
}) {
  return (
    <AlertDialog.Root isOpen={isOpen} onOpenChange={(open: boolean) => { if (!open && !isPending) onClose() }}>
      <AlertDialog.Backdrop variant="blur" isDismissable={!isPending}>
        <AlertDialog.Container placement="center" size="sm">
          <AlertDialog.Dialog>
            <AlertDialog.Header className="items-start justify-between gap-4">
              <div className="flex items-start gap-3">
                <AlertDialog.Icon status={status} />
                <div>
                  <AlertDialog.Heading>{title}</AlertDialog.Heading>
                </div>
              </div>
              <AlertDialog.CloseTrigger isDisabled={isPending} aria-label={closeLabel} />
            </AlertDialog.Header>
            <AlertDialog.Body>
              <p className="text-sm leading-6 text-muted">{description}</p>
            </AlertDialog.Body>
            <AlertDialog.Footer>
              <Button size="sm" variant="ghost" isDisabled={isPending} onPress={onClose}>{cancelLabel}</Button>
              <Button size="sm" variant={confirmVariant} isPending={isPending} onPress={onConfirm}>{confirmLabel}</Button>
            </AlertDialog.Footer>
          </AlertDialog.Dialog>
        </AlertDialog.Container>
      </AlertDialog.Backdrop>
    </AlertDialog.Root>
  )
}
