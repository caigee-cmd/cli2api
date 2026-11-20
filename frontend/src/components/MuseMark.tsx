import museMark from '@/assets/muse-mark.png'

type Props = {
  size?: number
  className?: string
}

// Muse (Meta) brand mark: official meta.ai favicon.
export function MuseMark({ size = 16, className = '' }: Props) {
  return (
    <img
      src={museMark}
      alt=""
      width={size}
      height={size}
      className={`block shrink-0 rounded-[22%] ${className}`.trim()}
      draggable={false}
    />
  )
}
