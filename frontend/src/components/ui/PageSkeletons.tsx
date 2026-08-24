import { Skeleton } from '@heroui/react'

function SkeletonBlock({ className }: { className: string }) {
  return <Skeleton className={`rounded-lg ${className}`} />
}

export function OverviewPageSkeleton() {
  return (
    <div className="space-y-7" aria-label="Loading overview">
      <section className="flex items-end justify-between gap-4 border-b border-[var(--app-line)] pb-4"><div className="space-y-3"><SkeletonBlock className="h-3 w-36" /><SkeletonBlock className="h-8 w-48" /><SkeletonBlock className="h-4 w-80 max-w-[70vw]" /></div><div className="hidden space-y-2 sm:block"><SkeletonBlock className="ml-auto h-3 w-28" /><SkeletonBlock className="ml-auto h-4 w-20" /></div></section>
      <section className="grid overflow-hidden rounded-lg border border-[var(--app-line)] bg-[var(--app-surface)] sm:grid-cols-2 xl:grid-cols-4">{Array.from({ length: 4 }, (_, index) => <div key={index} className="min-h-36 space-y-5 border-[var(--app-line)] p-5 first:border-0 sm:border-l sm:first:border-0"><SkeletonBlock className="h-4 w-24" /><SkeletonBlock className="h-8 w-28" /><SkeletonBlock className="h-3 w-36" /></div>)}</section>
      <section className="grid gap-5 xl:grid-cols-[minmax(0,1.15fr)_minmax(380px,.85fr)]"><div className="app-panel-flat overflow-hidden rounded-lg p-5"><SkeletonBlock className="h-5 w-32" /><SkeletonBlock className="mt-2 h-3 w-48" /><div className="mt-6 space-y-4">{Array.from({ length: 5 }, (_, index) => <SkeletonBlock key={index} className="h-10 w-full" />)}</div></div><div className="app-panel-flat overflow-hidden rounded-lg p-5"><SkeletonBlock className="h-5 w-24" /><SkeletonBlock className="mt-2 h-3 w-40" /><div className="mt-6 space-y-4">{Array.from({ length: 4 }, (_, index) => <SkeletonBlock key={index} className="h-12 w-full" />)}</div></div></section>
    </div>
  )
}

export function AccountsPageSkeleton() {
  return (
    <div className="space-y-6" aria-label="Loading accounts">
      <section className="flex items-center justify-between gap-4 border-b border-[var(--app-line)] pb-4"><div className="flex gap-5"><SkeletonBlock className="h-4 w-24" /><SkeletonBlock className="h-4 w-28" /></div><SkeletonBlock className="h-9 w-28" /></section>
      {Array.from({ length: 2 }, (_, index) => <section key={index} className="app-panel-flat overflow-hidden rounded-lg"><div className="flex items-start justify-between gap-4 p-5"><div className="flex-1 space-y-3"><SkeletonBlock className="h-6 w-48" /><SkeletonBlock className="h-3 w-56" /><SkeletonBlock className="h-3 w-72 max-w-full" /></div><div className="flex gap-2"><SkeletonBlock className="h-8 w-20" /><SkeletonBlock className="h-8 w-20" /></div></div><div className="grid gap-4 border-t border-[var(--app-line)] bg-[var(--app-surface-muted)]/55 px-5 py-4 md:grid-cols-2"><SkeletonBlock className="h-9 w-full" /><SkeletonBlock className="h-9 w-full" /></div></section>)}
    </div>
  )
}

export function ProvidersPageSkeleton() {
  return (
    <div className="space-y-6" aria-label="Loading models">
      <section className="grid gap-5 border-b border-[var(--app-line)] pb-6 lg:grid-cols-[minmax(0,1fr)_auto] lg:items-end"><div className="space-y-3"><SkeletonBlock className="h-3 w-24" /><SkeletonBlock className="h-9 w-48" /><SkeletonBlock className="h-4 w-56" /></div><div className="flex gap-2"><SkeletonBlock className="h-9 w-56" /><SkeletonBlock className="h-9 w-24" /></div></section>
      <div className="app-panel-flat overflow-hidden rounded-lg"><div className="grid grid-cols-4 gap-4 border-b border-[var(--app-line)] px-5 py-4"><SkeletonBlock className="h-3 w-20" /><SkeletonBlock className="h-3 w-24" /><SkeletonBlock className="h-3 w-24" /><SkeletonBlock className="h-3 w-20" /></div>{Array.from({ length: 6 }, (_, index) => <div key={index} className="grid grid-cols-4 items-center gap-4 border-b border-[var(--app-line)] px-5 py-4 last:border-0"><SkeletonBlock className="h-5 w-32" /><SkeletonBlock className="h-4 w-28" /><SkeletonBlock className="h-4 w-36" /><SkeletonBlock className="h-8 w-28" /></div>)}</div>
    </div>
  )
}

export function AccessPageSkeleton() {
  return (
    <div className="space-y-6" aria-label="Loading access settings">
      <section className="grid gap-5 border-b border-[var(--app-line)] pb-6 lg:grid-cols-[minmax(0,1fr)_auto] lg:items-end"><div className="space-y-3"><SkeletonBlock className="h-3 w-32" /><SkeletonBlock className="h-9 w-48" /><SkeletonBlock className="h-4 w-80 max-w-[70vw]" /></div><SkeletonBlock className="h-7 w-24" /></section>
      <section className="grid gap-5 xl:grid-cols-[minmax(0,.9fr)_minmax(0,1.1fr)]"><div className="space-y-5"><div className="app-panel-flat overflow-hidden rounded-lg p-5"><SkeletonBlock className="h-5 w-36" /><SkeletonBlock className="mt-2 h-3 w-52" /><div className="mt-6 space-y-4">{Array.from({ length: 3 }, (_, index) => <SkeletonBlock key={index} className="h-12 w-full" />)}</div></div><div className="app-panel-flat overflow-hidden rounded-lg p-5"><SkeletonBlock className="h-5 w-28" /><SkeletonBlock className="mt-6 h-56 w-full" /></div></div><div className="app-panel overflow-hidden rounded-lg p-5"><div className="flex justify-between"><div className="space-y-2"><SkeletonBlock className="h-5 w-28" /><SkeletonBlock className="h-3 w-44" /></div><SkeletonBlock className="h-9 w-20" /></div><div className="mt-6 grid gap-5 lg:grid-cols-2"><div className="space-y-4">{Array.from({ length: 3 }, (_, index) => <SkeletonBlock key={index} className="h-10 w-full" />)}</div><SkeletonBlock className="h-72 w-full" /></div></div></section>
    </div>
  )
}
