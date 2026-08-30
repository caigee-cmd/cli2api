type Props = {
  size?: number
  className?: string
  /** Cyan scan along the C rail. Use for in-flight brand loading, not page skeletons. */
  loading?: boolean
}

const RAIL = 'M 91.413 45.646 A 32.000 32.000 0 1 0 91.413 82.354'
const TIP = 'M 96.889 68.454 A 32.000 32.000 0 0 1 91.413 82.354'

/** App brand mark (same geometry as /favicon.svg). Stroke follows the theme
   ink color; the cyan tip stays fixed as the brand accent. */
export function BrandMark({ size = 24, className = '', loading = false }: Props) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="22 22 84 84"
      fill="none"
      aria-hidden="true"
      className={`brand-mark block shrink-0 text-foreground ${loading ? 'brand-mark--loading' : ''} ${className}`.trim()}
    >
      <path
        className="brand-mark__rail"
        pathLength={100}
        d={RAIL}
        stroke="currentColor"
        strokeWidth={11.4}
        strokeLinecap="round"
        strokeLinejoin="round"
      />
      <path
        className="brand-mark__tip text-accent"
        pathLength={100}
        d={TIP}
        stroke="currentColor"
        strokeWidth={11.4}
        strokeLinecap="round"
        strokeLinejoin="round"
      />
      {loading ? (
        <path
          className="brand-mark__scan text-accent"
          pathLength={100}
          d={RAIL}
          stroke="currentColor"
          strokeWidth={11.4}
          strokeLinecap="round"
          strokeLinejoin="round"
        />
      ) : null}
    </svg>
  )
}
