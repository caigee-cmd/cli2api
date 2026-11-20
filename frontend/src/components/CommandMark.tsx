import commandMark from '@/assets/command-mark.png'

type Props = {
  size?: number
  className?: string
}

// Command Code brand mark: official commandcode.ai favicon.
export function CommandMark({ size = 16, className = '' }: Props) {
  return (
    <img
      src={commandMark}
      alt=""
      width={size}
      height={size}
      className={`block shrink-0 rounded-[22%] ${className}`.trim()}
      draggable={false}
    />
  )
}
