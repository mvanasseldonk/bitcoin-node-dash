// Small helpers for the dashboard's motion (story #41). The animations
// themselves are plain CSS, and only run without prefers-reduced-motion.

import { useEffect, useRef, useState } from 'preact/hooks'

const reduce = window.matchMedia('(prefers-reduced-motion: reduce)')

/**
 * A number that goes up by one each time value changes, but not on the
 * first value: use it as a key to replay a CSS animation once per change.
 * null and undefined don't count as values (e.g. while loading).
 */
export function useChangeKey(value: unknown): number {
  const prev = useRef<unknown>(undefined)
  const key = useRef(0)
  if (value != null) {
    if (prev.current !== undefined && prev.current !== value) key.current++
    prev.current = value
  }
  return key.current
}

/** How far a count-up goes; a bigger jump (e.g. after a restart) just shows the new number. */
const MAX_STEPS = 50
const COUNT_MS = 600

/** value, counting up to a new value in steps of one, like an odometer. */
export function useCountUp(value: number): number {
  const [shown, setShown] = useState(value)
  const from = useRef(value)

  useEffect(() => {
    const start = from.current
    from.current = value
    const steps = value - start
    if (steps <= 0 || steps > MAX_STEPS || reduce.matches) {
      setShown(value)
      return
    }
    let frame = 0
    const t0 = performance.now()
    const tick = (t: number) => {
      const p = Math.min(1, (t - t0) / COUNT_MS)
      const eased = 1 - (1 - p) ** 3
      setShown(start + Math.round(steps * eased))
      if (p < 1) frame = requestAnimationFrame(tick)
    }
    frame = requestAnimationFrame(tick)
    return () => {
      cancelAnimationFrame(frame)
      setShown(value)
    }
  }, [value])

  return shown
}
