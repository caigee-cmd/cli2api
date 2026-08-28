type Props = {
  size?: number
  className?: string
}

export function TraeMark({ size = 16, className = '' }: Props) {
  return (
    <span
      className={`inline-flex shrink-0 items-center justify-center rounded-[22%] bg-[#1a1a1a] text-white ${className}`.trim()}
      style={{ width: size, height: size, fontSize: Math.max(9, Math.round(size * 0.55)), fontWeight: 650, lineHeight: 1 }}
      aria-hidden="true"
    >
      T
    </span>
  )
}
