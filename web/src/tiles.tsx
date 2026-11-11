import type { Mempool, NodeData } from './api'
import { ago, formatBytes, formatFee, formatVMB, isZeroTime, longDuration, shortDuration } from './format'
import { type MeterData, Meters, Slots, uptimeShort } from './meters'
import { useChangeKey, useCountUp } from './motion'

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
  /** A smaller unit after the big value, e.g. " sat/vB". */
  unit?: string
  line: string
  tone: Tone
  /** An extra small line under the tile, e.g. the node software. */
  extra?: { text: string; tone: Tone }
  /** The line is about the latest block: it moves when a new one arrives (#41). */
  block?: { height: number; ago: string }
  /** Meters at the bottom of the tile (#65). */
  meters?: MeterData[]
  uptime?: { short: string; label: string }
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
    return { big: 'In sync', line: `Block ${height}`, tone: 'neutral', block: { height: bc.blocks, ago: '' } }
  }
  const age = now - new Date(bc.lastBlockTime).getTime()
  if (age > LAST_BLOCK_WARN_MS) {
    return { big: shortDuration(age), line: 'since the last block', tone: 'warning' }
  }
  const when = ago(bc.lastBlockTime, now)
  return { big: 'In sync', line: `Block ${height} · ${when}`, tone: 'neutral', block: { height: bc.blocks, ago: when } }
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

const pct = (part: number, whole: number) => Math.min(100, Math.max(0, (part / whole) * 100))

/** The server's meters (#65): CPU, memory, disk, and temperature when known. */
export function serverMeters(s: NodeData['server']): MeterData[] {
  const why = (key: string, fallback: string) => s.missing?.[key] ?? fallback
  const meters: MeterData[] = [
    s.cpuPercent != null
      ? { kind: 'cpu', pct: s.cpuPercent, tone: 'muted', label: `CPU ${Math.round(s.cpuPercent)}%` }
      : { kind: 'cpu', pct: null, tone: 'muted', label: `CPU · ${why('cpu', 'measuring…')}` },
    s.memTotalBytes && s.memUsedBytes != null
      ? {
          kind: 'memory',
          pct: pct(s.memUsedBytes, s.memTotalBytes),
          tone: 'muted',
          label: `Memory ${formatBytes(s.memUsedBytes)} of ${formatBytes(s.memTotalBytes)} · ${Math.round(pct(s.memUsedBytes, s.memTotalBytes))}%`,
        }
      : { kind: 'memory', pct: null, tone: 'muted', label: `Memory · ${why('memory', 'unknown')}` },
  ]
  if (s.diskTotalBytes) {
    const full = pct(s.diskTotalBytes - s.diskFreeBytes, s.diskTotalBytes)
    const tone = below(s.diskFreeBytes, s.diskTotalBytes, DISK_ERROR) ? 'error' : below(s.diskFreeBytes, s.diskTotalBytes, DISK_WARN) ? 'warning' : 'muted'
    meters.push({ kind: 'disk', pct: full, tone, label: `Disk ${formatBytes(s.diskFreeBytes)} free of ${formatBytes(s.diskTotalBytes)} · ${Math.round(full)}% full` })
  } else {
    meters.push({ kind: 'disk', pct: null, tone: 'muted', label: 'Disk · unknown' })
  }
  // Left out, not empty, when the server has no sensor (decision 3).
  if (s.cpuTempC != null) {
    meters.push({ kind: 'temperature', pct: pct(s.cpuTempC, 100), tone: 'muted', label: `CPU temperature ${Math.round(s.cpuTempC)} °C` })
  }
  return meters
}

export function serverTile(s: NodeData['server']): Single {
  // Older servers send only the disk: then no meters.
  const extra =
    s.memTotalBytes !== undefined
      ? {
          meters: serverMeters(s),
          uptime: s.uptimeSec ? { short: uptimeShort(s.uptimeSec), label: `Server up ${longDuration(s.uptimeSec)}` } : undefined,
        }
      : {}
  if (!s.diskTotalBytes) {
    return { big: '—', line: 'Disk usage unknown', tone: 'muted', ...extra }
  }
  const used = 1 - s.diskFreeBytes / s.diskTotalBytes
  const free = formatBytes(s.diskFreeBytes)
  const full = `free · disk ${Math.round(used * 100)}% full`
  if (below(s.diskFreeBytes, s.diskTotalBytes, DISK_ERROR)) return { big: free, line: full, tone: 'error', ...extra }
  if (below(s.diskFreeBytes, s.diskTotalBytes, DISK_WARN)) return { big: free, line: full, tone: 'warning', ...extra }
  return { big: free, line: 'free disk space', tone: 'neutral', ...extra }
}

/** The mempool's memory of maxmempool (#65), or null without a mempool. */
export function mempoolMeter(m: Mempool | null | undefined): MeterData | null {
  if (!m || m.error || !m.maxBytes) return null
  if (!m.loaded) return { kind: 'mempool', pct: null, tone: 'muted', label: 'Mempool memory · loading…' }
  const p = pct(m.usageBytes, m.maxBytes)
  return { kind: 'mempool', pct: p, tone: 'muted', label: `Mempool memory ${formatBytes(m.usageBytes)} of ${formatBytes(m.maxBytes)} · ${Math.round(p)}%` }
}

/** The fee targets and their labels (story #11). */
export const FEE_LABELS: Record<number, string> = { 2: '~20 min', 6: '~1 hour', 144: '~1 day' }

/**
 * The fee for ~20 minutes, with the size of the mempool. Never a status
 * colour: a busy network says nothing about the node.
 */
export function mempoolTile(m: Mempool | null | undefined): Single {
  if (!m || m.error) return { big: '—', line: 'Mempool unavailable', tone: 'muted' }
  const size = `${m.transactions.toLocaleString('en-US')} transactions · ${formatVMB(m.vbytes)}`
  const extra = { text: m.loaded ? size : 'Loading mempool…', tone: 'muted' as Tone }
  const fee = m.fees?.find((f) => f.blocks === 2)
  if (!m.loaded || fee?.rate == null) {
    return { big: '—', line: fee?.missing ?? 'Not enough data yet', tone: 'muted', extra }
  }
  return { big: formatFee(fee.rate), unit: ' sat/vB', line: `Fee for ${FEE_LABELS[2]}`, tone: 'neutral', extra }
}

/** The Mempool tile with its meter. */
function mempoolWithMeter(m: Mempool | null | undefined): Single {
  const meter = mempoolMeter(m)
  return { ...mempoolTile(m), meters: meter ? [meter] : undefined }
}

/** A muted chevron: the tile leads to more of the same (#64). */
function Chevron() {
  return (
    <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
      <path d="M9 6l6 6-6 6" />
    </svg>
  )
}

/**
 * The tile's title, with a link to its section of the details page. The
 * link is the chevron, stretched over the whole tile (#64): one tab stop,
 * read as "Server details", and the whole tile is the target.
 */
function TileHead({ title, details }: { title: string; details?: string }) {
  return (
    <div class="tile-head">
      <h2 class="tile-title">{title}</h2>
      {details && (
        <a class="tile-open" href={`#/details/${details}`} aria-label={`${title} details`}>
          <Chevron />
        </a>
      )}
    </div>
  )
}

/** "Block 977,413 · just now": on a new block the height counts up and the time lights up. */
function BlockLine({ height, ago }: { height: number; ago: string }) {
  const shown = useCountUp(height)
  const flash = useChangeKey(height)
  return (
    <span class="tile-line">
      Block <span class="count">{shown.toLocaleString('en-US')}</span>
      {ago && (
        <>
          {' · '}
          <span key={flash} class={flash ? 'flash' : ''}>
            {ago}
          </span>
        </>
      )}
    </span>
  )
}

function SingleTile({ title, tile, details }: { title: string; tile: Single; details?: string }) {
  return (
    <section class={details ? 'tile tile-linked' : 'tile'}>
      <TileHead title={title} details={details} />
      <span class={`tile-big tone-${tile.tone}`}>
        {tile.big}
        {tile.unit && <span class="tile-unit">{tile.unit}</span>}
      </span>
      {tile.block ? <BlockLine {...tile.block} /> : <span class="tile-line">{tile.line}</span>}
      {tile.extra && <span class={`tile-line small tile-extra tone-${tile.extra.tone}`}>{tile.extra.text}</span>}
      {tile.meters && <Meters meters={tile.meters} uptime={tile.uptime} ruled={!tile.extra} />}
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
    <section class="tile tile-linked">
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
      <Slots count={network.outbound} tone={t.out.tone === 'neutral' ? 'muted' : t.out.tone === 'error' ? 'error' : 'warning'} />
    </section>
  )
}

export function Tiles({ data, now }: { data: NodeData; now: number }) {
  return (
    <div class="tiles">
      <SingleTile title="Blockchain" tile={{ ...blockchainTile(data.blockchain, now), extra: softwareLine(data) ?? undefined }} details="blockchain" />
      <NetworkTile network={data.network} />
      <SingleTile title="Server" tile={serverTile(data.server)} details="server" />
      <SingleTile title="Mempool" tile={mempoolWithMeter(data.mempool)} details="mempool" />
    </div>
  )
}
