import { useEffect, useState } from 'preact/hooks'
import { fetchStatus, type Level, type Snapshot } from './api'
import { ago, clockTime, isZeroTime, shortDuration } from './format'
import { Tiles } from './tiles'

const REFRESH_MS = 10_000

const LABELS: Record<Level, string> = {
  healthy: 'Healthy',
  warning: 'Attention',
  error: 'Problem',
  unreachable: 'Unreachable',
}

/** Data older than this is marked as stale, even when the node is reachable. */
const STALE_MS = 30_000

interface Received {
  snap: Snapshot
  /** Browser clock (ms) when the snapshot arrived. */
  at: number
}

/**
 * Polls the dashboard API and ticks a clock for the "updated" indicator.
 * Returns serverNow: the current time on the server's clock, so ages don't
 * depend on the browser's clock being right.
 */
function useStatus() {
  const [received, setReceived] = useState<Received | null>(null)
  const [serverDown, setServerDown] = useState(false)
  const [now, setNow] = useState(Date.now())

  useEffect(() => {
    let alive = true
    let inFlight = false
    let timer: number | undefined
    const ctrl = new AbortController()

    const load = async () => {
      if (inFlight) return
      inFlight = true
      window.clearTimeout(timer)
      try {
        const snap = await fetchStatus(ctrl.signal)
        if (!alive) return
        setReceived({ snap, at: Date.now() })
        setServerDown(false)
      } catch {
        if (alive) setServerDown(true)
      } finally {
        inFlight = false
        if (alive) timer = window.setTimeout(load, REFRESH_MS)
      }
    }
    load()

    // Background tabs get their timers throttled; refresh right away on return.
    const onVisible = () => {
      if (document.hidden) return
      setNow(Date.now())
      load()
    }
    document.addEventListener('visibilitychange', onVisible)

    const clock = window.setInterval(() => setNow(Date.now()), 1000)
    return () => {
      alive = false
      ctrl.abort()
      window.clearTimeout(timer)
      window.clearInterval(clock)
      document.removeEventListener('visibilitychange', onVisible)
    }
  }, [])

  const snap = received?.snap ?? null
  const offset = received?.snap.serverTime ? Date.parse(received.snap.serverTime) - received.at : 0
  return { snap, serverDown, serverNow: now + offset, offset }
}

export function App() {
  const { snap, serverDown, serverNow: now, offset } = useStatus()

  const updatedAt = snap && !isZeroTime(snap.updatedAt) ? Date.parse(snap.updatedAt) : null
  const age = updatedAt === null ? null : now - updatedAt
  // Reachable, but no new data for a while: e.g. the dashboard's poller is stuck.
  const stale = !serverDown && !!snap?.reachable && age !== null && age > STALE_MS

  const level: Level | 'loading' = serverDown ? 'unreachable' : (snap?.health.level ?? 'loading')
  const label = level === 'loading' ? '' : LABELS[level]
  // Reasons are listed for warnings and errors; unreachable gets a plain subline.
  const reasons = level === 'warning' || level === 'error' ? (snap?.health.reasons ?? []) : []

  let subline = ''
  if (serverDown) {
    subline = 'Can’t reach the dashboard server'
  } else if (snap && !snap.reachable) {
    subline = 'Can’t reach bitcoin node · retrying'
  } else if (stale) {
    subline = 'Waiting for fresh data · retrying'
  } else if (snap?.data && reasons.length === 0) {
    const { network, blockchain } = snap.data
    const peers = `${network.outbound + network.inbound} peers`
    subline = blockchain.initialBlockDownload
      ? `Syncing ${Math.floor(blockchain.verificationProgress * 100)}% · ${peers}`
      : `Fully synced · ${peers} · last block ${ago(blockchain.lastBlockTime, now)}`
  }

  const fresh = !!snap && !serverDown && snap.reachable && !stale
  const updated = age === null ? null : `${shortDuration(age)} ago`

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
        <section class={`hero ${stale ? 'hero-stale' : ''}`} aria-live="polite">
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

        {snap?.data && (
          <div class={fresh ? '' : 'stale'}>
            {!fresh && <p class="stale-caption">Last known values {'·'} {clockTime(Date.parse(snap.updatedAt) - offset)}</p>}
            <Tiles data={snap.data} now={now} />
          </div>
        )}
      </main>
    </div>
  )
}
