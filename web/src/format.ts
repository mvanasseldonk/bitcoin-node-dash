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

/** Local time as "21:14", from an ISO string or epoch milliseconds. */
export function clockTime(t: string | number): string {
  return new Date(t).toLocaleTimeString('en-GB', { hour: '2-digit', minute: '2-digit' })
}

/** "today 03:12", "yesterday 03:12" or "8 Nov 03:12": when something ran. */
export function dayTime(t: string, now: number): string {
  const d = new Date(t)
  const day = (x: Date) => new Date(x.getFullYear(), x.getMonth(), x.getDate()).getTime()
  const days = Math.round((day(new Date(now)) - day(d)) / 86_400_000)
  const date =
    days === 0 ? 'today' : days === 1 ? 'yesterday' : d.toLocaleDateString('en-GB', { day: 'numeric', month: 'short' })
  return `${date} ${clockTime(t)}`
}

/** "12 days 4 h", "3 h 12 min", "5 min", "40 s": for uptimes. */
export function longDuration(sec: number): string {
  const s = Math.max(0, Math.floor(sec))
  const d = Math.floor(s / 86400)
  const h = Math.floor((s % 86400) / 3600)
  const m = Math.floor((s % 3600) / 60)
  if (d > 0) return `${d} ${d === 1 ? 'day' : 'days'} ${h} h`
  if (h > 0) return `${h} h ${m} min`
  if (m > 0) return `${m} min`
  return `${s} s`
}

/** Decimal units per second: "820 B/s", "45.2 kB/s", "1.3 MB/s". */
export function formatRate(bytesPerSec: number): string {
  if (bytesPerSec >= 1e6) return `${(bytesPerSec / 1e6).toFixed(1)} MB/s`
  if (bytesPerSec >= 1e3) return `${(bytesPerSec / 1e3).toFixed(1)} kB/s`
  return `${Math.round(bytesPerSec)} B/s`
}
