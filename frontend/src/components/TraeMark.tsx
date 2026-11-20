import traeMark from '@/assets/trae-mark.png'

type Props = {
  size?: number
  className?: string
}

// Trae brand mark: official trae.ai favicon.
export function TraeMark({ size = 16, className = '' }: Props) {
  return (
    <img
      src={traeMark}
      alt=""
      width={size}
      height={size}
      className={`block shrink-0 rounded-[22%] ${className}`.trim()}
      draggable={false}
    />
  )
}
