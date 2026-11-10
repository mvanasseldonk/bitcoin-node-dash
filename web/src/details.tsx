// The details page (story #14): the technical numbers behind the Blockchain
// and Network tiles. Extra data is only fetched while this page is open.

import type { ComponentChildren } from 'preact'
import { useEffect, useState } from 'preact/hooks'
import { fetchDetails, type Details, type Snapshot } from './api'
import { ago, formatBytes, formatRate, isZeroTime, longDuration } from './format'
import { softwareLine } from './tiles'

const REFRESH_MS = 5_000

export type Section = 'blockchain' | 'network'

const CHAINS: Record<string, string> = {
  main: 'Mainnet',
  test: 'Testnet3',
  testnet4: 'Testnet4',
  signet: 'Signet',
  regtest: 'Regtest',
}

const NETWORKS: Record<string, string> = {
  ipv4: 'IPv4',
  ipv6: 'IPv6',
  onion: 'Tor',
  i2p: 'I2P',
  cjdns: 'CJDNS',
  not_publicly_routable: 'Local network',
}

/** Polls /api/details every 5 s while the page is visible. */
function useDetails() {
  const [details, setDetails] = useState<Details | null>(null)
  const [error, setError] = useState('')

  useEffect(() => {
    let alive = true
    let timer: number | undefined
    const ctrl = new AbortController()
    const load = async () => {
      window.clearTimeout(timer)
      if (document.hidden) return // resumes on visibilitychange
      try {
        const d = await fetchDetails(ctrl.signal)
        if (!alive) return
        setDetails(d)
        setError('')
      } catch (e) {
        if (alive) setError(e instanceof Error ? e.message : String(e))
      } finally {
        if (alive && !document.hidden) timer = window.setTimeout(load, REFRESH_MS)
      }
    }
    load()
    const onVisible = () => !document.hidden && load()
    document.addEventListener('visibilitychange', onVisible)
    return () => {
      alive = false
      ctrl.abort()
      window.clearTimeout(timer)
      document.removeEventListener('visibilitychange', onVisible)
    }
  }, [])

  return { details, error }
}

function Row({ label, children, hint }: { label: string; children: ComponentChildren; hint?: string }) {
  return (
    <div class="detail-row">
      <dt>
        {label}
        {hint && <span class="detail-hint">{hint}</span>}
      </dt>
      <dd>{children}</dd>
    </div>
  )
}

const dash = '—'
const yesNo = (b: boolean) => (b ? 'Yes' : 'No')

interface Props {
  snap: Snapshot | null
  now: number
  section: Section | null
}

export function DetailsPage({ snap, now, section }: Props) {
  const { details, error } = useDetails()

  // Open at the section of the tile the link came from.
  useEffect(() => {
    if (section) document.getElementById(`details-${section}`)?.scrollIntoView({ block: 'start' })
    else window.scrollTo(0, 0)
  }, [section])

  const data = snap?.data
  const bc = data?.blockchain
  const net = data?.network
  const d = details
  const software = data ? softwareLine(data) : null
  const inbound = net?.inbound ?? 0
  const peers = net ? net.outbound + net.inbound : null
  // Bitcoin Core's verification progress is an estimate; three decimals are plenty.
  const progress = bc ? `${(Math.floor(bc.verificationProgress * 100_000) / 1000).toFixed(3)}%` : dash

  return (
    <div class="details">
      <h1 class="details-title">Details</h1>
      {error && (
        <p class="details-note" role="status">
          Extra numbers unavailable: {error}
          {d ? ' · showing the last known values' : ''}
        </p>
      )}

      <section class="tile details-section" id="details-blockchain" aria-labelledby="h-blockchain">
        <h2 class="tile-title" id="h-blockchain">
          Blockchain
        </h2>
        <dl class="detail-rows">
          <Row label="Chain">{bc ? (CHAINS[bc.chain] ?? bc.chain) : dash}</Row>
          <Row label="Block height">{bc ? bc.blocks.toLocaleString('en-US') : dash}</Row>
          <Row label="Header height" hint="The latest block the node has heard of">
            {bc ? bc.headers.toLocaleString('en-US') : dash}
          </Row>
          <Row label="Verification progress">{progress}</Row>
          <Row label="Initial block download">{bc ? yesNo(bc.initialBlockDownload) : dash}</Row>
          <Row label="Last block">{bc && !isZeroTime(bc.lastBlockTime) ? ago(bc.lastBlockTime, now) : dash}</Row>
          <Row label="Node software">
            {software ? <span class={`tone-${software.tone === 'muted' ? 'neutral' : software.tone}`}>{software.text}</span> : dash}
          </Row>
          <Row label="bitcoind uptime">{d ? longDuration(d.blockchain.uptimeSec) : dash}</Row>
          <Row label="Blockchain data" hint="Blocks and undo data on disk">
            {d ? formatBytes(d.blockchain.sizeOnDiskBytes) : dash}
          </Row>
          <Row label="Pruned">{d ? yesNo(d.blockchain.pruned) : dash}</Row>
        </dl>
      </section>

      <section class="tile details-section" id="details-network" aria-labelledby="h-network">
        <h2 class="tile-title" id="h-network">
          Network
        </h2>
        <dl class="detail-rows">
          <Row label="Peers">{peers ?? dash}</Row>
          <Row label="Outgoing">{net?.outbound ?? dash}</Row>
          <Row label="Incoming">{net?.inbound ?? dash}</Row>
          <Row
            label="Reachable from outside"
            hint={
              inbound > 0
                ? 'Other nodes connect to yours'
                : 'No incoming peers right now. Bitcoin Core can’t tell for sure; your port may be closed'
            }
          >
            {net ? (inbound > 0 ? 'Yes' : 'Unknown') : dash}
          </Row>
          {(d?.network.networks ?? []).map((n) => (
            <Row key={n.name} label={NETWORKS[n.name] ?? n.name}>
              {n.enabled || n.peers > 0 ? `${n.peers} ${n.peers === 1 ? 'peer' : 'peers'}` : <span class="tone-muted">Off</span>}
            </Row>
          ))}
          <Row label="Sent" hint="Since bitcoind started">
            {d ? formatBytes(d.network.totalSentBytes) : dash}
          </Row>
          <Row label="Received" hint="Since bitcoind started">
            {d ? formatBytes(d.network.totalRecvBytes) : dash}
          </Row>
          <Row label="Upload">{d?.network.sendRate != null ? formatRate(d.network.sendRate) : dash}</Row>
          <Row label="Download">{d?.network.recvRate != null ? formatRate(d.network.recvRate) : dash}</Row>
        </dl>
      </section>
    </div>
  )
}

/** The current route: the dashboard, or the details page at a section. */
export function useRoute(): { page: 'dashboard' } | { page: 'details'; section: Section | null } {
  const read = () => location.hash
  const [hash, setHash] = useState(read())
  useEffect(() => {
    const on = () => setHash(read())
    window.addEventListener('hashchange', on)
    return () => window.removeEventListener('hashchange', on)
  }, [])
  const m = hash.match(/^#\/details(?:\/(blockchain|network))?$/)
  if (!m) return { page: 'dashboard' }
  return { page: 'details', section: (m[1] as Section | undefined) ?? null }
}
