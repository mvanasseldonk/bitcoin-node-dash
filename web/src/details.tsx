// The details page (story #14): the technical numbers behind the Blockchain
// and Network tiles. Extra data is only fetched while this page is open.

import type { ComponentChildren } from 'preact'
import { useEffect, useState } from 'preact/hooks'
import { fetchDetails, type Details, type Mempool, type Setting, type Settings, type Snapshot } from './api'
import { ago, formatBytes, formatFee, formatRate, formatVMB, isZeroTime, longDuration } from './format'
import { FEE_LABELS, softwareLine } from './tiles'
import { AutoUpdateRow, DashboardRow, UpdateBox } from './update'

const REFRESH_MS = 5_000

export type Section = 'blockchain' | 'network' | 'mempool' | 'settings' | 'server' | 'dashboard'

const CHAINS: Record<string, string> = {
  main: 'Mainnet',
  test: 'Testnet3',
  testnet4: 'Testnet4',
  signet: 'Signet',
  regtest: 'Regtest',
}

export const NETWORKS: Record<string, string> = {
  ipv4: 'IPv4',
  ipv6: 'IPv6',
  onion: 'Tor',
  i2p: 'I2P',
  cjdns: 'CJDNS',
  not_publicly_routable: 'Local network',
}

/** The node's part of the details. */
interface NodeParts {
  blockchain: NonNullable<Details['blockchain']>
  network: NonNullable<Details['network']>
}

/**
 * Polls /api/details every 5 s while the page is visible. While the node
 * can't be reached, its last known numbers are kept (lastNode).
 */
function useDetails() {
  const [details, setDetails] = useState<Details | null>(null)
  const [lastNode, setLastNode] = useState<NodeParts | null>(null)
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
        if (d.blockchain && d.network) setLastNode({ blockchain: d.blockchain, network: d.network })
        setError(d.nodeError)
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

  return { details, lastNode, error }
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
  const { details, lastNode, error } = useDetails()

  // Open at the section of the tile the link came from. Once the numbers
  // arrive, the rows above it grow, so scroll there again.
  const loaded = !!details || !!error
  const hasDash = snap?.dashboard != null
  useEffect(() => {
    // The Dashboard row needs a snapshot; until then, open at the Server section.
    const el = section && (document.getElementById(`details-${section}`) ?? document.getElementById('details-server'))
    if (el) el.scrollIntoView({ block: 'start' })
    else window.scrollTo(0, 0)
  }, [section, loaded, hasDash])

  const data = snap?.data
  const bc = data?.blockchain
  const net = data?.network
  // The node's numbers, or its last known ones while it can't be reached.
  const d: NodeParts | null =
    details?.blockchain && details.network ? { blockchain: details.blockchain, network: details.network } : lastNode
  const srv = details?.server
  const dash = snap?.dashboard
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
          Node numbers unavailable: {error}
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
          <Row label="Peers">
            <span class="detail-with-link">
              {peers ?? dash}
              <a href="#/peers">
                See all<span aria-hidden="true"> →</span>
              </a>
            </span>
          </Row>
          <Row label="Outgoing">{net?.outbound ?? dash}</Row>
          <Row label="Incoming">{net?.inbound ?? dash}</Row>
          <Row
            label="Reachable from outside"
            hint={
              !net
                ? undefined
                : inbound > 0
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

      <MempoolSection m={data?.mempool} />

      <SettingsSection s={details?.settings ?? null} />

      <section class="tile details-section" id="details-server" aria-labelledby="h-server">
        <h2 class="tile-title" id="h-server">
          Server
        </h2>
        <dl class="detail-rows">
          <ServerRows srv={srv} />
          {dash?.autoUpdate && <AutoUpdateRow auto={dash.autoUpdate} now={now} />}
          {dash && <DashboardRow dash={dash} />}
        </dl>
        {dash?.available && <UpdateBox dash={dash} />}
      </section>
    </div>
  )
}

/** The mempool and fee estimates (story #11), from the status snapshot. */
function MempoolSection({ m }: { m: Mempool | null | undefined }) {
  const ok = !!m && !m.error
  const counted = ok && m.loaded
  const sat = (v: number) => `${formatFee(v)} sat/vB`
  return (
    <section class="tile details-section" id="details-mempool" aria-labelledby="h-mempool">
      <h2 class="tile-title" id="h-mempool">
        Mempool
      </h2>
      <dl class="detail-rows">
        <Row
          label="Transactions"
          hint={!m ? undefined : m.error ? m.error : m.loaded ? 'Waiting for a block' : 'Loading mempool…'}
        >
          {counted ? m.transactions.toLocaleString('en-US') : dash}
        </Row>
        <Row label="Size" hint="The sum of their virtual sizes">
          {counted ? formatVMB(m.vbytes) : dash}
        </Row>
        <Row label="Memory" hint="Used of the maximum (maxmempool)">
          {counted && m.maxBytes ? `${formatBytes(m.usageBytes)} of ${formatBytes(m.maxBytes)}` : dash}
        </Row>
        <Row label="Minimum mempool fee" hint="Cheaper transactions aren’t kept">
          {ok ? sat(m.minFee) : dash}
        </Row>
        <Row label="Minimum relay fee" hint="Cheaper transactions aren’t passed on">
          {ok ? sat(m.minRelayFee) : dash}
        </Row>
      </dl>
      <h3 class="detail-subtitle">
        Fee estimates<span class="detail-subtitle-note"> · economical</span>
      </h3>
      <dl class="detail-rows">
        {[2, 6, 144].map((blocks) => {
          const f = ok ? m.fees?.find((x) => x.blocks === blocks) : undefined
          const known = f?.rate != null && m?.loaded
          return (
            <Row
              key={blocks}
              label={FEE_LABELS[blocks]}
              hint={known ? `Within ${blocks} blocks` : ok ? (f?.missing ?? 'Not enough data yet') : undefined}
            >
              {known ? sat(f.rate as number) : dash}
            </Row>
          )
        })}
      </dl>
    </section>
  )
}

/** Names and one-line explanations of the settings (story #13). */
export const SETTINGS: Record<string, { label: string; hint: string }> = {
  dbcache: { label: 'Database cache', hint: 'Memory for the chain state; more speeds up syncing' },
  maxconnections: { label: 'Connections', hint: 'Most peers at once, in and out' },
  maxmempool: { label: 'Mempool size', hint: 'Memory for waiting transactions' },
  prune: { label: 'Pruning', hint: 'Delete old blocks to save disk' },
  txindex: { label: 'Transaction index', hint: 'Look up any transaction; uses about 60 GB' },
  listen: { label: 'Incoming peers', hint: 'Let other nodes connect to yours' },
  blocksonly: { label: 'Blocks only', hint: 'No mempool: saves bandwidth, no fee estimates' },
  maxuploadtarget: { label: 'Upload limit', hint: 'Most data sent to peers per day' },
}

const num = (n: number) => n.toLocaleString('en-US')

/** A setting's value in words: "2,048 MiB", "Off · keeps all blocks", "Unlimited". */
export function settingValue(key: string, v: number | null | undefined): string {
  if (v == null) return key === 'dbcache' ? 'Default' : '—'
  switch (key) {
    case 'dbcache':
      return `${num(v)} MiB`
    case 'maxmempool':
      return `${num(v)} MB`
    case 'prune':
      return v === 0 ? 'Off · keeps all blocks' : v === 1 ? 'Manual' : `${num(v)} MiB`
    case 'txindex':
    case 'listen':
    case 'blocksonly':
      return v ? 'On' : 'Off'
    case 'maxuploadtarget':
      return v === 0 ? 'Unlimited' : `${num(v)} MiB`
  }
  return num(v)
}

/** Where a value comes from: "Default", "bitcoin.conf · [main]", "extra.conf (included)". */
function settingSource(s: Setting): string {
  switch (s.source) {
    case 'default':
      return 'Default'
    case 'conf':
      return s.sourceDetail ? `bitcoin.conf · ${s.sourceDetail}` : 'bitcoin.conf'
    case 'include':
      return `${s.sourceDetail ?? 'included file'} (included)`
    case 'cmdline':
      return 'Command line'
    case 'settingsfile':
      return 'settings.json'
    case 'running':
      return 'Running'
  }
  return 'Unknown'
}

/** The node's main settings, read-only (story #13). */
function SettingsSection({ s }: { s: Settings | null }) {
  return (
    <section class="tile details-section" id="details-settings" aria-labelledby="h-settings">
      <div class="tile-head">
        <h2 class="tile-title" id="h-settings">
          Node settings
        </h2>
        {s?.editable && (
          <a class="tile-link" href="#/settings">
            Change settings<span aria-hidden="true"> →</span>
          </a>
        )}
      </div>
      {s?.restartNeeded && <p class="details-section-note tone-warning">Saved · restart bitcoind to apply</p>}
      {s?.fileError && <p class="details-section-note">{s.fileError}. The values below come from the node itself, or are Bitcoin Core’s defaults.</p>}
      <dl class="detail-rows">
        {(s?.items ?? []).map((it) => {
          const meta = SETTINGS[it.key] ?? { label: it.key, hint: '' }
          let hint = <span class="detail-hint">{meta.hint}</span>
          if (it.confValue != null) {
            hint = (
              <span class="detail-hint tone-warning">
                bitcoin.conf says {settingValue(it.key, it.confValue)} · restart to apply
              </span>
            )
          } else if (it.invalid) {
            hint = <span class="detail-hint tone-warning">“{it.invalid}” isn’t a valid number</span>
          }
          return (
            <div class="detail-row" key={it.key}>
              <dt>
                <span>
                  {meta.label} <code class="detail-key">{it.key}</code>
                </span>
                {hint}
              </dt>
              <dd class="detail-value-source">
                {settingValue(it.key, it.value)}
                <span class="detail-hint" title={it.source === 'cmdline' ? it.sourceDetail : undefined}>
                  {settingSource(it)}
                </span>
              </dd>
            </div>
          )
        })}
        {!s && <Row label="Settings">{dash}</Row>}
      </dl>
      {s?.path && (
        <p class="details-path">
          Read from <code>{s.path}</code>
        </p>
      )}
    </section>
  )
}

/** The server's figures; an unknown one shows "—" with the reason. */
function ServerRows({ srv }: { srv: Details['server'] | undefined }) {
  const why = (key: string) => srv?.missing?.[key]
  const pct = (part: number, total: number) => `${Math.round((part / total) * 100)}%`
  return (
    <>
      <Row label="CPU usage" hint={why('cpu') ?? (srv && srv.cpuPercent == null ? 'Measuring…' : undefined)}>
        {srv?.cpuPercent != null ? `${Math.round(srv.cpuPercent)}%` : dash}
      </Row>
      <Row label="Memory" hint={why('memory') ?? (srv?.memTotalBytes ? `${pct(srv.memUsedBytes, srv.memTotalBytes)} in use` : undefined)}>
        {srv?.memTotalBytes ? `${formatBytes(srv.memUsedBytes)} of ${formatBytes(srv.memTotalBytes)}` : dash}
      </Row>
      <Row label="Disk free" hint={why('disk') ?? 'On the disk with the blockchain data'}>
        {srv?.diskTotalBytes ? formatBytes(srv.diskFreeBytes) : dash}
      </Row>
      <Row label="Disk used" hint={srv?.diskTotalBytes ? `${pct(srv.diskUsedBytes, srv.diskTotalBytes)} full` : undefined}>
        {srv?.diskTotalBytes ? `${formatBytes(srv.diskUsedBytes)} of ${formatBytes(srv.diskTotalBytes)}` : dash}
      </Row>
      <Row label="CPU temperature" hint={why('temperature')}>
        {srv?.cpuTempC != null ? `${Math.round(srv.cpuTempC)} °C` : dash}
      </Row>
      <Row label="Server uptime" hint={why('uptime')}>
        {srv?.uptimeSec ? longDuration(srv.uptimeSec) : dash}
      </Row>
    </>
  )
}

/** The current route: the dashboard, or the details page at a section. */
export function useRoute(): { page: 'dashboard' | 'peers' | 'settings' } | { page: 'details'; section: Section | null } {
  const read = () => location.hash
  const [hash, setHash] = useState(read())
  useEffect(() => {
    const on = () => setHash(read())
    window.addEventListener('hashchange', on)
    return () => window.removeEventListener('hashchange', on)
  }, [])
  if (hash === '#/peers') return { page: 'peers' }
  if (hash === '#/settings') return { page: 'settings' }
  const m = hash.match(/^#\/details(?:\/(blockchain|network|mempool|settings|server|dashboard))?$/)
  if (!m) return { page: 'dashboard' }
  return { page: 'details', section: (m[1] as Section | undefined) ?? null }
}
