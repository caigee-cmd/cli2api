type Props = {
  size?: number
  className?: string
}

// Muse (Meta) mark — simple infinity/loop glyph matching the Magpie tile style
// (currentColor, no decorative brand chrome).
export function MuseMark({ size = 16, className = '' }: Props) {
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
        fillRule="evenodd"
        d="M8 6a4 4 0 0 0 0 8c.9 0 1.7-.3 2.4-.8L14 16.8A4 4 0 1 0 16 8a3.9 3.9 0 0 0-2.4.8L10 5.2A4 4 0 0 0 8 6Zm0 2a2 2 0 1 1 0 4 2 2 0 0 1 0-4Zm8 4a2 2 0 1 1 0 4 2 2 0 0 1 0-4Z"
        clipRule="evenodd"
      />
    </svg>
  )
}
