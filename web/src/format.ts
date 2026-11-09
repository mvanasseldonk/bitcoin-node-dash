// Plain-language formatting helpers.

const ZERO_TIME = '0001-01-01T00:00:00Z'

export function isZeroTime(iso: string | undefined): boolean {
  return !iso || iso === ZERO_TIME
}

/** "3 s", "4 min", "2 h", "3 d" */
export function shortDuration(ms: number): string {
  const s = Math.max(0, Math.round(ms / 1000))
  if (s < 60) return `${s} s`
  const m = Math.floor(s / 60)
  if (m < 60) return `${m} min`
  const h = Math.floor(m / 60)
  if (h < 48) return `${h} h`
  return `${Math.floor(h / 24)} d`
}

export function ago(iso: string, now: number): string {
  return `${shortDuration(now - new Date(iso).getTime())} ago`
}
