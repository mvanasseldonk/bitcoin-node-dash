// Types mirror internal/model/model.go.

export type Level = 'healthy' | 'warning' | 'error' | 'unreachable'

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
}

export async function fetchStatus(signal?: AbortSignal): Promise<Snapshot> {
  const res = await fetch('/api/status', { signal, cache: 'no-store' })
  if (!res.ok) throw new Error(`status ${res.status}`)
  return res.json()
}
