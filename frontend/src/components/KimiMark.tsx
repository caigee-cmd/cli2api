import kimiMark from '@/assets/kimi-mark.png'

type Props = {
  size?: number
  className?: string
}

// Kimi brand mark: official kimi.com favicon.
export function KimiMark({ size = 16, className = '' }: Props) {
  return (
    <img
      src={kimiMark}
      alt=""
      width={size}
      height={size}
      className={`block shrink-0 rounded-[22%] ${className}`.trim()}
      draggable={false}
    />
  )
}
