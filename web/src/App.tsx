import { useEffect, useState } from 'preact/hooks'
import { fetchStatus, type Level, type Snapshot } from './api'
import { ago, isZeroTime } from './format'

const REFRESH_MS = 10_000

const LABELS: Record<Level, string> = {
  healthy: 'Healthy',
  warning: 'Attention',
  error: 'Problem',
  unreachable: 'Unreachable',
}

/** Polls the dashboard API and ticks a clock for the "updated" indicator. */
function useStatus() {
  const [snap, setSnap] = useState<Snapshot | null>(null)
  const [serverDown, setServerDown] = useState(false)
  const [now, setNow] = useState(Date.now())

  useEffect(() => {
    let alive = true
    let timer: number | undefined
    const ctrl = new AbortController()

    const load = async () => {
      try {
        const s = await fetchStatus(ctrl.signal)
        if (!alive) return
        setSnap(s)
        setServerDown(false)
      } catch {
        if (alive) setServerDown(true)
      } finally {
        if (alive) timer = window.setTimeout(load, REFRESH_MS)
      }
    }
    load()

    const clock = window.setInterval(() => setNow(Date.now()), 1000)
    return () => {
      alive = false
      ctrl.abort()
      window.clearTimeout(timer)
      window.clearInterval(clock)
    }
  }, [])

  return { snap, serverDown, now }
}

export function App() {
  const { snap, serverDown, now } = useStatus()

  const level: Level | 'loading' = serverDown ? 'unreachable' : (snap?.health.level ?? 'loading')
  const label = level === 'loading' ? '' : LABELS[level]
  // Reasons are listed for warnings and errors; unreachable gets a plain subline.
  const reasons = level === 'warning' || level === 'error' ? (snap?.health.reasons ?? []) : []

  let subline = ''
  if (serverDown) {
    subline = 'Can’t reach the dashboard server'
  } else if (snap && !snap.reachable) {
    subline = 'Can’t reach bitcoind · retrying'
  } else if (snap?.data && reasons.length === 0) {
    const { network, blockchain } = snap.data
    subline = `Fully synced · ${network.outbound + network.inbound} peers · last block ${ago(blockchain.lastBlockTime, now)}`
  }

  const fresh = snap && !serverDown && snap.reachable
  const updated = snap && !isZeroTime(snap.updatedAt) ? ago(snap.updatedAt, now) : null

  return (
    <div class="page">
      <header class="header">
        <span class="wordmark">Node</span>
        {updated && (
          <span class="updated">
            <span class={`dot ${fresh ? 'dot-live' : ''}`} aria-hidden="true" />
            {fresh ? `Updated ${updated}` : `Last update ${updated}`}
          </span>
        )}
      </header>

      <main class="main">
        <section class="hero" aria-live="polite">
          <div class={`circle circle-${level}`} role="img" aria-label={label ? `Node status: ${label}` : 'Loading node status'}>
            <span class="circle-label">{label}</span>
          </div>
          {reasons.length > 0 ? (
            <ul class="reasons">
              {reasons.map((r) => (
                <li key={r}>
                  <span class={`dot dot-${level}`} aria-hidden="true" />
                  {r}
                </li>
              ))}
            </ul>
          ) : (
            subline && <p class="subline">{subline}</p>
          )}
        </section>
      </main>
    </div>
  )
}
