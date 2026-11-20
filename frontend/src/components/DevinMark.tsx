import devinMark from '@/assets/devin-mark.png'

type Props = {
  size?: number
  className?: string
}

// Devin brand mark: official devin.ai favicon.
export function DevinMark({ size = 16, className = '' }: Props) {
  return (
    <img
      src={devinMark}
      alt=""
      width={size}
      height={size}
      className={`block shrink-0 rounded-[22%] ${className}`.trim()}
      draggable={false}
    />
  )
}
