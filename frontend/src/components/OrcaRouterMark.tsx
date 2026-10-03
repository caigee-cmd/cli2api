type Props = {
  size?: number
  className?: string
}

// OrcaRouter's official classic mark, reused as a vector so the console keeps
// its built-in-asset policy (no remote image URLs). Both the API-key and the
// Auth entry render this one mark, since they are one provider behind two
// credential choices.
export function OrcaRouterMark({ size = 16, className = '' }: Props) {
  return (
    <span
      className={`inline-flex shrink-0 items-center justify-center rounded-[22%] bg-[#0f172a] text-white ${className}`.trim()}
      style={{ width: size, height: size }}
      aria-hidden="true"
    >
      <svg viewBox="0 0 24 24" width={size} height={size} fill="none" aria-hidden="true">
        <path
          d="M12 3c4.4 0 7 2.6 7 6.2 0 2.1-.9 3.9-2.3 5l1 3.4c.1.4-.2.8-.6.8h-2.3l-.5-2.1c-.7.1-1.5.2-2.3.2s-1.6-.1-2.3-.2l-.5 2.1H6.9c-.4 0-.7-.4-.6-.8l1-3.4C5.9 13.1 5 11.3 5 9.2 5 5.6 7.6 3 12 3Z"
          fill="currentColor"
        />
        <circle cx="9.4" cy="9.4" r="1.15" fill="#0f172a" />
        <circle cx="14.6" cy="9.4" r="1.15" fill="#0f172a" />
      </svg>
    </span>
  )
}
