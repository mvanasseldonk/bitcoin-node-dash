// Types mirror internal/model/model.go.

export type Level = 'healthy' | 'warning' | 'error' | 'unreachable' | 'starting'

export interface NodeData {
  blockchain: {
    chain: string
    blocks: number
    headers: number
    verificationProgress: number
    initialBlockDownload: boolean
    lastBlockTime: string
  }
  network: {
    outbound: number
    inbound: number
  }
  server: {
    diskFreeBytes: number
    diskTotalBytes: number
  }
  /** The node software, e.g. label "Bitcoin Core 32.1" (story #24). */
  software?: { name: string; version: string; label: string }
  /** Upgrade advice; null when there is none to give. */
  upgrade?: { latest: string; available: boolean; endOfLife: boolean } | null
}

export interface Snapshot {
  checkedAt: string
  updatedAt: string
  reachable: boolean
  health: { level: Level; reasons: string[] }
  data: NodeData | null
  /** The server's clock when this snapshot was served. */
  serverTime?: string
}

/** A request that hangs longer than this counts as a failure. */
const FETCH_TIMEOUT_MS = 8_000

export async function fetchStatus(signal: AbortSignal): Promise<Snapshot> {
  const res = await fetch('/api/status', {
    signal: AbortSignal.any([signal, AbortSignal.timeout(FETCH_TIMEOUT_MS)]),
    cache: 'no-store',
  })
  if (!res.ok) throw new Error(`status ${res.status}`)
  return res.json()
}

// Node control (story #10). Mirrors internal/control.State.

export type Action = 'start' | 'stop' | 'restart'

export interface ControlState {
  unit: string
  available: boolean
  /** systemd's ActiveState: active, activating, deactivating, inactive, failed… */
  activeState: string
  subState: string
  since: string
  /** How long systemd waits for a clean stop: 0 unknown, -1 infinite. */
  stopTimeoutSec: number
  actions: Action[]
  pending: { action: Action; startedAt: string } | null
  permissionDenied: boolean
  error: string
}

/** Returns null when node control is off (404). */
export async function fetchControl(signal: AbortSignal): Promise<ControlState | null> {
  const res = await fetch('/api/control', {
    signal: AbortSignal.any([signal, AbortSignal.timeout(FETCH_TIMEOUT_MS)]),
    cache: 'no-store',
  })
  if (res.status === 404) return null
  if (!res.ok) throw new Error(`control ${res.status}`)
  return res.json()
}

/** Starts an action; the error message comes from the server when it refuses. */
export async function postControl(action: Action): Promise<ControlState> {
  const res = await fetch('/api/control', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ action }),
    signal: AbortSignal.timeout(15_000),
  })
  const body = await res.json().catch(() => ({}))
  if (!res.ok) throw new Error(body.error || `The dashboard refused the action (${res.status})`)
  return body
}
