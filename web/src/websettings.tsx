// The dashboard's own settings (story #61, decision 0004): the name, the
// Ancoro link and the update check can be changed; the access switches can
// only be turned off; the address and host names are only shown.

import { useEffect, useRef, useState } from 'preact/hooks'
import type { ComponentChildren } from 'preact'
import {
  fetchWebSettings,
  saveWebSettings,
  SaveError,
  type SwitchKey,
  type WebChange,
  type WebSettings,
  type WebSource,
  type WebSwitch,
  type WebValue,
} from './api'
import { setBrand } from './brand'
import { Dialog } from './settings'

const MAX_NAME = 40
const DEFAULT_NAME = 'Node'
const INSTALL = 'curl -fsSL https://raw.githubusercontent.com/mvanasseldonk/bitcoin-node-dash/main/install.sh | sh -s --'

type DisplayKey = 'name' | 'ancoro' | 'updateCheck'

const ENV: Record<DisplayKey, string> = {
  name: 'NODEDASH_NAME',
  ancoro: 'NODEDASH_ANCORO',
  updateCheck: 'NODEDASH_UPDATE_CHECK',
}

const TOGGLES: { key: 'ancoro' | 'updateCheck'; label: string; hint: string }[] = [
  { key: 'ancoro', label: 'Ancoro link', hint: 'A link under the tiles to look up a transaction' },
  { key: 'updateCheck', label: 'Update check', hint: 'Look up new Bitcoin Core and node-dash releases daily' },
]

const SWITCHES: Record<SwitchKey, { label: string; hint: string; option: string; env: string; offLine: string }> = {
  control: {
    label: 'Node control',
    hint: 'Stop, start and restart Bitcoin Core',
    option: '--control',
    env: 'NODEDASH_CONTROL',
    offLine: 'Stop, start and restart go away from the dashboard, for everyone. Bitcoin Core keeps running.',
  },
  settings: {
    label: 'Changing settings',
    hint: 'Change bitcoin.conf from the dashboard',
    option: '--settings',
    env: 'NODEDASH_SETTINGS',
    offLine: 'The settings form goes away. bitcoin.conf stays as it is, and so do its backups.',
  },
  'peer-addresses': {
    label: 'Peer addresses',
    hint: 'Show the addresses of your peers',
    option: '--peer-addresses',
    env: 'NODEDASH_PEER_ADDRESSES',
    offLine: 'The Peers page hides the addresses again, for everyone.',
  },
}

interface Inputs {
  name: string
  ancoro: boolean
  updateCheck: boolean
}

const inputsOf = (v: WebSettings): Inputs => ({ name: v.name.value, ancoro: v.ancoro.value, updateCheck: v.updateCheck.value })

const onOff = (b: boolean) => (b ? 'on' : 'off')

/** Like the server's NameProblem: at most 40 characters, no control characters. */
function nameProblem(v: string): string {
  const n = [...v.trim()].length
  if (n > MAX_NAME) return `At most ${MAX_NAME} characters.`
  if (/[\u0000-\u001f\u007f-\u009f]/.test(v)) return 'No line breaks or control characters.'
  return ''
}

/** Where a value comes from, as a phrase: "the default", "the environment (NODEDASH_NAME)". */
function fromPhrase(source: WebSource, key: DisplayKey): string {
  if (source === 'environment') return `the environment (${ENV[key]})`
  if (source === 'web') return 'set here'
  return 'the default'
}

function sourceLabel(source: WebSource): string {
  return source === 'environment' ? 'Environment' : source === 'web' ? 'Set here' : 'Default'
}

const shortDate = (iso: string) => new Date(iso).toLocaleDateString('en-GB', { day: 'numeric', month: 'short' })

type Result = { kind: 'saved'; lines: string[] } | { kind: 'conflict' } | { kind: 'error'; message: string } | null

export function WebSettingsPage() {
  const [view, setView] = useState<WebSettings | null>(null)
  const [off, setOff] = useState(false)
  const [loadError, setLoadError] = useState('')
  const [inputs, setInputs] = useState<Inputs | null>(null)
  const [resets, setResets] = useState<DisplayKey[]>([])
  const [serverErrors, setServerErrors] = useState<Record<string, string>>({})
  const [result, setResult] = useState<Result>(null)
  const [saving, setSaving] = useState(false)
  const [confirm, setConfirm] = useState<WebSwitch | null>(null)
  const top = useRef<HTMLDivElement>(null)

  const show = (v: WebSettings) => {
    setView(v)
    setInputs(inputsOf(v))
    setResets([])
    setServerErrors({})
  }

  const load = async () => {
    try {
      const v = await fetchWebSettings(new AbortController().signal)
      if (!v) {
        setOff(true)
        return
      }
      show(v)
      setResult(null)
      setLoadError('')
    } catch (e) {
      setLoadError(e instanceof Error ? e.message : String(e))
    }
  }
  useEffect(() => {
    window.scrollTo(0, 0)
    load()
  }, [])

  const intro = (
    <>
      <a class="settings-back" href="#/">
        ← Dashboard
      </a>
      <div class="settings-intro">
        <h1 class="details-title">Dashboard settings</h1>
        <p>
          Settings of node-dash itself. Bitcoin Core’s settings are on the <a href="#/details/settings">details page</a>.
        </p>
      </div>
    </>
  )

  if (off) {
    return (
      <div class="details settings">
        {intro}
        <div class="settings-card tone-card-muted" role="status">
          <strong>Dashboard settings are off on this server</strong>
          <p>NODEDASH_WEB_SETTINGS=0 turns this page off. To change the dashboard’s settings, run this on the server:</p>
          <pre>sudo systemctl edit node-dash</pre>
        </div>
      </div>
    )
  }
  if (!view || !inputs) {
    return (
      <div class="details settings">
        {intro}
        {loadError && <p class="details-note">Dashboard settings unavailable: {loadError}</p>}
      </div>
    )
  }

  // What Save sends: a value that differs from what applies now, or "Use that".
  const change: WebChange = {}
  const set: NonNullable<WebChange['set']> = {}
  const name = inputs.name.trim()
  if (!resets.includes('name') && (name || DEFAULT_NAME) !== view.name.value) set.name = name
  for (const { key } of TOGGLES) {
    if (!resets.includes(key) && inputs[key] !== view[key].value) set[key] = inputs[key]
  }
  if (Object.keys(set).length) change.set = set
  if (resets.length) change.reset = resets
  const nChanges = Object.keys(set).length + resets.length

  const errors: Record<string, string> = { ...serverErrors }
  const local = nameProblem(inputs.name)
  if (local) errors.name = local
  const nErrors = Object.keys(errors).length
  const readOnly = !view.canSave
  const canSave = !readOnly && !saving && nChanges > 0 && nErrors === 0

  const edit = <K extends DisplayKey>(key: K, value: Inputs[K]) => {
    setInputs({ ...inputs, [key]: value })
    setResets(resets.filter((r) => r !== key))
    if (key === 'name') setServerErrors({})
  }
  const useFallback = (key: DisplayKey) => {
    setInputs({ ...inputs, [key]: view[key].fallback })
    setResets([...resets.filter((r) => r !== key), key])
    if (key === 'name') setServerErrors({})
  }

  const post = async (c: WebChange, keepInputs: boolean) => {
    setSaving(true)
    setResult(null)
    try {
      const v = await saveWebSettings(view.version, c)
      setBrand({ name: v.name.value, ancoro: v.ancoro.value })
      if (keepInputs) setView(v)
      else show(v)
      setResult({ kind: 'saved', lines: v.saved ?? [] })
    } catch (e) {
      if (e instanceof SaveError && e.conflict) setResult({ kind: 'conflict' })
      else if (e instanceof SaveError && Object.keys(e.fields).length > 0) setServerErrors(e.fields)
      else setResult({ kind: 'error', message: e instanceof Error ? e.message : String(e) })
    } finally {
      setSaving(false)
      top.current?.scrollIntoView({ block: 'start' })
    }
  }

  const onSave = () => canSave && post(change, false)
  // Turning a switch off applies at once; changes not saved yet stay in the form.
  const turnOff = (sw: WebSwitch) => {
    setConfirm(null)
    post({ off: [sw.key] }, true)
  }

  return (
    <div class="details settings" ref={top}>
      {intro}

      {result?.kind === 'saved' && (
        <div class="settings-card tone-card-healthy" role="status">
          <strong>Saved</strong>
          <p>{result.lines.join(' · ')}. No restart needed.</p>
        </div>
      )}
      {result?.kind === 'conflict' && (
        <div class="settings-card tone-card-error" role="alert">
          <strong>Not saved: changed elsewhere</strong>
          <p>The settings were changed since you opened this page, perhaps in another tab. Reload to see them, then make your change again.</p>
          <button type="button" class="btn btn-primary settings-card-btn" onClick={load}>
            Reload
          </button>
        </div>
      )}
      {result?.kind === 'error' && (
        <div class="settings-card tone-card-error" role="alert">
          <strong>Not saved</strong>
          <p>{result.message}</p>
        </div>
      )}
      {readOnly && (
        <div class="settings-card tone-card-error" role="status">
          <strong>Can’t save</strong>
          <p>{view.cantSave} The values from the environment still apply.</p>
        </div>
      )}
      {view.warnings?.map((w) => (
        <div class="settings-card" role="status" key={w}>
          <strong>Settings file ignored</strong>
          <p>{w}</p>
        </div>
      ))}

      <section class="tile settings-group" aria-label="This dashboard">
        <h2 class="tile-title">This dashboard</h2>
        <NameField
          value={view.name}
          input={inputs.name}
          reset={resets.includes('name')}
          error={errors.name}
          disabled={readOnly || saving}
          onInput={(v) => edit('name', v)}
          onUseFallback={() => useFallback('name')}
        />
        {TOGGLES.map((t) => (
          <ToggleField
            key={t.key}
            id={`web-${t.key}`}
            label={t.label}
            hint={t.hint}
            on={inputs[t.key]}
            source={
              resets.includes(t.key) ? (
                `Back to ${fromPhrase(view[t.key].fallbackSource, t.key)} when saved`
              ) : (
                <Source value={view[t.key]} env={ENV[t.key]} format={onOff} disabled={readOnly || saving} onUseFallback={() => useFallback(t.key)} />
              )
            }
            disabled={readOnly || saving}
            onToggle={() => edit(t.key, !inputs[t.key])}
          />
        ))}
      </section>

      <section class="tile settings-group" aria-label="Access">
        <h2 class="tile-title">Access</h2>
        <p class="web-group-intro">These let anyone on your network do more. Turn them off here; turning one on is done on the server.</p>
        {view.switches.map((sw) => (
          <SwitchRow key={sw.key} sw={sw} docker={view.docker} disabled={readOnly || saving} onTurnOff={() => setConfirm(sw)} />
        ))}
      </section>

      <section class="tile settings-group" aria-label="Fixed at startup">
        <h2 class="tile-title">Fixed at startup</h2>
        <FixedRow label="Address" env="NODEDASH_LISTEN" hint="Where the dashboard listens" value={view.listen.value} source={view.listen.source} />
        <FixedRow
          label="Extra host names"
          env="NODEDASH_HOSTS"
          hint="Names on which the switches may be used"
          value={view.hosts.value?.length ? view.hosts.value.join(', ') : 'None'}
          source={view.hosts.source}
        />
        <p class="web-fixed-note">
          {view.docker ? (
            <>
              Change these in <code>docker-compose.yml</code>: a wrong address would make the dashboard unreachable.
            </>
          ) : (
            <>
              Change these on the server with <code>sudo systemctl edit node-dash</code>: a wrong address would make the dashboard unreachable.
            </>
          )}
        </p>
      </section>

      <div class="settings-footer">
        <span class={`settings-count ${nErrors > 0 ? 'tone-error' : ''}`}>
          {nErrors > 0
            ? `Fix ${nErrors} ${nErrors === 1 ? 'field' : 'fields'} to save`
            : nChanges === 0
              ? 'No changes'
              : `${nChanges} ${nChanges === 1 ? 'change' : 'changes'}`}
        </span>
        <div class="settings-buttons">
          <a class="btn btn-secondary settings-btn" href="#/">
            Cancel
          </a>
          <button type="button" class="btn btn-primary settings-btn" disabled={!canSave} onClick={onSave}>
            {saving ? 'Saving…' : 'Save changes'}
          </button>
        </div>
      </div>

      {confirm && (
        <Dialog
          title={`Turn off ${SWITCHES[confirm.key].label.toLowerCase()}?`}
          button="Turn off"
          onYes={() => turnOff(confirm)}
          onNo={() => setConfirm(null)}
          lines={[
            SWITCHES[confirm.key].offLine,
            view.docker
              ? 'It stays off after a restart. To turn it on again, delete this file next to docker-compose.yml and restart the container:'
              : 'It stays off after a restart. To turn it on again, run this on the server:',
          ]}
          command={view.docker ? `node-dash/off/${confirm.key}` : `${INSTALL} ${SWITCHES[confirm.key].option}`}
        />
      )}
    </div>
  )
}

interface SourceProps<T> {
  value: WebValue<T>
  env: string
  format: (v: T) => string
  disabled: boolean
  onUseFallback: () => void
}

/** "Default", "Environment (NODEDASH_ANCORO)", or "Set here · the environment says on · Use that". */
function Source<T>({ value, env, format, disabled, onUseFallback }: SourceProps<T>) {
  if (value.source === 'default') return <>Default</>
  if (value.source === 'environment') return <>Environment ({env})</>
  const says = value.fallbackSource === 'environment' ? 'the environment says' : 'the default is'
  return (
    <>
      Set here · {says} {format(value.fallback)} ·{' '}
      <button type="button" class="web-use" disabled={disabled} onClick={onUseFallback}>
        Use that
      </button>
    </>
  )
}

interface NameFieldProps {
  value: WebValue<string>
  input: string
  reset: boolean
  error: string | undefined
  disabled: boolean
  onInput: (v: string) => void
  onUseFallback: () => void
}

function NameField({ value, input, reset, error, disabled, onInput, onUseFallback }: NameFieldProps) {
  const id = 'web-name'
  const changed = !reset && (input.trim() || DEFAULT_NAME) !== value.value
  let note: ComponentChildren
  if (error) note = error
  else if (reset) note = `Back to “${value.fallback}”, ${fromPhrase(value.fallbackSource, 'name')}, when saved.`
  else if (changed) note = `Was “${value.value}”, ${value.source === 'default' ? 'the default' : value.source === 'web' ? 'set here' : `from ${fromPhrase(value.source, 'name')}`}.`
  else note = <Source value={value} env={ENV.name} format={(v) => `“${v}”`} disabled={disabled} onUseFallback={onUseFallback} />
  return (
    <div class="settings-field">
      <div class="settings-label">
        <label for={id}>Name</label>
        <span class="detail-hint">In the header and the browser tab</span>
      </div>
      <span class={`settings-input web-name ${error ? 'invalid' : ''}`}>
        <input
          id={id}
          autoComplete="off"
          spellcheck={false}
          aria-invalid={!!error}
          aria-describedby={`${id}-note`}
          placeholder={DEFAULT_NAME}
          value={input}
          disabled={disabled}
          onInput={(e) => onInput((e.target as HTMLInputElement).value)}
        />
      </span>
      <div class={`web-name-note ${error ? 'tone-error' : ''}`}>
        <span id={`${id}-note`} role={error ? 'alert' : undefined}>
          {note}
        </span>
        <span class="web-count" aria-hidden="true">
          {[...input.trim()].length} / {MAX_NAME}
        </span>
      </div>
    </div>
  )
}

interface ToggleFieldProps {
  id: string
  label: string
  hint: string
  on: boolean
  source: ComponentChildren
  disabled: boolean
  onToggle: () => void
}

function ToggleField({ id, label, hint, on, source, disabled, onToggle }: ToggleFieldProps) {
  return (
    <div class="settings-field">
      <div class="settings-field-row">
        <div class="settings-label">
          <label id={`${id}-label`}>{label}</label>
          <span class="detail-hint">{hint}</span>
          <span class="detail-hint">{source}</span>
        </div>
        <button
          type="button"
          role="switch"
          id={id}
          class="switch"
          aria-checked={on}
          aria-labelledby={`${id}-label`}
          disabled={disabled}
          onClick={onToggle}
        >
          <span class="switch-knob" />
        </button>
      </div>
    </div>
  )
}

function SwitchRow({ sw, docker, disabled, onTurnOff }: { sw: WebSwitch; docker: boolean; disabled: boolean; onTurnOff: () => void }) {
  const meta = SWITCHES[sw.key]
  let how: ComponentChildren = null
  if (!sw.available) {
    how = 'Not available in Docker.'
  } else if (!sw.on && sw.offSince) {
    how = docker ? (
      <>
        Turned off here on {shortDate(sw.offSince)}. To turn it on again, delete <code>node-dash/off/{sw.key}</code> and restart the container.
      </>
    ) : (
      <>
        Turned off here on {shortDate(sw.offSince)}. To turn it on again, run the installer with <code>{meta.option}</code>.
      </>
    )
  } else if (!sw.on) {
    how = docker ? (
      <>
        Turn on in <code>docker-compose.yml</code>: <code>{meta.env}=1</code>
      </>
    ) : (
      <>
        Turn on on the server: run the installer with <code>{meta.option}</code>.
      </>
    )
  }
  return (
    <div class="settings-field">
      <div class="settings-field-row">
        <div class="settings-label">
          <span class="web-switch-label">{meta.label}</span>
          <span class="detail-hint">{meta.hint}</span>
        </div>
        <div class="web-switch-state">
          <span class="web-state">
            <span class={`web-dot ${sw.on ? 'web-dot-on' : ''}`} aria-hidden="true" />
            {sw.on ? 'On' : 'Off'}
          </span>
          {sw.on && (
            <button type="button" class="btn btn-secondary web-turn-off" disabled={disabled} onClick={onTurnOff} aria-label={`Turn off ${meta.label.toLowerCase()}`}>
              Turn off
            </button>
          )}
        </div>
      </div>
      {how && <p class="settings-note">{how}</p>}
    </div>
  )
}

function FixedRow({ label, env, hint, value, source }: { label: string; env: string; hint: string; value: string; source: WebSource }) {
  return (
    <div class="settings-field">
      <div class="web-fixed-row">
        <div class="settings-label">
          <span class="web-fixed-label">
            {label} <code class="detail-key">{env}</code>
          </span>
          <span class="detail-hint">{hint}</span>
        </div>
        <div class="web-fixed-value">
          <span>{value}</span>
          <span class="detail-hint">{sourceLabel(source)}</span>
        </div>
      </div>
    </div>
  )
}
