import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { Button, Modal } from '@heroui/react'
import { ArrowCircleUp, Notebook, X } from '@phosphor-icons/react'
import { fetchSystemUpdate, type SystemUpdateInfo } from '@/api/system'
import { ReleaseNotes } from '@/components/ReleaseNotes'
import { extractReleaseNotes } from '@/lib/releaseNotes'
import { useI18n } from '@/hooks/useI18n'

/** Compact version / new-release entry pinned to the bottom-left of the sidebar. */
export function SidebarUpdateItem({ collapsed, onNavigate }: { collapsed: boolean; onNavigate?: () => void }) {
  const { t, lang } = useI18n()
  const navigate = useNavigate()
  const [info, setInfo] = useState<SystemUpdateInfo | null>(null)
  const [open, setOpen] = useState(false)

  useEffect(() => {
    let alive = true
    fetchSystemUpdate().then((result) => { if (alive) setInfo(result) }).catch(() => {})
    return () => { alive = false }
  }, [])

  if (!info) return null
  const hasUpdate = Boolean(info.has_update)
  const version = hasUpdate ? info.next_version || info.release?.tag_name || '' : info.current_version
  const release = hasUpdate
    ? info.release
    : info.recent_releases?.find((item) => item.tag_name === info.current_version) ?? info.release
  const label = hasUpdate ? t('sidebarNewVersion', { version }) : t('sidebarReleaseNotes')
  const Icon = hasUpdate ? ArrowCircleUp : Notebook

  return (
    <>
      <button
        type="button"
        onClick={() => setOpen(true)}
        aria-label={label}
        title={collapsed ? label : undefined}
        className={`flex w-full items-center gap-2.5 rounded-lg py-1.5 text-body transition-colors hover:bg-default ${collapsed ? 'mx-auto w-10 justify-center px-0' : 'px-3'} ${hasUpdate ? 'font-medium text-accent' : 'text-muted hover:text-foreground'}`}
      >
        <span className="relative inline-flex shrink-0">
          <Icon size={16} weight={hasUpdate ? 'fill' : 'regular'} />
          {hasUpdate && collapsed ? <span className="absolute -right-0.5 -top-0.5 size-1.5 rounded-full bg-accent" /> : null}
        </span>
        {!collapsed && <span className="truncate">{label}</span>}
        {!collapsed && !hasUpdate && version ? <span className="mono ml-auto text-caption">{version}</span> : null}
      </button>

      <Modal.Root isOpen={open} onOpenChange={setOpen}>
        <Modal.Backdrop variant="blur">
          <Modal.Container placement="center" size="lg" scroll="inside">
            <Modal.Dialog>
              <Modal.Header className="items-start justify-between gap-4">
                <div>
                  <Modal.Heading className="text-section font-semibold">{t('sidebarReleaseNotes')}</Modal.Heading>
                  <p className="mono mt-1 text-xs text-muted">{release?.tag_name || version}</p>
                </div>
                <Modal.CloseTrigger aria-label={t('close')} className="grid size-8 place-items-center rounded-lg text-muted hover:bg-surface-secondary"><X size={16} /></Modal.CloseTrigger>
              </Modal.Header>
              <Modal.Body>
                <ReleaseNotes markdown={extractReleaseNotes(release?.body, lang)} emptyLabel={t('updateNoNotes')} />
              </Modal.Body>
              <Modal.Footer className="justify-end">
                <Button variant="ghost" onPress={() => setOpen(false)}>{t('close')}</Button>
                <Button onPress={() => { setOpen(false); onNavigate?.(); navigate('/system?tab=updates') }}>
                  <ArrowCircleUp size={14} />{t('sidebarGoToUpdate')}
                </Button>
              </Modal.Footer>
            </Modal.Dialog>
          </Modal.Container>
        </Modal.Backdrop>
      </Modal.Root>
    </>
  )
}
