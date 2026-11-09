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
