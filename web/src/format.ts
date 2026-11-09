// Plain-language formatting helpers.

const ZERO_TIME = '0001-01-01T00:00:00Z'

export function isZeroTime(iso: string | undefined): boolean {
  return !iso || iso === ZERO_TIME
}

/** "3 s", "74 min", "3 h", "3 d" (minutes up to two hours, so "74 min" stays precise) */
export function shortDuration(ms: number): string {
  const s = Math.max(0, Math.round(ms / 1000))
  if (s < 60) return `${s} s`
  const m = Math.floor(s / 60)
  if (m < 120) return `${m} min`
  const h = Math.floor(m / 60)
  if (h < 48) return `${h} h`
  return `${Math.floor(h / 24)} d`
}

export function ago(iso: string, now: number): string {
  return `${shortDuration(now - new Date(iso).getTime())} ago`
}

/** Decimal units, like disk vendors: "812 GB", "1.3 TB". */
export function formatBytes(bytes: number): string {
  if (bytes >= 1e12) return `${(bytes / 1e12).toFixed(1)} TB`
  if (bytes >= 1e9) return `${Math.round(bytes / 1e9)} GB`
  return `${Math.round(bytes / 1e6)} MB`
}

/** Local time as "21:14". */
export function clockTime(iso: string): string {
  return new Date(iso).toLocaleTimeString('en-GB', { hour: '2-digit', minute: '2-digit' })
}
