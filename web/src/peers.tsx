// The Peers page (story #12): every connected peer, sortable. The list is
// only fetched while this page is open. Addresses only show when the server
// runs with NODEDASH_PEER_ADDRESSES=1.

import { useEffect, useState } from 'preact/hooks'
import { fetchPeers, type Peer, type Peers } from './api'
import { NETWORKS } from './details'
import { clockTime, formatBytes, longDuration } from './format'

const REFRESH_MS = 5_000
/** Below this width, each peer is a card and sorting is a choice list. */
const NARROW = '(max-width: 760px)'

type Key = 'peer' | 'connection' | 'ping' | 'since' | 'sent' | 'recv' | 'transport'
interface Sort {
  key: Key
  /** desc: the largest value first (the longest connected, the slowest ping, the most bytes). */
  desc: boolean
}

/** Where a first click on a column header starts. */
const FIRST_DESC: Record<Key, boolean> = {
  peer: false,
  connection: false,
  ping: true,
  since: true,
  sent: true,
  recv: true,
  transport: false,
}

const DEFAULT_SORT: Sort = { key: 'since', desc: true }

/** The choices at phone width, the same as the column headers. */
const CHOICES: { label: string; sort: Sort }[] = [
  { label: 'Connected · oldest first', sort: { key: 'since', desc: true } },
  { label: 'Connected · newest first', sort: { key: 'since', desc: false } },
  { label: 'Ping · slowest first', sort: { key: 'ping', desc: true } },
  { label: 'Ping · fastest first', sort: { key: 'ping', desc: false } },
  { label: 'Received · most first', sort: { key: 'recv', desc: true } },
  { label: 'Sent · most first', sort: { key: 'sent', desc: true } },
]

const network = (p: Peer) => NETWORKS[p.network] ?? p.network
const direction = (p: Peer) => (p.inbound ? 'Incoming' : 'Outgoing')

/** The value a column sorts on; null sorts last, whatever the direction. */
function value(p: Peer, key: Key, now: number): string | number | null {
  switch (key) {
    case 'peer':
      return `${network(p)} ${p.client}`.toLowerCase()
    case 'connection':
      return `${direction(p)} ${p.type}`
    case 'ping':
      return p.pingMs
    case 'since':
      return now - Date.parse(p.connectedAt)
    case 'sent':
      return p.sentBytes
    case 'recv':
      return p.recvBytes
    case 'transport':
      return p.encrypted ? 0 : 1
  }
}

function sorted(peers: Peer[], s: Sort, now: number): Peer[] {
  return [...peers].sort((a, b) => {
    const x = value(a, s.key, now)
    const y = value(b, s.key, now)
    if (x === null || y === null) return x === y ? a.id - b.id : x === null ? 1 : -1
    const c = x < y ? -1 : x > y ? 1 : 0
    return (s.desc ? -c : c) || a.id - b.id
  })
}

/** "182 ms", "1.4 s", or "—" before the node measured it. */
function ping(ms: number | null): string {
  if (ms == null) return '—'
  return ms < 1000 ? `${Math.round(ms)} ms` : `${(ms / 1000).toFixed(1)} s`
}

/** "6 days", "9 h", "2 h 10 min", "41 min", "12 s". */
function since(iso: string, now: number): string {
  const sec = Math.max(0, (now - Date.parse(iso)) / 1000)
  const days = Math.floor(sec / 86400)
  if (days > 0) return `${days} ${days === 1 ? 'day' : 'days'}`
  return longDuration(sec).replace(/ 0 min$/, '')
}

/** Traffic with a decimal where it helps: "1.2 GB", "14 GB", "310 MB", "2 kB". */
function traffic(bytes: number): string {
  if (bytes >= 1e9 && bytes < 1e10) return `${(bytes / 1e9).toFixed(1)} GB`
  return formatBytes(bytes)
}

/** A long Tor or I2P address, shortened in the middle. */
function shortAddress(a: string): string {
  return a.length > 30 ? `${a.slice(0, 12)}…${a.slice(-16)}` : a
}

function useNarrow(): boolean {
  const [narrow, setNarrow] = useState(() => window.matchMedia(NARROW).matches)
  useEffect(() => {
    const mq = window.matchMedia(NARROW)
    const on = () => setNarrow(mq.matches)
    mq.addEventListener('change', on)
    return () => mq.removeEventListener('change', on)
  }, [])
  return narrow
}

/** Polls /api/peers every 5 s while the page is visible, and keeps the last list. */
function usePeers() {
  const [peers, setPeers] = useState<Peers | null>(null)
  const [last, setLast] = useState<{ peers: Peer[]; at: string } | null>(null)
  const [error, setError] = useState('')

  useEffect(() => {
    let alive = true
    let timer: number | undefined
    const ctrl = new AbortController()
    const load = async () => {
      window.clearTimeout(timer)
      if (document.hidden) return // resumes on visibilitychange
      try {
        const p = await fetchPeers(ctrl.signal)
        if (!alive) return
        setPeers(p)
        if (p.peers) setLast({ peers: p.peers, at: p.fetchedAt })
        setError(p.nodeError)
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

  return { peers, last, error }
}

export function PeersPage({ now }: { now: number }) {
  const { peers, last, error } = usePeers()
  const [sort, setSort] = useState<Sort>(DEFAULT_SORT)
  const narrow = useNarrow()
  useEffect(() => window.scrollTo(0, 0), [])

  const list = peers?.peers ?? last?.peers ?? null
  const stale = !!error && !!last
  const addresses = !!peers?.addresses
  const rows = list ? sorted(list, sort, now) : []

  let summary = 'Loading peers…'
  if (list) {
    const out = list.filter((p) => !p.inbound).length
    const enc = list.filter((p) => p.encrypted).length
    summary =
      list.length === 0
        ? 'No peers'
        : narrow
          ? `${list.length} peers · ${out} out, ${list.length - out} in`
          : `${list.length} ${list.length === 1 ? 'peer' : 'peers'} · ${out} outgoing, ${list.length - out} incoming · ${enc} encrypted`
  } else if (error) {
    summary = `Peers unavailable: ${error}`
  }

  const click = (key: Key) => setSort((s) => (s.key === key ? { key, desc: !s.desc } : { key, desc: FIRST_DESC[key] }))

  return (
    <div class="peers">
      <a class="peers-back" href="#/details/network">
        ← Details
      </a>
      <div class="peers-head">
        <h1 class="details-title">Peers</h1>
        <p class="peers-summary" role="status">
          {summary}
        </p>
      </div>
      {stale && (
        <p class="details-note">
          {error} · last known values {'·'} {clockTime(last.at)}
        </p>
      )}

      {list && list.length === 0 && (
        <section class="tile peers-empty">
          <h2 class="peers-empty-title">Your node has no peers right now</h2>
          <p class="peers-empty-text">
            It can’t get new blocks this way. Check the network connection of the server, and whether bitcoind may make
            outgoing connections.
          </p>
        </section>
      )}

      {rows.length > 0 && narrow && (
        <div class={`peer-cards ${stale ? 'stale' : ''}`}>
          <label class="peers-sort">
            <span class="peers-sort-label">Sort by</span>
            <select
              value={CHOICES.findIndex((c) => c.sort.key === sort.key && c.sort.desc === sort.desc)}
              onChange={(e) => setSort(CHOICES[Number((e.target as HTMLSelectElement).value)].sort)}
            >
              {CHOICES.map((c, i) => (
                <option key={c.label} value={i}>
                  {c.label}
                </option>
              ))}
            </select>
          </label>
          {rows.map((p) => (
            <article key={p.id} class="peer-card">
              <div class="peer-card-top">
                <span class="peer-name">
                  <span class="peer-net">{network(p)} · </span>
                  {p.client}
                </span>
                <span class="peer-num">{ping(p.pingMs)}</span>
              </div>
              {addresses && p.address && (
                <span class="peer-address" title={p.address}>
                  {shortAddress(p.address)}
                </span>
              )}
              <span class="peer-meta">
                {direction(p)}
                {p.type && ` · ${p.type}`} · {since(p.connectedAt, now)}
              </span>
              <span class="peer-meta peer-num">
                ↑ {traffic(p.sentBytes)} · ↓ {traffic(p.recvBytes)} · {p.encrypted ? 'Encrypted' : 'Not encrypted'}
              </span>
            </article>
          ))}
        </div>
      )}

      {rows.length > 0 && !narrow && (
        <section class={`tile peer-table ${stale ? 'stale' : ''}`}>
          <table>
            <thead>
              <tr>
                <Header k="peer" label="Peer" sort={sort} onClick={click} />
                <Header k="connection" label="Connection" sort={sort} onClick={click} />
                <Header k="ping" label="Ping" sort={sort} onClick={click} end />
                <Header k="since" label="Connected" sort={sort} onClick={click} end />
                <th class="end" aria-sort={ariaSort(sort, 'sent') ?? ariaSort(sort, 'recv')}>
                  <SortButton k="sent" label="Sent" sort={sort} onClick={click} />
                  <span class="peer-slash"> / </span>
                  <SortButton k="recv" label="received" sort={sort} onClick={click} />
                </th>
                <Header k="transport" label="Transport" sort={sort} onClick={click} />
              </tr>
            </thead>
            <tbody>
              {rows.map((p) => (
                <tr key={p.id}>
                  <td class="peer-first">
                    <span class="peer-name">
                      <span class="peer-net">{network(p)} · </span>
                      {p.client}
                    </span>
                    {addresses && p.address && (
                      <span class="peer-address" title={p.address}>
                        {shortAddress(p.address)}
                      </span>
                    )}
                  </td>
                  <td>
                    <span class="peer-dir">{direction(p)}</span>
                    {p.type && <span class="peer-type">{p.type}</span>}
                  </td>
                  <td class="end peer-num">{ping(p.pingMs)}</td>
                  <td class="end peer-num">{since(p.connectedAt, now)}</td>
                  <td class="end peer-num">
                    {traffic(p.sentBytes)}
                    <span class="peer-slash"> / </span>
                    {traffic(p.recvBytes)}
                  </td>
                  <td class={p.encrypted ? '' : 'tone-muted'}>
                    <span class="peer-transport">
                      {p.encrypted && <Lock />}
                      {p.encrypted ? 'Encrypted' : 'Not encrypted'}
                    </span>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </section>
      )}
    </div>
  )
}

function ariaSort(sort: Sort, k: Key): 'ascending' | 'descending' | undefined {
  if (sort.key !== k) return undefined
  return sort.desc ? 'descending' : 'ascending'
}

interface HeaderProps {
  k: Key
  label: string
  sort: Sort
  onClick: (k: Key) => void
  end?: boolean
}

function Header({ k, label, sort, onClick, end }: HeaderProps) {
  return (
    <th class={end ? 'end' : ''} aria-sort={ariaSort(sort, k)}>
      <SortButton k={k} label={label} sort={sort} onClick={onClick} />
    </th>
  )
}

function SortButton({ k, label, sort, onClick }: Omit<HeaderProps, 'end'>) {
  const on = sort.key === k
  return (
    <button type="button" class={`sort-button ${on ? 'on' : ''}`} onClick={() => onClick(k)}>
      {label}
      {on && <span aria-hidden="true">{sort.desc ? ' ↓' : ' ↑'}</span>}
    </button>
  )
}

function Lock() {
  return (
    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
      <rect x="5" y="11" width="14" height="10" rx="2" />
      <path d="M8 11V8a4 4 0 0 1 8 0v3" />
    </svg>
  )
}
