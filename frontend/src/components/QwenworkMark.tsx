import qwenworkMark from '@/assets/qwenwork-mark.png'

type Props = {
  size?: number
  className?: string
}

// QwenWork / 千问办公 brand mark: official qwenwork favicon.
export function QwenworkMark({ size = 16, className = '' }: Props) {
  return (
    <img
      src={qwenworkMark}
      alt=""
      width={size}
      height={size}
      className={`block shrink-0 rounded-[22%] ${className}`.trim()}
      draggable={false}
    />
  )
}
