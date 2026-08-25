type Props = {
  size?: number
  className?: string
}

/** App brand mark (same geometry as /favicon.svg). Stroke follows the theme
   ink color; the cyan node stays fixed as the brand accent. */
export function BrandMark({ size = 24, className = '' }: Props) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="none"
      aria-hidden="true"
      className={`block shrink-0 text-[var(--app-ink)] ${className}`.trim()}
    >
      <path
        d="M13 2 L4 14 H11 L9 11 H13.5 L11 22 L20 10 H13 L15 13 H10.5 Z"
        stroke="currentColor"
        strokeWidth={1.75}
        strokeLinejoin="round"
        strokeLinecap="round"
      />
      <circle cx="9" cy="18" r="1.25" fill="#22D3EE" />
    </svg>
  )
}
