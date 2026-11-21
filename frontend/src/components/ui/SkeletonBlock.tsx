export function SkeletonBlock({ className }: { className: string }) {
  return <div aria-hidden="true" className={`ui-skeleton rounded-lg ${className}`} />
}
