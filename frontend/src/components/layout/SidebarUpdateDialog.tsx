import { Button, Modal } from '@heroui/react'
import { X } from '@phosphor-icons/react'
import { PageAlert } from '@/components/ui/PageAlert'
import { ReleaseNotes } from '@/components/ReleaseNotes'
import { UpdateControls } from '@/components/system/UpdateControls'
import { extractReleaseNotes } from '@/lib/releaseNotes'
import type { SystemUpdateInfo } from '@/api/system'
import type { SystemUpdateController } from '@/hooks/useSystemUpdate'

type Props = {
  open: boolean
  update: SystemUpdateController
  release?: SystemUpdateInfo['release']
  version: string
  lang: 'zh' | 'en'
  labels: {
    versionUpdate: string
    close: string
    releaseNotes: string
    emptyNotes: string
    goToUpdate: string
  }
  onClose: () => void
  onGoToUpdate: () => void
}

/** The update dialog is lazy: it is only downloaded when the sidebar entry is opened. */
export function SidebarUpdateDialog({ open, update, release, version, lang, labels, onClose, onGoToUpdate }: Props) {
  return (
    <Modal.Root isOpen={open} onOpenChange={(next) => { if (!next) onClose() }}>
      <Modal.Backdrop variant="blur">
        <Modal.Container placement="center" size="lg" scroll="inside">
          <Modal.Dialog>
            <Modal.Header className="items-start justify-between gap-4">
              <div>
                <Modal.Heading className="text-section font-semibold">{labels.versionUpdate}</Modal.Heading>
                <p className="mono mt-1 text-caption text-muted">{release?.tag_name || version}</p>
              </div>
              <Modal.CloseTrigger aria-label={labels.close} className="grid size-8 place-items-center rounded-lg text-muted hover:bg-surface-secondary">
                <X size={16} />
              </Modal.CloseTrigger>
            </Modal.Header>
            <Modal.Body className="space-y-4">
              {update.error ? <PageAlert title={update.error} /> : null}
              <UpdateControls update={update} />
              <div className="text-caption font-medium text-muted">{labels.releaseNotes}</div>
              <ReleaseNotes markdown={extractReleaseNotes(release?.body, lang)} emptyLabel={labels.emptyNotes} />
            </Modal.Body>
            <Modal.Footer className="justify-end">
              <Button variant="ghost" className="mr-auto" onPress={onGoToUpdate}>
                {labels.goToUpdate}
              </Button>
              <Button variant="secondary" onPress={onClose}>{labels.close}</Button>
            </Modal.Footer>
          </Modal.Dialog>
        </Modal.Container>
      </Modal.Backdrop>
    </Modal.Root>
  )
}
