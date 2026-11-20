import codeArtsMark from '@/assets/codearts-mark.png'

type Props = {
  size?: number
  className?: string
}

// Huawei Cloud CodeArts (码道) brand mark: the Huawei flower emblem.
export function CodeArtsMark({ size = 16, className = '' }: Props) {
  return (
    <img
      src={codeArtsMark}
      alt=""
      width={size}
      height={size}
      className={`block shrink-0 rounded-[22%] ${className}`.trim()}
      draggable={false}
    />
  )
}
