import qoderMark from '@/assets/qoder-mark.png'

type Props = {
  size?: number
  className?: string
}

export function QoderMark({ size = 16, className = '' }: Props) {
  return (
    <img
      src={qoderMark}
      alt=""
      width={size}
      height={size}
      className={`block shrink-0 rounded-[22%] ${className}`.trim()}
      draggable={false}
    />
  )
}
