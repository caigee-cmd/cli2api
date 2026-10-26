type Props = {
  size?: number
  className?: string
}

// Kimi crescent mark. Kimi's own glyph is a crescent, so the console reuses that
// silhouette instead of inventing a letterform for the provider row.
export function KimiMark({ size = 16, className = '' }: Props) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      className={`block shrink-0 text-foreground ${className}`.trim()}
      aria-hidden="true"
    >
      <path
        fill="currentColor"
        d="M21 12.79A9 9 0 1 1 11.21 3a7 7 0 0 0 9.79 9.79Z"
      />
    </svg>
  )
}
