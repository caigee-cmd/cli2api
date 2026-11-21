import { Suspense, lazy, useLayoutEffect, useRef, useState, type RefObject } from 'react'
import { useNavigate } from 'react-router-dom'
import { ArrowCircleUp, Notebook } from '@phosphor-icons/react'
import { useSystemUpdate } from '@/hooks/useSystemUpdate'
import { duration, ease, gsap } from '@/lib/motion'
import { useI18n } from '@/hooks/useI18n'

const SidebarUpdateDialog = lazy(() => import('@/components/layout/SidebarUpdateDialog').then((module) => ({ default: module.SidebarUpdateDialog })))

/** Compact version / new-release entry pinned to the bottom-left of the sidebar. */
export function SidebarUpdateItem({ collapsed, onNavigate }: { collapsed: boolean; onNavigate?: () => void }) {
  const { t, lang } = useI18n()
  const navigate = useNavigate()
  const update = useSystemUpdate()
  const { info } = update
  const [open, setOpen] = useState(false)
  const nudgeRef = useRef<HTMLSpanElement>(null)

  if (!info) return null
  const hasUpdate = Boolean(info.has_update)
  const version = hasUpdate ? info.next_version || info.release?.tag_name || '' : info.current_version
  const release = hasUpdate
    ? info.release
    : info.recent_releases?.find((item) => item.tag_name === info.current_version) ?? info.release
  const label = hasUpdate ? t('sidebarNewVersion', { version }) : t('versionUpdate')
  const Icon = hasUpdate ? ArrowCircleUp : Notebook

  return (
    <>
      <button
        type="button"
        onClick={() => setOpen(true)}
        aria-label={label}
        title={collapsed ? label : undefined}
        data-sidebar-item
        className={`flex h-10 w-full items-center gap-3 rounded-[10px] text-nav transition-colors ${collapsed ? 'mx-auto w-10 justify-center px-0' : 'px-3'} ${hasUpdate ? 'font-medium text-accent' : 'text-muted hover:text-foreground'}`}
      >
        <span ref={nudgeRef} className="relative inline-flex shrink-0 will-change-transform">
          <Icon size={20} weight={hasUpdate ? 'fill' : 'regular'} />
          {hasUpdate && collapsed ? <span className="absolute -right-0.5 -top-0.5 size-1.5 rounded-full bg-accent" /> : null}
        </span>
        {!collapsed && <span className="truncate">{label}</span>}
        {!collapsed && !hasUpdate && version ? <span className="mono ml-auto text-caption">{version}</span> : null}
      </button>

      {hasUpdate ? <UpdateNudge targetRef={nudgeRef} /> : null}

      {open ? (
        <Suspense fallback={null}>
          <SidebarUpdateDialog
            open={open}
            update={update}
            release={release}
            version={version}
            lang={lang}
            labels={{
              versionUpdate: t('versionUpdate'),
              close: t('close'),
              releaseNotes: t('sidebarReleaseNotes'),
              emptyNotes: t('updateNoNotes'),
              goToUpdate: t('sidebarGoToUpdate'),
            }}
            onClose={() => setOpen(false)}
            onGoToUpdate={() => {
              setOpen(false)
              onNavigate?.()
              navigate('/system?tab=updates')
            }}
          />
        </Suspense>
      ) : null}
    </>
  )
}

/** Light left-right nudge every few seconds when an update is available. */
function UpdateNudge({ targetRef }: { targetRef: RefObject<HTMLSpanElement | null> }) {
  useLayoutEffect(() => {
    const el = targetRef.current
    if (!el) return

    const context = gsap.context(() => {
      const media = gsap.matchMedia()
      media.add('(prefers-reduced-motion: reduce)', () => {
        gsap.set(el, { clearProps: 'transform' })
      })
      media.add('(prefers-reduced-motion: no-preference)', () => {
        // Amplitude ~3px, one short cycle, then idle ~4.5s. Not a continuous jitter.
        const shake = gsap.timeline({ repeat: -1, repeatDelay: 4.5 })
        shake
          .to(el, { x: 3, duration: duration.fast, ease: ease.inOut })
          .to(el, { x: -2.5, duration: duration.fast, ease: ease.inOut })
          .to(el, { x: 1.5, duration: duration.instant, ease: ease.inOut })
          .to(el, { x: 0, duration: duration.fast, ease: ease.out })
        return () => { shake.kill() }
      })
    }, el)

    return () => context.revert()
  }, [targetRef])

  return null
}
