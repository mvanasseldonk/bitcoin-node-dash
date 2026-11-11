import { useEffect, useRef, useState } from 'preact/hooks'

/** One meter on a tile (#65): a 4 px bar with an icon, the number in a label. */
export interface MeterData {
  kind: 'cpu' | 'memory' | 'disk' | 'temperature' | 'mempool'
  /** 0–100, or null when unknown: then label says why. */
  pct: number | null
  tone: 'muted' | 'warning' | 'error'
  /** "CPU 12%": shown on hover or tap, and read by screen readers. */
  label: string
}

const ICONS: Record<MeterData['kind'], preact.JSX.Element> = {
  cpu: (
    <>
      <rect x="6" y="6" width="12" height="12" rx="2" />
      <path d="M9 2v4M15 2v4M9 18v4M15 18v4M2 9h4M2 15h4M18 9h4M18 15h4" />
    </>
  ),
  memory: (
    <>
      <rect x="2" y="6" width="20" height="10" rx="2" />
      <path d="M6 16v3M10 16v3M14 16v3M18 16v3M7 10v2M12 10v2M17 10v2" />
    </>
  ),
  disk: (
    <>
      <ellipse cx="12" cy="5" rx="8" ry="3" />
      <path d="M4 5v14c0 1.7 3.6 3 8 3s8-1.3 8-3V5" />
      <path d="M4 12c0 1.7 3.6 3 8 3s8-1.3 8-3" />
    </>
  ),
  temperature: <path d="M14 14.8V4a2 2 0 1 0-4 0v10.8a4 4 0 1 0 4 0z" />,
  mempool: (
    <>
      <path d="M3 7l9-4 9 4-9 4-9-4z" />
      <path d="M3 12l9 4 9-4" />
      <path d="M3 17l9 4 9-4" />
    </>
  ),
}

function Icon({ children }: { children: preact.ComponentChildren }) {
  return (
    <svg class="meter-icon" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
      {children}
    </svg>
  )
}

/** A meter. It sits above the tile's link: a tap shows its number, a tap elsewhere hides it. */
function Meter({ m }: { m: MeterData }) {
  const [open, setOpen] = useState(false)
  const ref = useRef<HTMLSpanElement>(null)
  useEffect(() => {
    if (!open) return
    const close = (e: PointerEvent) => {
      if (!ref.current?.contains(e.target as Node)) setOpen(false)
    }
    document.addEventListener('pointerdown', close)
    return () => document.removeEventListener('pointerdown', close)
  }, [open])
  const known = m.pct != null
  return (
    <span
      ref={ref}
      class={`meter meter-${m.tone}${known ? '' : ' meter-unknown'}${open ? ' open' : ''}`}
      onClick={() => setOpen(!open)}
      role={known ? 'meter' : 'img'}
      aria-label={m.label}
      aria-valuemin={known ? 0 : undefined}
      aria-valuemax={known ? 100 : undefined}
      aria-valuenow={known ? Math.round(m.pct!) : undefined}
    >
      <Icon>{ICONS[m.kind]}</Icon>
      <span class="meter-track">
        <span class="meter-fill" style={{ transform: `scaleX(${known ? m.pct! / 100 : 0})` }} />
      </span>
      <span class="meter-tip" aria-hidden="true">
        {m.label}
      </span>
    </span>
  )
}

/** The meters at the bottom of a tile, with the uptime after them on the Server tile. */
export function Meters({ meters, uptime, ruled }: { meters: MeterData[]; uptime?: { short: string; label: string }; ruled: boolean }) {
  return (
    <div class={ruled ? 'meters ruled' : 'meters'}>
      {meters.map((m) => (
        <Meter key={m.kind} m={m} />
      ))}
      {uptime && (
        <span class="uptime" role="img" aria-label={uptime.label} title={uptime.label}>
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
            <path d="M12 2v10" />
            <path d="M18.4 6.6a9 9 0 1 1-12.8 0" />
          </svg>
          <span aria-hidden="true">{uptime.short}</span>
        </span>
      )}
    </div>
  )
}

/** The outgoing connections as 10 slots (#65); more than 10 fills all. */
export function Slots({ count, tone }: { count: number; tone: 'muted' | 'warning' | 'error' }) {
  return (
    <div class={`slots slots-${tone}`} role="img" aria-label={`${count} of 10 outgoing connections`}>
      {Array.from({ length: 10 }, (_, i) => (
        <span key={i} class={i < count ? 'slot on' : 'slot'} />
      ))}
    </div>
  )
}

/** The server's uptime: "45 min", "9 h", "12 d"; "just started" under a minute. */
export function uptimeShort(sec: number): string {
  if (sec < 60) return 'just started'
  const m = Math.floor(sec / 60)
  if (m < 60) return `${m} min`
  const h = Math.floor(m / 60)
  if (h < 24) return `${h} h`
  return `${Math.floor(h / 24)} d`
}
