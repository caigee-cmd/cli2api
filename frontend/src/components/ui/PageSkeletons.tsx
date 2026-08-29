import { Skeleton } from '@heroui/react'

function SkeletonBlock({ className }: { className: string }) {
  return <Skeleton className={`rounded-xl ${className}`} />
}

export function OverviewPageSkeleton() {
  return (
    <div className="space-y-6" aria-label="Loading overview">
      <section className="flex items-end justify-between gap-4 border-b border-separator pb-4"><div className="space-y-3"><SkeletonBlock className="h-8 w-40" /><SkeletonBlock className="h-4 w-80 max-w-[70vw]" /></div><div className="hidden space-y-2 sm:block"><SkeletonBlock className="ml-auto h-8 w-40" /><SkeletonBlock className="ml-auto h-4 w-20" /></div></section>
      <section className="grid overflow-hidden rounded-3xl border border-border bg-surface sm:grid-cols-2 xl:grid-cols-4">{Array.from({ length: 4 }, (_, index) => <div key={index} className="min-h-32 space-y-5 border-separator p-5 first:border-0 sm:border-l sm:first:border-0"><SkeletonBlock className="h-4 w-24" /><SkeletonBlock className="h-8 w-28" /><SkeletonBlock className="h-3 w-36" /></div>)}</section>
      <section className="grid gap-5"><div className="overflow-hidden rounded-3xl border border-border bg-surface p-5"><SkeletonBlock className="h-5 w-24" /><SkeletonBlock className="mt-2 h-3 w-48" /><SkeletonBlock className="mt-6 h-72 w-full" /><div className="mt-5 grid grid-cols-4 gap-3">{Array.from({ length: 4 }, (_, index) => <SkeletonBlock key={index} className="h-10 w-full" />)}</div></div></section>
      <section className="grid gap-5 xl:grid-cols-2">{Array.from({ length: 2 }, (_, index) => <div key={index} className="overflow-hidden rounded-3xl border border-border bg-surface p-5"><SkeletonBlock className="h-5 w-28" /><SkeletonBlock className="mt-2 h-3 w-40" /><div className="mt-6 space-y-4">{Array.from({ length: 5 }, (_, item) => <SkeletonBlock key={item} className="h-8 w-full" />)}</div></div>)}</section>
      <section className="grid gap-5 xl:grid-cols-2 2xl:grid-cols-3">{Array.from({ length: 3 }, (_, index) => <div key={index} className="overflow-hidden rounded-3xl border border-border bg-surface p-5"><SkeletonBlock className="h-5 w-28" /><div className="mt-6 space-y-4">{Array.from({ length: 4 }, (_, item) => <SkeletonBlock key={item} className="h-8 w-full" />)}</div></div>)}</section>
    </div>
  )
}

export function AccountCardSkeleton() {
  return (
    <article className="overflow-hidden rounded-3xl border border-border bg-surface">
      <div className="space-y-2.5 px-3 pt-3 pb-2.5">
        <div className="flex items-start justify-between gap-3">
          <div className="flex items-center gap-2.5">
            <SkeletonBlock className="size-[22px]" />
            <div className="space-y-1">
              <SkeletonBlock className="h-4 w-28" />
              <SkeletonBlock className="h-3 w-40" />
            </div>
          </div>
          <SkeletonBlock className="h-5 w-14" />
        </div>
        <SkeletonBlock className="h-2 w-full" />
        <SkeletonBlock className="h-1.5 w-full rounded-full" />
        <SkeletonBlock className="h-3 w-36" />
      </div>
      <div className="flex items-center gap-1.5 border-t border-separator px-3 py-2">
        <SkeletonBlock className="h-8 w-24" />
        <SkeletonBlock className="size-8" />
        <SkeletonBlock className="size-8" />
        <SkeletonBlock className="ml-auto h-[18px] w-[74px]" />
      </div>
    </article>
  )
}

export function AccountsListSkeleton({ count = 6 }: { count?: number }) {
  return (
    <section className="grid gap-2.5 lg:grid-cols-2 xl:grid-cols-3" aria-label="Loading accounts">
      {Array.from({ length: count }, (_, index) => <AccountCardSkeleton key={index} />)}
    </section>
  )
}

export function AccountsPageSkeleton() {
  return (
    <div className="space-y-4" aria-label="Loading accounts">
      <section className="flex items-end justify-between gap-4 border-b border-separator pb-3"><div className="flex flex-wrap gap-5"><SkeletonBlock className="h-4 w-20" /><SkeletonBlock className="h-4 w-24" /><SkeletonBlock className="h-4 w-24" /><SkeletonBlock className="h-4 w-20" /></div><SkeletonBlock className="h-8 w-28" /></section>
      <section className="flex items-center justify-between gap-4"><SkeletonBlock className="h-8 w-full max-w-md" /><SkeletonBlock className="h-8 w-72" /></section>
      <AccountsListSkeleton />
    </div>
  )
}

export function ProvidersPageSkeleton() {
  return (
    <div className="space-y-6" aria-label="Loading models">
      <section className="grid gap-5 border-b border-separator pb-6 lg:grid-cols-[minmax(0,1fr)_auto] lg:items-end"><div className="space-y-3"><SkeletonBlock className="h-3 w-24" /><SkeletonBlock className="h-8 w-44" /><SkeletonBlock className="h-4 w-56" /></div><div className="flex gap-2"><SkeletonBlock className="h-8 w-56" /><SkeletonBlock className="h-8 w-24" /></div></section>
      <div className="overflow-hidden rounded-3xl border border-border bg-surface"><div className="grid grid-cols-4 gap-4 border-b border-separator px-5 py-4"><SkeletonBlock className="h-3 w-20" /><SkeletonBlock className="h-3 w-24" /><SkeletonBlock className="h-3 w-24" /><SkeletonBlock className="h-3 w-20" /></div>{Array.from({ length: 6 }, (_, index) => <div key={index} className="grid grid-cols-4 items-center gap-4 border-b border-separator px-5 py-4 last:border-0"><SkeletonBlock className="h-5 w-32" /><SkeletonBlock className="h-4 w-28" /><SkeletonBlock className="h-4 w-36" /><SkeletonBlock className="h-8 w-28" /></div>)}</div>
    </div>
  )
}

export function KeysPageSkeleton() {
  return (
    <div className="space-y-6" aria-label="Loading API keys">
      <section className="flex items-end justify-between gap-4 border-b border-separator pb-4"><div className="space-y-3"><SkeletonBlock className="h-8 w-40" /><SkeletonBlock className="h-4 w-80 max-w-[70vw]" /></div><SkeletonBlock className="h-8 w-28" /></section>
      <section className="grid gap-2.5 lg:grid-cols-2 xl:grid-cols-3">{Array.from({ length: 3 }, (_, index) => <div key={index} className="overflow-hidden rounded-3xl border border-border bg-surface p-3"><SkeletonBlock className="h-4 w-28" /><SkeletonBlock className="mt-2 h-3 w-40" /><SkeletonBlock className="mt-4 h-6 w-24" /><SkeletonBlock className="mt-6 h-8 w-full" /></div>)}</section>
    </div>
  )
}

export function AccessPageSkeleton() {
  return (
    <div className="space-y-6" aria-label="Loading access settings">
      <section className="grid gap-5 border-b border-separator pb-6 lg:grid-cols-[minmax(0,1fr)_auto] lg:items-end"><div className="space-y-3"><SkeletonBlock className="h-3 w-32" /><SkeletonBlock className="h-8 w-44" /><SkeletonBlock className="h-4 w-80 max-w-[70vw]" /></div><SkeletonBlock className="h-7 w-24" /></section>
      <section className="grid overflow-hidden rounded-3xl border border-border bg-surface sm:grid-cols-3"><div className="space-y-3 border-b border-separator p-5 sm:col-span-2 sm:border-r sm:border-b-0"><SkeletonBlock className="h-3 w-20" /><SkeletonBlock className="h-4 w-72 max-w-full" /></div><div className="grid grid-cols-2 gap-4 p-5 sm:grid-cols-1"><SkeletonBlock className="h-4 w-24" /><SkeletonBlock className="h-4 w-28" /></div></section>
      <section className="grid min-h-[620px] overflow-hidden rounded-3xl border border-border bg-surface xl:grid-cols-[minmax(440px,.92fr)_minmax(0,1.08fr)]"><div className="space-y-6 border-b border-separator p-7 xl:border-r xl:border-b-0"><div className="flex items-center gap-3"><SkeletonBlock className="size-9" /><div className="space-y-2"><SkeletonBlock className="h-4 w-32" /><SkeletonBlock className="h-3 w-28" /></div></div><div className="grid gap-5 sm:grid-cols-2"><div className="space-y-2"><SkeletonBlock className="h-4 w-16" /><SkeletonBlock className="h-10 w-full" /></div><div className="space-y-2"><SkeletonBlock className="h-4 w-16" /><SkeletonBlock className="h-10 w-full" /></div></div><SkeletonBlock className="h-56 w-full" /><SkeletonBlock className="h-12 w-full" /></div><div className="space-y-4 bg-surface-secondary p-6"><div className="flex justify-between"><div className="space-y-2"><SkeletonBlock className="h-4 w-32" /><SkeletonBlock className="h-3 w-44" /></div><SkeletonBlock className="h-6 w-20" /></div><SkeletonBlock className="mt-10 h-3 w-32" /><SkeletonBlock className="h-3 w-full" /><SkeletonBlock className="h-3 w-[86%]" /></div></section>
      <section className="overflow-hidden rounded-3xl border border-border bg-surface p-5"><SkeletonBlock className="h-4 w-36" /><SkeletonBlock className="mt-5 h-24 w-full" /></section>
    </div>
  )
}

export function LogsRequestListSkeleton() {
  return (
    <div aria-label="Loading logs">
      <div className="hidden grid-cols-9 gap-4 border-b border-separator px-5 py-3 md:grid">
        {Array.from({ length: 9 }, (_, index) => <SkeletonBlock key={index} className="h-3 w-16" />)}
      </div>
      {Array.from({ length: 8 }, (_, index) => (
        <div key={index} className="grid grid-cols-2 items-center gap-4 border-b border-separator px-5 py-3.5 last:border-0 md:grid-cols-9">
          <SkeletonBlock className="h-4 w-28" />
          <SkeletonBlock className="h-4 w-24" />
          <SkeletonBlock className="hidden h-4 w-20 md:block" />
          <SkeletonBlock className="hidden h-4 w-16 md:block" />
          <SkeletonBlock className="hidden h-4 w-16 md:block" />
          <SkeletonBlock className="hidden h-4 w-14 md:block" />
          <SkeletonBlock className="hidden h-4 w-12 md:block" />
          <SkeletonBlock className="hidden h-4 w-12 md:block" />
          <SkeletonBlock className="hidden h-4 w-16 md:block" />
        </div>
      ))}
    </div>
  )
}

export function LogsRuntimeListSkeleton() {
  return (
    <div className="divide-y divide-separator" aria-label="Loading logs">
      {Array.from({ length: 8 }, (_, index) => (
        <div key={index} className="grid gap-2 px-5 py-3 sm:grid-cols-[150px_72px_minmax(0,1fr)] sm:items-center">
          <SkeletonBlock className="h-3 w-24" />
          <SkeletonBlock className="h-3 w-12" />
          <SkeletonBlock className="h-4 w-full" />
        </div>
      ))}
    </div>
  )
}

export function LogsPageSkeleton() {
  return (
    <div className="space-y-6" aria-label="Loading logs">
      <section className="flex items-end justify-between gap-4 border-b border-separator pb-4">
        <div className="space-y-3">
          <SkeletonBlock className="h-8 w-28" />
          <SkeletonBlock className="h-4 w-72 max-w-[70vw]" />
        </div>
        <SkeletonBlock className="h-8 w-24" />
      </section>
      <SkeletonBlock className="h-9 w-full max-w-md" />
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex gap-3">
          <SkeletonBlock className="h-8 w-72" />
          <SkeletonBlock className="h-8 w-56" />
        </div>
        <SkeletonBlock className="h-4 w-20" />
      </div>
      <div className="overflow-hidden rounded-3xl border border-border bg-surface">
        <LogsRequestListSkeleton />
      </div>
    </div>
  )
}
