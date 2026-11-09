// Node control (story #10): the power button in the header, the bottom sheet
// with stop / start / restart, and the progress of a running action.

import { useEffect, useRef, useState } from 'preact/hooks'
import { fetchControl, postControl, type Action, type ControlState, type Snapshot } from './api'
import { shortDuration } from './format'

const SLOW_MS = 10_000
const FAST_MS = 2_000

/** After start or restart, keep showing "Syncing" at most this long. */
const SYNC_FOLLOW_MS = 3 * 60_000

const STEPS: Record<Action, string[]> = {
  restart: ['Stopping', 'Stopped', 'Starting', 'Syncing'],
  stop: ['Stopping', 'Stopped'],
  start: ['Starting', 'Syncing'],
}

const VERB: Record<Action, string> = { restart: 'Restart', stop: 'Stop', start: 'Start' }

const INSTALL = 'curl -fsSL https://raw.githubusercontent.com/mvanasseldonk/bitcoin-node-dash/main/install.sh | NODEDASH_CONTROL=1 sh'

/** The action being followed in the UI, and how far it got. */
export interface Progress {
  action: Action
  steps: string[]
  /** Index of the current step; steps.length when finished. */
  current: number
}

interface Tracking {
  action: Action
  startedAt: string
  /** Browser time when the server's action finished (for the sync follow-up). */
  finishedAt?: number
  max: number
}

/** Index of the current step of a pending action, from systemd's state. */
function stepIndex(action: Action, c: ControlState, startedAt: string): number {
  const s = c.activeState
  const pending = c.pending !== null
  const restarted = Date.parse(c.since) >= Date.parse(startedAt) - 1000
  switch (action) {
    case 'stop':
      return !pending ? 2 : s === 'inactive' || s === 'failed' ? 1 : 0
    case 'start':
      return pending ? 0 : 1
    case 'restart':
      if (!pending) return 3
      if (s === 'inactive' || s === 'failed') return 1
      if (s === 'activating' || (s === 'active' && restarted)) return 2
      return 0
  }
}

function synced(snap: Snapshot | null): boolean {
  const b = snap?.data?.blockchain
  return !!snap?.reachable && snap.health.level !== 'starting' && !!b && !b.initialBlockDownload && b.blocks >= b.headers
}

/**
 * Polls the control state (null while unknown or when control is off) and
 * follows a running action until the node is back in sync.
 */
export function useControl(snap: Snapshot | null, open: boolean) {
  const [ctrl, setCtrl] = useState<ControlState | null>(null)
  const [enabled, setEnabled] = useState(false)
  const [tracking, setTracking] = useState<Tracking | null>(null)
  const [postError, setPostError] = useState('')
  const busy = useRef(false)

  const transitional = !!ctrl && (ctrl.pending !== null || ctrl.activeState === 'activating' || ctrl.activeState === 'deactivating')
  const fast = open || !!tracking || transitional

  useEffect(() => {
    let alive = true
    let timer: number | undefined
    const abort = new AbortController()
    const load = async () => {
      try {
        const c = await fetchControl(abort.signal)
        if (!alive) return
        setEnabled(c !== null)
        setCtrl(c)
      } catch {
        // Keep the last known state; the status poll shows a dashboard outage.
      } finally {
        if (alive) timer = window.setTimeout(load, fast ? FAST_MS : SLOW_MS)
      }
    }
    load()
    return () => {
      alive = false
      abort.abort()
      window.clearTimeout(timer)
    }
  }, [fast])

  // Pick up an action started elsewhere (another tab, after a reload).
  useEffect(() => {
    if (ctrl?.pending && (!tracking || tracking.startedAt !== ctrl.pending.startedAt)) {
      setTracking({ action: ctrl.pending.action, startedAt: ctrl.pending.startedAt, max: 0 })
    }
  }, [ctrl?.pending?.startedAt])

  let progress: Progress | null = null
  if (tracking && ctrl) {
    const steps = STEPS[tracking.action]
    const idx = Math.max(tracking.max, stepIndex(tracking.action, ctrl, tracking.startedAt))
    progress = { action: tracking.action, steps, current: idx }
  }

  // Remember the furthest step (steps never go back), and end the follow-up.
  useEffect(() => {
    if (!tracking || !progress) return
    let next = tracking
    if (progress.current > tracking.max) next = { ...next, max: progress.current }
    if (!ctrl?.pending && next.finishedAt === undefined) next = { ...next, finishedAt: Date.now() }
    const followed = next.finishedAt !== undefined
    const done =
      followed &&
      (tracking.action === 'stop' ||
        ctrl?.activeState === 'failed' ||
        synced(snap) ||
        Date.now() - (next.finishedAt ?? 0) > SYNC_FOLLOW_MS)
    if (done) setTracking(null)
    else if (next !== tracking) setTracking(next)
  })

  const run = async (action: Action) => {
    if (busy.current) return
    busy.current = true
    setPostError('')
    try {
      const c = await postControl(action)
      setCtrl(c)
      if (c.pending) setTracking({ action, startedAt: c.pending.startedAt, max: 0 })
    } catch (e) {
      setPostError(e instanceof Error ? e.message : String(e))
      // The server may know more now (e.g. permission denied).
      fetchControl(new AbortController().signal).then(setCtrl, () => {})
    } finally {
      busy.current = false
    }
  }

  return { enabled, ctrl, progress, run, error: postError || ctrl?.error || '' }
}

export function PowerButton({ onClick }: { onClick: () => void }) {
  return (
    <button type="button" class="power" aria-label="Node controls" aria-haspopup="dialog" onClick={onClick}>
      <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" aria-hidden="true">
        <path d="M12 3v9" />
        <path d="M6.4 6.4a8 8 0 1 0 11.2 0" />
      </svg>
    </button>
  )
}

interface SheetProps {
  /** The node software, e.g. "Bitcoin Core 32.1", for the Version row. */
  software: string | undefined
  ctrl: ControlState | null
  progress: Progress | null
  error: string
  now: number
  onRun: (a: Action) => void
  onClose: () => void
}

export function ControlSheet({ software, ctrl, progress, error, now, onRun, onClose }: SheetProps) {
  const [confirm, setConfirm] = useState<Action | null>(null)
  const dialog = useRef<HTMLDivElement>(null)

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && onClose()
    document.addEventListener('keydown', onKey)
    dialog.current?.focus()
    return () => document.removeEventListener('keydown', onKey)
  }, [])

  // A running action replaces a pending confirmation.
  useEffect(() => {
    if (progress) setConfirm(null)
  }, [!!progress])

  const status = sheetStatus(ctrl, progress)
  const active = ctrl?.activeState === 'active'
  const uptime = active && ctrl && ctrl.since ? shortDuration(now - Date.parse(ctrl.since)) : '—'

  return (
    <div class="sheet-layer">
      <button type="button" class="sheet-overlay" aria-label="Close node controls" onClick={onClose} />
      <div class="sheet" role="dialog" aria-modal="true" aria-labelledby="sheet-title" tabIndex={-1} ref={dialog}>
        <span class="sheet-handle" aria-hidden="true" />
        <div class="sheet-head">
          <h2 id="sheet-title">Node</h2>
          <button type="button" class="sheet-close" aria-label="Close" onClick={onClose}>
            <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" aria-hidden="true">
              <path d="M6 6l12 12M18 6L6 18" />
            </svg>
          </button>
        </div>

        <dl class="sheet-rows">
          <div class="sheet-row">
            <dt>Status</dt>
            <dd>
              <span class={`dot ${status.dot}`} aria-hidden="true" />
              {status.label}
            </dd>
          </div>
          <div class="sheet-row">
            <dt>Uptime</dt>
            <dd>{uptime}</dd>
          </div>
          <div class="sheet-row">
            <dt>Version</dt>
            <dd>{software || '—'}</dd>
          </div>
        </dl>

        {progress ? (
          <Steps progress={progress} />
        ) : confirm && ctrl ? (
          <Confirm action={confirm} ctrl={ctrl} onYes={() => onRun(confirm)} onNo={() => setConfirm(null)} />
        ) : ctrl?.permissionDenied ? (
          <div class="sheet-note-box">
            <p>
              The dashboard isn’t allowed to control <code>{ctrl.unit}</code>. To allow it, run the installer on the server
              with node control enabled:
            </p>
            <pre>{INSTALL}</pre>
          </div>
        ) : ctrl?.available ? (
          <Buttons
            actions={ctrl.actions}
            onAction={(a) => (a === 'start' ? onRun(a) : setConfirm(a))}
          />
        ) : null}

        {error && !progress && (
          <p class="sheet-error" role="alert">
            {error}
          </p>
        )}
      </div>
    </div>
  )
}

function sheetStatus(ctrl: ControlState | null, progress: Progress | null): { label: string; dot: string } {
  if (progress && progress.current < progress.steps.length) return { label: progress.steps[progress.current], dot: 'dot-ink' }
  switch (ctrl?.activeState) {
    case 'active':
      return { label: 'Running', dot: 'dot-healthy' }
    case 'inactive':
      return { label: 'Stopped', dot: '' }
    case 'failed':
      return { label: 'Stopped with an error', dot: 'dot-error' }
    case 'activating':
      return { label: 'Starting', dot: 'dot-ink' }
    case 'deactivating':
      return { label: 'Stopping', dot: 'dot-ink' }
    case undefined:
      return { label: '—', dot: '' }
    default:
      return { label: ctrl?.available ? ctrl.activeState : 'Unknown', dot: '' }
  }
}

function Buttons({ actions, onAction }: { actions: Action[]; onAction: (a: Action) => void }) {
  if (actions.length === 0) return <p class="sheet-note">Waiting for systemd…</p>
  return (
    <div class="sheet-actions">
      {actions.map((a, i) => (
        <button type="button" key={a} class={`btn ${i === 0 ? 'btn-primary' : 'btn-secondary'}`} onClick={() => onAction(a)}>
          {VERB[a]}
        </button>
      ))}
      {actions.includes('stop') && <p class="sheet-note">The Bitcoin node shuts down cleanly. This can take a minute.</p>}
    </div>
  )
}

function Confirm({ action, ctrl, onYes, onNo }: { action: Action; ctrl: ControlState; onYes: () => void; onNo: () => void }) {
  const t = ctrl.stopTimeoutSec
  const shortTimeout = t > 0 && t < 300
  return (
    <div class="sheet-confirm">
      <h3>{VERB[action]} the Bitcoin node?</h3>
      <p>Your node will be offline for a few minutes.</p>
      {shortTimeout && (
        <div class="sheet-note-box sheet-warning">
          <p>
            systemd force-stops the node after {shortDuration(t * 1000)}. A clean shutdown can take longer, and a forced
            stop can corrupt the chainstate. Allow 10 minutes first, on the server:
          </p>
          <pre>{`sudo systemctl edit ${ctrl.unit}\n# add:\n[Service]\nTimeoutStopSec=600`}</pre>
        </div>
      )}
      <div class="sheet-actions">
        <button type="button" class="btn btn-primary" onClick={onYes}>
          {shortTimeout ? `${VERB[action]} anyway` : VERB[action]}
        </button>
        <button type="button" class="btn btn-secondary" onClick={onNo}>
          Cancel
        </button>
      </div>
    </div>
  )
}

function Steps({ progress }: { progress: Progress }) {
  return (
    <ol class="steps" aria-label={`${VERB[progress.action]} progress`}>
      {progress.steps.map((label, i) => {
        const state = i < progress.current ? 'done' : i === progress.current ? 'current' : 'todo'
        return (
          <li key={label} class={`step step-${state}`} aria-current={state === 'current' ? 'step' : undefined}>
            <span class="step-mark" aria-hidden="true">
              {state === 'done' && (
                <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="#fff" stroke-width="3" stroke-linecap="round" stroke-linejoin="round">
                  <path d="M5 12.5l4.5 4.5L19 7.5" />
                </svg>
              )}
            </span>
            {label}
          </li>
        )
      })}
    </ol>
  )
}
