import type { NodeData } from './api'
import { ago, formatBytes, isZeroTime, shortDuration } from './format'

// Thresholds from stories #2–#4. Colour is only used when something needs attention.
const LAST_BLOCK_WARN_MS = 60 * 60 * 1000
const FEW_OUTBOUND = 3
// Free disk space limits: the absolute amount or the fraction of the disk,
// whichever is smaller. Same rule as internal/health/health.go.
const GB = 1e9
const DISK_WARN = { bytes: 50 * GB, fraction: 0.1 }
const DISK_ERROR = { bytes: 10 * GB, fraction: 0.05 }
const below = (free: number, total: number, limit: { bytes: number; fraction: number }) =>
  free < Math.min(limit.bytes, limit.fraction * total)

type Tone = 'neutral' | 'muted' | 'warning' | 'error'

interface Single {
  big: string
  line: string
  tone: Tone
  /** An extra small line under the tile, e.g. the node software. */
  extra?: { text: string; tone: Tone }
}

/** "Bitcoin Core 32.1 · 32.2 available", or null without software info. */
export function softwareLine(data: NodeData): { text: string; tone: Tone } | null {
  const label = data.software?.label
  if (!label) return null
  const u = data.upgrade
  if (u?.endOfLife) return { text: `${label} · no security fixes`, tone: 'warning' }
  if (u?.available) return { text: `${label} · ${u.latest} available`, tone: 'muted' }
  return { text: label, tone: 'muted' }
}

export function blockchainTile(bc: NodeData['blockchain'], now: number): Single {
  if (bc.initialBlockDownload) {
    return {
      big: `${Math.floor(bc.verificationProgress * 100)}%`,
      line: 'Syncing with the network',
      tone: 'neutral',
    }
  }
  const height = bc.blocks.toLocaleString('en-US')
  if (isZeroTime(bc.lastBlockTime)) {
    return { big: 'In sync', line: `Block ${height}`, tone: 'neutral' }
  }
  const age = now - new Date(bc.lastBlockTime).getTime()
  if (age > LAST_BLOCK_WARN_MS) {
    return { big: shortDuration(age), line: 'since the last block', tone: 'warning' }
  }
  return { big: 'In sync', line: `Block ${height} · ${ago(bc.lastBlockTime, now)}`, tone: 'neutral' }
}

export function networkTile(n: NodeData['network']) {
  const out =
    n.outbound === 0
      ? { tone: 'error' as Tone, line: 'Not connected' }
      : n.outbound <= FEW_OUTBOUND
        ? { tone: 'warning' as Tone, line: 'Few connections' }
        : { tone: 'neutral' as Tone, line: 'Keeps you in sync' }
  const inbound =
    n.inbound === 0
      ? { tone: 'muted' as Tone, line: 'Optional · port closed' }
      : { tone: 'neutral' as Tone, line: 'You help others' }
  return { out: { count: n.outbound, ...out }, in: { count: n.inbound, ...inbound } }
}

export function serverTile(s: NodeData['server']): Single {
  if (!s.diskTotalBytes) {
    return { big: '—', line: 'Disk usage unknown', tone: 'muted' }
  }
  const used = 1 - s.diskFreeBytes / s.diskTotalBytes
  const free = formatBytes(s.diskFreeBytes)
  const full = `free · disk ${Math.round(used * 100)}% full`
  if (below(s.diskFreeBytes, s.diskTotalBytes, DISK_ERROR)) return { big: free, line: full, tone: 'error' }
  if (below(s.diskFreeBytes, s.diskTotalBytes, DISK_WARN)) return { big: free, line: full, tone: 'warning' }
  return { big: free, line: 'free disk space', tone: 'neutral' }
}

/** The tile's title, with a link to its section of the details page. */
function TileHead({ title, details }: { title: string; details?: string }) {
  return (
    <div class="tile-head">
      <h2 class="tile-title">{title}</h2>
      {details && (
        <a class="tile-link" href={`#/details/${details}`} aria-label={`${title} details`}>
          Details
        </a>
      )}
    </div>
  )
}

function SingleTile({ title, tile, details }: { title: string; tile: Single; details?: string }) {
  return (
    <section class="tile">
      <TileHead title={title} details={details} />
      <span class={`tile-big tone-${tile.tone}`}>{tile.big}</span>
      <span class="tile-line">{tile.line}</span>
      {tile.extra && <span class={`tile-line small tile-extra tone-${tile.extra.tone}`}>{tile.extra.text}</span>}
    </section>
  )
}

function ArrowOut() {
  return (
    <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
      <path d="M7 17L17 7" />
      <path d="M8 7h9v9" />
    </svg>
  )
}

function ArrowIn() {
  return (
    <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
      <path d="M17 7L7 17" />
      <path d="M16 17H7V8" />
    </svg>
  )
}

function NetworkTile({ network }: { network: NodeData['network'] }) {
  const t = networkTile(network)
  return (
    <section class="tile">
      <TileHead title="Network" details="network" />
      <div class="halves">
        <div class="half">
          <span class={`half-count tone-${t.out.tone}`}>
            <span class="tile-big">{t.out.count}</span>
            <ArrowOut />
          </span>
          <span class="half-label">Outgoing</span>
          <span class="tile-line small">{t.out.line}</span>
        </div>
        <div class="half-divider" aria-hidden="true" />
        <div class="half">
          <span class={`half-count tone-${t.in.tone}`}>
            <span class="tile-big">{t.in.count}</span>
            <ArrowIn />
          </span>
          <span class="half-label">Incoming</span>
          <span class="tile-line small">{t.in.line}</span>
        </div>
      </div>
    </section>
  )
}

export function Tiles({ data, now }: { data: NodeData; now: number }) {
  return (
    <div class="tiles">
      <SingleTile title="Blockchain" tile={{ ...blockchainTile(data.blockchain, now), extra: softwareLine(data) ?? undefined }} details="blockchain" />
      <NetworkTile network={data.network} />
      <SingleTile title="Server" tile={serverTile(data.server)} details="server" />
    </div>
  )
}
