// Shared motion tokens for every GSAP animation in the console.
// Durations are in seconds; eases are GSAP ease strings.
import gsap from 'gsap'

export const duration = {
  instant: 0.12,
  fast: 0.18,
  base: 0.28,
  slow: 0.42,
  calm: 0.6,
} as const

export const ease = {
  out: 'power3.out',
  inOut: 'power2.inOut',
  emphasized: 'expo.out',
  soft: 'back.out(1.2)',
  linear: 'none',
} as const

export const stagger = {
  tight: 0.03,
  base: 0.045,
} as const

export function prefersReducedMotion(): boolean {
  return typeof window !== 'undefined' && window.matchMedia('(prefers-reduced-motion: reduce)').matches
}

/** Returns `value` unless the user asked for reduced motion, in which case 0. */
export function motionDuration(value: number): number {
  return prefersReducedMotion() ? 0 : value
}

export { gsap }
