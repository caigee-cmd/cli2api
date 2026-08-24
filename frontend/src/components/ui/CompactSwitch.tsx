import { useLayoutEffect, useRef } from 'react'
import { Switch } from '@heroui/react'
import { gsap } from 'gsap'

type CompactSwitchProps = {
  isSelected: boolean
  isDisabled?: boolean
  ariaLabel: string
  onChange: (selected: boolean) => void
}

export function CompactSwitch({ isSelected, isDisabled, ariaLabel, onChange }: CompactSwitchProps) {
  const scopeRef = useRef<HTMLSpanElement>(null)
  const previousSelectedRef = useRef(isSelected)
  const animateRef = useRef<((selected: boolean) => void) | null>(null)

  useLayoutEffect(() => {
    const scope = scopeRef.current
    if (!scope) return

    let media: ReturnType<typeof gsap.matchMedia> | null = null
    const context = gsap.context(() => {
      const control = scope.querySelector<HTMLElement>('.compact-switch__control')
      const fill = scope.querySelector<HTMLElement>('.compact-switch__fill')
      const thumb = scope.querySelector<HTMLElement>('.compact-switch__thumb')
      if (!control || !fill || !thumb) return

      media = gsap.matchMedia()
      media.add({ all: 'all', reduceMotion: '(prefers-reduced-motion: reduce)' }, (match) => {
        const reduceMotion = Boolean(match.conditions?.reduceMotion)
        animateRef.current = (selected) => {
          gsap.killTweensOf([control, fill, thumb])
          if (reduceMotion) {
            gsap.set(control, { scale: 1 })
            gsap.set(fill, { scaleX: selected ? 1 : 0 })
            gsap.set(thumb, { x: selected ? 16 : 0, scale: 1 })
            return
          }

          gsap.timeline({ defaults: { overwrite: 'auto' } })
            .to(control, { scale: 0.96, duration: 0.07, ease: 'power2.out' }, 0)
            .to(fill, { scaleX: selected ? 1 : 0, duration: 0.2, ease: 'power2.out' }, 0)
            .to(thumb, { x: selected ? 16 : 0, scale: 1.06, duration: 0.2, ease: 'power3.out' }, 0)
            .to(control, { scale: 1, duration: 0.13, ease: 'power2.out' }, 0.07)
            .to(thumb, { scale: 1, duration: 0.12, ease: 'power2.out' }, 0.1)
        }

        return () => {
          animateRef.current = null
          gsap.killTweensOf([control, fill, thumb])
        }
      })
    }, scope)

    return () => {
      animateRef.current = null
      media?.revert()
      context.revert()
    }
  }, [])

  useLayoutEffect(() => {
    if (previousSelectedRef.current === isSelected) return
    previousSelectedRef.current = isSelected
    animateRef.current?.(isSelected)
  }, [isSelected])

  function handleChange(selected: boolean) {
    previousSelectedRef.current = selected
    animateRef.current?.(selected)
    onChange(selected)
  }

  return (
    <span ref={scopeRef} className="compact-switch-shell" data-selected={isSelected || undefined}>
      <Switch.Root
        className="compact-switch"
        isSelected={isSelected}
        isDisabled={isDisabled}
        onChange={handleChange}
      >
        <Switch.Content className="compact-switch__content" aria-label={ariaLabel}>
          <Switch.Control className="compact-switch__control">
            <span className="compact-switch__fill" aria-hidden />
            <Switch.Thumb className="compact-switch__thumb" />
          </Switch.Control>
        </Switch.Content>
      </Switch.Root>
    </span>
  )
}
