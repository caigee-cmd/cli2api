import { SkeletonBlock } from '@/components/ui/PageSkeletons'

/** Route-level fallback shown while a page chunk is fetched. */
export function RouteFallback() {
  return (
    <div className="space-y-4" aria-busy="true">
      <div className="space-y-2">
        <SkeletonBlock className="h-8 w-52 max-w-full" />
        <SkeletonBlock className="h-4 w-80 max-w-full" />
      </div>
      <div className="grid gap-2.5 md:grid-cols-2 xl:grid-cols-3">
        {Array.from({ length: 6 }, (_, index) => (
          <SkeletonBlock key={index} className="h-28" />
        ))}
      </div>
    </div>
  )
}
