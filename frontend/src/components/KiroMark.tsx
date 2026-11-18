import kiroMarkDark from '@/assets/kiro-mark-dark.webp'
import kiroMarkLight from '@/assets/kiro-mark-light.webp'

type Props = {
  size?: number
  className?: string
}

// Kiro ghost. The reference console ships two rasters of the same mark: a light
// ghost for dark surfaces and a dark ghost for light surfaces, rainbow edge
// unchanged. Decorative; the provider name sits next to it.
export function KiroMark({ size = 16, className = '' }: Props) {
  const frame = `block shrink-0 ${className}`.trim()
  return (
    <span className="inline-flex shrink-0" style={{ width: size, height: size }} aria-hidden="true">
      <img src={kiroMarkLight} alt="" width={size} height={size} className={`${frame} dark:hidden`} draggable={false} />
      <img src={kiroMarkDark} alt="" width={size} height={size} className={`${frame} hidden dark:block`} draggable={false} />
    </span>
  )
}
