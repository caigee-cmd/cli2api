import miniMaxCodeMark from '@/assets/minimaxcode-mark.png'

type Props = {
  size?: number
  className?: string
}

// MiniMax Code brand mark.
export function MiniMaxCodeMark({ size = 16, className = '' }: Props) {
  return (
    <img
      src={miniMaxCodeMark}
      alt=""
      width={size}
      height={size}
      className={`block shrink-0 rounded-[22%] ${className}`.trim()}
      draggable={false}
    />
  )
}
