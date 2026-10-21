import { useLayoutEffect, useRef, type CSSProperties, type ReactNode } from 'react'
import { ToggleButton, ToggleButtonGroup } from '@heroui/react'
import { duration, ease, gsap, motionDuration } from '@/lib/motion'

export type FilterToggleOption = {
  id: string
  label: string
  icon?: ReactNode
}

// The selected background is drawn by one sliding pill instead of each button,
// so switching options glides between them (after arc's segmented-control).
const transparentSelected = {
  '--toggle-button-bg-selected': 'transparent',
  '--toggle-button-bg-selected-hover': 'transparent',
  '--toggle-button-bg-selected-pressed': 'transparent',
} as CSSProperties

export function FilterToggle({
  value,
  onChange,
  options,
  ariaLabel,
  className,
}: {
  value: string
  onChange: (value: string) => void
  options: FilterToggleOption[]
  ariaLabel: string
  className?: string
}) {
  const wrapRef = useRef<HTMLDivElement>(null)
  const pillRef = useRef<HTMLSpanElement>(null)
  const placed = useRef(false)

  useLayoutEffect(() => {
    const wrap = wrapRef.current
    const pill = pillRef.current
    if (!wrap || !pill) return
    const place = (animate: boolean) => {
      const active = wrap.querySelector<HTMLElement>('[data-selected="true"]')
      if (!active) {
        gsap.set(pill, { autoAlpha: 0 })
        return
      }
      const wrapRect = wrap.getBoundingClientRect()
      const rect = active.getBoundingClientRect()
      gsap.to(pill, {
        x: rect.left - wrapRect.left,
        y: rect.top - wrapRect.top,
        width: rect.width,
        height: rect.height,
        autoAlpha: 1,
        duration: animate ? motionDuration(duration.base) : 0,
        ease: ease.out,
        overwrite: true,
      })
    }
    place(placed.current)
    placed.current = true
    const observer = new ResizeObserver(() => place(false))
    observer.observe(wrap)
    return () => {
      observer.disconnect()
      gsap.killTweensOf(pill)
    }
  }, [value, options])

  return (
    <div ref={wrapRef} className={`filter-toggle relative inline-flex ${className ?? ''}`}>
      <span
        ref={pillRef}
        aria-hidden
        className="filter-toggle__pill pointer-events-none absolute left-0 top-0 opacity-0"
      />
      <ToggleButtonGroup
        aria-label={ariaLabel}
        size="sm"
        selectionMode="single"
        disallowEmptySelection
        selectedKeys={new Set([value])}
        onSelectionChange={(keys) => {
          const next = [...keys][0]
          if (typeof next === 'string' && next !== value) onChange(next)
        }}
      >
        {options.map((option) => (
          <ToggleButton key={option.id} id={option.id} className="relative z-[1]" style={transparentSelected}>
            {option.icon}
            {option.label}
          </ToggleButton>
        ))}
      </ToggleButtonGroup>
    </div>
  )
}
