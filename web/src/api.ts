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
  /** The mempool and fee estimates (story #11); null when not read. */
  mempool?: Mempool | null
  /** Upgrade advice; null when there is none to give. */
  upgrade?: { latest: string; available: boolean; endOfLife: boolean } | null
}

/** Mirrors model.Mempool. Fee rates are in sat/vB, already rounded. */
export interface Mempool {
  /** Why the mempool is unknown; the other fields are then zero. */
  error?: string
  /** False while Bitcoin Core loads mempool.dat after a start. */
  loaded: boolean
  transactions: number
  /** The sum of the transactions' virtual sizes. */
  vbytes: number
  usageBytes: number
  maxBytes: number
  minFee: number
  minRelayFee: number
  /** For 2, 6 and 144 blocks; rate is null when missing says why. */
  fees: { blocks: number; rate: number | null; missing?: string }[] | null
}

export interface Snapshot {
  checkedAt: string
  updatedAt: string
  reachable: boolean
  health: { level: Level; reasons: string[] }
  data: NodeData | null
  /** The server's clock when this snapshot was served. */
  serverTime?: string
  /** The dashboard's own version and latest release (story #39). */
  dashboard?: Dashboard
}

/** Mirrors model.Dashboard. */
export interface Dashboard {
  /** The running version, e.g. "v0.5.0", or "dev" for a local build. */
  version: string
  /** on; off with NODEDASH_UPDATE_CHECK=0; local for a local build. */
  check: 'on' | 'off' | 'local'
  /** The latest release, e.g. "v0.6.0", once known. */
  latest?: string
  available: boolean
  /** The latest release's page on GitHub, when it is newer. */
  url?: string
  /** In the Docker image, which is updated by pulling a new image. */
  docker: boolean
  /** The last automatic update (story #50); absent when they are off. */
  autoUpdate?: AutoUpdate
}

/** Mirrors model.AutoUpdate. */
export interface AutoUpdate {
  lastRun: string
  result: 'updated' | 'current' | 'skipped' | 'refused' | 'failed'
  from?: string
  to?: string
  message?: string
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

// Details page (story #14). Mirrors model.Details.

export interface NetworkType {
  /** ipv4, ipv6, onion, i2p, cjdns, or another network with peers */
  name: string
  enabled: boolean
  peers: number
}

export interface ServerDetails {
  /** Since the previous reading; null on the first. */
  cpuPercent: number | null
  memUsedBytes: number
  memTotalBytes: number
  diskFreeBytes: number
  diskUsedBytes: number
  diskTotalBytes: number
  cpuTempC: number | null
  uptimeSec: number
  /** Figure ("cpu", "memory", "disk", "temperature", "uptime") → why it is unknown. */
  missing: Record<string, string>
}

export interface Details {
  fetchedAt: string
  /** Null while the node can't be reached; nodeError says why. */
  nodeError: string
  server: ServerDetails
  blockchain: { uptimeSec: number; sizeOnDiskBytes: number; pruned: boolean } | null
  network: {
    networks: NetworkType[]
    totalSentBytes: number
    totalRecvBytes: number
    /** Bytes per second; null until the second reading. */
    sendRate: number | null
    recvRate: number | null
  } | null
}

// Peers page (story #12). Mirrors model.Peers.

export interface Peer {
  id: number
  /** ipv4, ipv6, onion, i2p, cjdns or not_publicly_routable */
  network: string
  inbound: boolean
  /** "Full relay", "Blocks only", "Manual", "Feeler", "Address fetch"; "" for inbound. */
  type: string
  /** "Bitcoin Core 27.0", or the raw user agent. */
  client: string
  /** Null until the node measured it. */
  pingMs: number | null
  connectedAt: string
  sentBytes: number
  recvBytes: number
  /** The v2 transport (BIP324). */
  encrypted: boolean
  /** Only with NODEDASH_PEER_ADDRESSES=1. */
  address?: string
}

export interface Peers {
  fetchedAt: string
  /** Null while the node can't be reached; nodeError says why. */
  peers: Peer[] | null
  nodeError: string
  /** Whether the server shows addresses (NODEDASH_PEER_ADDRESSES=1). */
  addresses: boolean
}

export async function fetchPeers(signal: AbortSignal): Promise<Peers> {
  const res = await fetch('/api/peers', {
    signal: AbortSignal.any([signal, AbortSignal.timeout(FETCH_TIMEOUT_MS)]),
    cache: 'no-store',
  })
  const body = await res.json().catch(() => ({}))
  if (!res.ok) throw new Error(body.error || `peers ${res.status}`)
  return body
}

/** The node's part may be missing (nodeError); the server's is always there. */
export async function fetchDetails(signal: AbortSignal): Promise<Details> {
  const res = await fetch('/api/details', {
    signal: AbortSignal.any([signal, AbortSignal.timeout(FETCH_TIMEOUT_MS)]),
    cache: 'no-store',
  })
  const body = await res.json().catch(() => ({}))
  if (!res.ok) throw new Error(body.error || `details ${res.status}`)
  return body
}
