type Props = {
  size?: number
  className?: string
}

export function CodexMark({ size = 16, className = '' }: Props) {
  return (
    <span
      className={`inline-flex shrink-0 items-center justify-center rounded-[22%] bg-black text-white ${className}`.trim()}
      style={{ width: size, height: size, fontSize: Math.max(9, Math.round(size * 0.5)), fontWeight: 700, lineHeight: 1 }}
      aria-hidden="true"
    >
      C
    </span>
  )
}
