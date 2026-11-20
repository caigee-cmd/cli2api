import doubaoworkMark from '@/assets/doubaowork-mark.webp'

type Props = {
  size?: number
  className?: string
}

// Doubao Work mark: the official Doubao Work logo raster.
export function DoubaoworkMark({ size = 16, className = '' }: Props) {
  return (
    <img
      src={doubaoworkMark}
      alt=""
      width={size}
      height={size}
      className={`block shrink-0 ${className}`.trim()}
      draggable={false}
    />
  )
}
