// A new version of the dashboard (story #39): a quiet line under the tiles,
// and the Dashboard row on the details page with the command to update.
// The dashboard can't update itself; the operator runs the command.

import type { ComponentChildren } from 'preact'
import { useEffect, useRef, useState } from 'preact/hooks'
import type { Dashboard } from './api'

const INSTALL = 'curl -fsSL https://raw.githubusercontent.com/mvanasseldonk/bitcoin-node-dash/main/install.sh | sh'
const DOCKER = 'docker compose pull && docker compose up -d'

/** "node-dash v0.6.0 is available · What's new ↗ · How to update" */
export function UpdateLine({ dash }: { dash: Dashboard }) {
  return (
    <p class="update-line">
      node-dash {dash.latest} is available {'·'} <WhatsNew dash={dash} label="What’s new" /> {'·'}{' '}
      <a href="#/details/dashboard">How to update</a>
    </p>
  )
}

function WhatsNew({ dash, label }: { dash: Dashboard; label: string }) {
  return (
    <a href={dash.url} target="_blank" rel="noopener noreferrer">
      {label}
      <span aria-hidden="true"> ↗</span>
      <span class="sr-only"> (opens in a new tab)</span>
    </a>
  )
}

const HINTS: Record<Dashboard['check'], string> = {
  on: 'Latest version',
  off: 'Update check is off',
  local: 'No update check for local builds',
}

/** The Dashboard row in the Server section: the version, and what's newer. */
export function DashboardRow({ dash }: { dash: Dashboard }) {
  const hint = dash.available
    ? `${dash.latest} available`
    : dash.check === 'on' && !dash.latest
      ? undefined // not looked up yet, or GitHub couldn't be reached
      : HINTS[dash.check]
  return (
    <div class="detail-row details-section" id="details-dashboard">
      <dt>
        Dashboard
        {hint && <span class={`detail-hint ${dash.available ? 'detail-hint-news' : ''}`}>{hint}</span>}
      </dt>
      <dd>{dash.version}</dd>
    </div>
  )
}

/** The command to update, with a copy button and the release notes. */
export function UpdateBox({ dash }: { dash: Dashboard }) {
  const command = dash.docker ? DOCKER : INSTALL
  return (
    <div class="update-box">
      <p>{dash.docker ? 'To update, run this next to your docker-compose.yml:' : 'To update, run this on the server:'}</p>
      <CopyCommand command={command}>
        <WhatsNew dash={dash} label={`What’s new in ${dash.latest}`} />
      </CopyCommand>
    </div>
  )
}

type CopyState = 'idle' | 'copied' | 'failed'

function CopyCommand({ command, children }: { command: string; children?: ComponentChildren }) {
  const [state, setState] = useState<CopyState>('idle')
  const pre = useRef<HTMLPreElement>(null)
  const button = useRef<HTMLButtonElement>(null)

  useEffect(() => {
    if (state !== 'copied') return
    const t = setTimeout(() => setState('idle'), 2_000)
    return () => clearTimeout(t)
  }, [state])

  const copy = async () => {
    const ok = await copyText(command)
    button.current?.focus()
    if (!ok && pre.current) {
      // Copying isn't allowed here: select the command, so ⌘C / Ctrl+C works.
      const range = document.createRange()
      range.selectNodeContents(pre.current)
      const sel = window.getSelection()
      sel?.removeAllRanges()
      sel?.addRange(range)
    }
    setState(ok ? 'copied' : 'failed')
  }

  return (
    <>
      <pre ref={pre}>{command}</pre>
      <div class="update-actions">
        <button ref={button} type="button" class="copy-button" onClick={copy}>
          {state === 'copied' ? <CheckIcon /> : <CopyIcon />}
          {state === 'copied' ? 'Copied' : 'Copy command'}
        </button>
        {children}
        <span class="sr-only" role="status">
          {state === 'copied' ? 'Command copied' : state === 'failed' ? 'Copying isn’t allowed here; the command is selected' : ''}
        </span>
      </div>
      {state === 'failed' && <p class="detail-hint">Copying isn’t allowed here. The command is selected: copy it with ⌘C or Ctrl+C.</p>}
    </>
  )
}

/**
 * Copies text. The Clipboard API only works on https or localhost; the
 * dashboard usually runs on plain http in the home network, where the
 * older execCommand still works.
 */
async function copyText(text: string): Promise<boolean> {
  try {
    if (window.isSecureContext && navigator.clipboard) {
      await navigator.clipboard.writeText(text)
      return true
    }
  } catch {
    // fall through to the older way
  }
  const area = document.createElement('textarea')
  area.value = text
  area.setAttribute('readonly', '')
  area.style.position = 'fixed'
  area.style.opacity = '0'
  document.body.appendChild(area)
  area.select()
  let ok = false
  try {
    ok = document.execCommand('copy')
  } catch {
    ok = false
  }
  area.remove()
  return ok
}

function CopyIcon() {
  return (
    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
      <rect x="8" y="8" width="12" height="12" rx="2.5" />
      <path d="M16 8V6.5A2.5 2.5 0 0 0 13.5 4h-7A2.5 2.5 0 0 0 4 6.5v7A2.5 2.5 0 0 0 6.5 16H8" />
    </svg>
  )
}

function CheckIcon() {
  return (
    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
      <path d="M5 12.5l4.5 4.5L19 7.5" />
    </svg>
  )
}
