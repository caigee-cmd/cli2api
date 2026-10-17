type Props = {
  size?: number
  className?: string
}

// QwenWork mark: an ink tile, a Q glyph, and a Signal Cyan terminal dot —
// a simple geometric placeholder, deliberately not the official trademark.
export function QwenworkMark({ size = 16, className = '' }: Props) {
  const dot = Math.max(2, Math.round(size * 0.14))
  return (
    <span
      className={`inline-flex shrink-0 items-baseline justify-center rounded-[22%] bg-[#18181B] text-white ${className}`.trim()}
      style={{ width: size, height: size, fontSize: Math.max(9, Math.round(size * 0.5)), fontWeight: 650, lineHeight: 1 }}
      aria-hidden="true"
    >
      <span style={{ alignSelf: 'center' }}>Q</span>
      <span
        style={{
          alignSelf: 'center',
          width: dot,
          height: dot,
          marginLeft: Math.max(0, Math.round(size * 0.03)),
          borderRadius: '50%',
          background: '#22D3EE',
        }}
      />
    </span>
  )
}
