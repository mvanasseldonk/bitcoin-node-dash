import { useEffect, useRef, useState } from 'preact/hooks'
import { fetchStatus, type Level, type Snapshot } from './api'
import { ago, clockTime, isZeroTime, shortDuration } from './format'
import { Tiles } from './tiles'
import { ControlSheet, PowerButton, useControl } from './control'
import { DetailsPage, useRoute } from './details'
import { PeersPage } from './peers'
import { SettingsPage } from './settings'
import { AncoroLine, Mark, useAncoro, useBrand, useName, type Tone } from './brand'
import { useChangeKey } from './motion'
import { ThemeSwitch } from './theme'
import { UpdateLine } from './update'
import { WebSettingsPage } from './websettings'

const REFRESH_MS = 10_000
/** While a node action runs, follow its progress closely. */
const FAST_REFRESH_MS = 2_000

const LABELS: Record<Level, string> = {
  healthy: 'Healthy',
  warning: 'Attention',
  error: 'Problem',
  unreachable: 'Unreachable',
  starting: 'Starting',
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
function useStatus(fast: boolean) {
  const [received, setReceived] = useState<Received | null>(null)
  const [serverDown, setServerDown] = useState(false)
  const [now, setNow] = useState(Date.now())
  const interval = useRef(REFRESH_MS)
  interval.current = fast ? FAST_REFRESH_MS : REFRESH_MS
  const reload = useRef<() => void>(() => {})

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
        if (alive) timer = window.setTimeout(load, interval.current)
      }
    }
    load()
    reload.current = load

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

  // Switching to fast polling: don't wait for the slow timer.
  useEffect(() => {
    if (fast) reload.current()
  }, [fast])

  const snap = received?.snap ?? null
  const offset = received?.snap.serverTime ? Date.parse(received.snap.serverTime) - received.at : 0
  return { snap, serverDown, serverNow: now + offset, offset }
}

export function App() {
  const route = useRoute()
  const [sheetOpen, setSheetOpen] = useState(false)
  const [fast, setFast] = useState(false)
  const { snap, serverDown, serverNow: now, offset } = useStatus(fast)
  const control = useControl(snap, sheetOpen)
  const progress = control.progress && control.progress.current < control.progress.steps.length ? control.progress : null
  useEffect(() => setFast(!!control.progress), [!!control.progress])
  const stopped = !progress && (control.ctrl?.activeState === 'inactive' || control.ctrl?.activeState === 'failed')

  const updatedAt = snap && !isZeroTime(snap.updatedAt) ? Date.parse(snap.updatedAt) : null
  const age = updatedAt === null ? null : now - updatedAt
  // Reachable, but no new data for a while: e.g. the dashboard's poller is stuck.
  const stale = !serverDown && !!snap?.reachable && age !== null && age > STALE_MS

  const level: Level | 'loading' = serverDown ? 'unreachable' : (snap?.health.level ?? 'loading')
  // A running action or a stopped node (node control, #10) overrides the health level.
  const tone: Tone = serverDown ? 'unreachable' : progress ? 'busy' : stopped ? 'stopped' : level
  const label = serverDown ? LABELS.unreachable : progress ? progress.steps[progress.current] : stopped ? 'Stopped' : level === 'loading' ? '' : LABELS[level]
  useBrand(tone, label)
  const name = useName()
  const ancoro = useAncoro()
  // Motion (#41): one pulse per status change, one blink per fresh reading.
  const pulse = useChangeKey(tone === 'loading' ? null : tone)
  const blink = useChangeKey(snap?.updatedAt)
  // The first data fades in; later renders, e.g. back from details, don't.
  const [intro, setIntro] = useState(true)
  useEffect(() => {
    if (!snap) return
    const t = window.setTimeout(() => setIntro(false), 1000)
    return () => window.clearTimeout(t)
  }, [!!snap])
  // Reasons are listed for warnings and errors; unreachable gets a plain subline.
  const reasons = !progress && !stopped && (level === 'warning' || level === 'error') ? (snap?.health.reasons ?? []) : []

  let subline = ''
  if (serverDown) {
    subline = 'Can’t reach the dashboard server'
  } else if (progress) {
    // While starting, Bitcoin Core's own progress message, e.g. "Loading block index…"
    const step = progress.steps[progress.current]
    const b = snap?.reachable ? snap.data?.blockchain : undefined
    subline = 'This can take a minute'
    if (step === 'Starting' && level === 'starting' && snap?.health.reasons.length) {
      subline = snap.health.reasons.join(' · ')
    } else if (step === 'Syncing' && b && b.headers > b.blocks) {
      subline = `Catching up · ${(b.headers - b.blocks).toLocaleString('en-US')} blocks to go`
    }
  } else if (stopped) {
    subline =
      control.ctrl?.activeState === 'failed'
        ? 'Bitcoin node stopped with an error · start it from node controls'
        : 'Bitcoin node is stopped · start it from node controls'
  } else if (level === 'starting') {
    // Bitcoin Core's own progress message, e.g. "Loading block index…"
    subline = ['Bitcoin node is starting', ...(snap?.health.reasons ?? [])].join(' · ')
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

  const fresh = !!snap && !serverDown && snap.reachable && !stale && !stopped && !progress
  const updated = age === null ? null : `${shortDuration(age)} ago`

  return (
    <div class={`page ${intro ? 'intro' : ''}`}>
      <header class="header">
        {route.page !== 'dashboard' ? (
          <a class="back" href="#/">
            <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
              <path d="M15 6l-6 6 6 6" />
            </svg>
            <Mark />
            <span class="wordmark">{name}</span>
          </a>
        ) : (
          <span class="brand">
            <Mark />
            <span class="wordmark">{name}</span>
          </span>
        )}
        <div class="header-end">
          {updated && (
            // Next to the sliders button, a phone only has room for the dot (#61); the text stays for screen readers.
            <span class={`updated ${snap?.webSettings ? 'updated-compact' : ''}`}>
              <span key={blink} class={`dot ${fresh ? 'dot-live' : ''} ${fresh && blink ? 'blink' : ''}`} aria-hidden="true" />
              <span class="updated-text">{fresh ? `Updated ${updated}` : `Last update ${updated}`}</span>
            </span>
          )}
          <ThemeSwitch />
          {snap?.webSettings && <WebSettingsButton current={route.page === 'web-settings'} />}
          {control.enabled && <PowerButton onClick={() => setSheetOpen(true)} />}
        </div>
      </header>

      {route.page === 'details' ? (
        <main class="main" key="details">
          <DetailsPage snap={snap} now={now} section={route.section} />
        </main>
      ) : route.page === 'peers' ? (
        <main class="main" key="peers">
          <PeersPage now={now} />
        </main>
      ) : route.page === 'web-settings' ? (
        <main class="main" key="web-settings">
          <WebSettingsPage />
        </main>
      ) : route.page === 'settings' ? (
        <main class="main" key="settings">
          <SettingsPage restart={control.enabled ? () => control.run('restart') : null} progress={control.progress} />
        </main>
      ) : (
        <main class="main" key="dashboard">
          <section class={`hero ${stale ? 'hero-stale' : ''}`} aria-live="polite">
            <div class="circle-wrap">
              <div class={`circle circle-${tone}`} role="img" aria-label={label ? `Node status: ${label}` : 'Loading node status'}>
                <span class="circle-label">{label}</span>
              </div>
              {pulse > 0 && <span key={pulse} class={`pulse circle-${tone}`} aria-hidden="true" />}
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
          {snap?.dashboard?.available && <UpdateLine dash={snap.dashboard} />}
          {ancoro && <AncoroLine />}
        </main>
      )}

      {sheetOpen && control.enabled && (
        <ControlSheet
          software={snap?.data?.software?.label}
          ctrl={control.ctrl}
          progress={control.progress}
          error={control.error}
          now={now}
          onRun={control.run}
          onClose={() => setSheetOpen(false)}
        />
      )}
    </div>
  )
}

/** Sliders, not a gear, so it doesn't read as the node's settings (#61). */
function WebSettingsButton({ current }: { current: boolean }) {
  return (
    <a class="header-btn" href="#/dashboard" aria-label="Dashboard settings" aria-current={current ? 'page' : undefined}>
      <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
        <path d="M4 7h10" />
        <path d="M18 7h2" />
        <circle cx="16" cy="7" r="2" />
        <path d="M4 17h2" />
        <path d="M10 17h10" />
        <circle cx="8" cy="17" r="2" />
      </svg>
    </a>
  )
}
