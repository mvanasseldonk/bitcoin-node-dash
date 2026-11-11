// The settings form (story #55): change the node's main settings in
// bitcoin.conf. Only with NODEDASH_SETTINGS=1. Nothing is written until
// "Save changes"; pruning and the transaction index ask first. Changes apply
// when bitcoind restarts, and the page says so until it has.

import type { ComponentChildren } from 'preact'
import { useEffect, useRef, useState } from 'preact/hooks'
import { fetchSettings, saveSettings, SaveError, type SettingField, type SettingsForm } from './api'
import { formatBytes } from './format'
import { SETTINGS, settingValue } from './details'
import type { Progress } from './control'

const POLL_MS = 5_000

type Kind = 'number' | 'switch'

/** The form's fields: a short hint, the unit, and the kind of input. */
const FIELDS: Record<string, { hint: string; unit: string; kind: Kind }> = {
  dbcache: { hint: 'More speeds up syncing', unit: 'MiB', kind: 'number' },
  maxmempool: { hint: 'Memory for waiting transactions', unit: 'MB', kind: 'number' },
  maxconnections: { hint: 'Most peers at once, in and out', unit: '', kind: 'number' },
  listen: { hint: 'Let other nodes connect to yours', unit: '', kind: 'switch' },
  maxuploadtarget: { hint: 'Per day; 0 is unlimited', unit: 'MiB', kind: 'number' },
  prune: { hint: 'Delete old blocks to save disk', unit: 'MiB', kind: 'switch' },
  txindex: { hint: 'Look up any transaction; about 60 GB', unit: '', kind: 'switch' },
  blocksonly: { hint: 'No mempool: no fee estimates', unit: '', kind: 'switch' },
}

const GROUPS: { title: string; keys: string[] }[] = [
  { title: 'Memory', keys: ['dbcache', 'maxmempool'] },
  { title: 'Peers', keys: ['maxconnections', 'listen', 'maxuploadtarget'] },
  { title: 'Blocks and data', keys: ['prune', 'txindex', 'blocksonly'] },
]

const MIN_PRUNE = 550

/** What the form starts with: what is configured, else what runs, else the default. */
function initial(f: SettingField): number | null {
  return f.value ?? f.running ?? f.default
}

/** The form's input: a number field's text, or a switch's state. */
type Inputs = Record<string, string>

function inputsOf(form: SettingsForm): Inputs {
  const out: Inputs = {}
  for (const f of form.fields) {
    const v = initial(f)
    if (f.key === 'prune') {
      out.prune = v != null && v > 0 ? '1' : '0'
      out.pruneTarget = String(v != null && v > 0 ? v : MIN_PRUNE)
    } else {
      out[f.key] = v == null ? '' : String(v)
    }
  }
  return out
}

/** The value an input stands for; undefined when it isn't a whole number. */
function valueOf(key: string, inputs: Inputs): number | null | undefined {
  if (key === 'prune') {
    if (inputs.prune !== '1') return 0
    return /^\d+$/.test(inputs.pruneTarget.trim()) ? Number(inputs.pruneTarget.trim()) : undefined
  }
  const t = inputs[key].trim()
  if (t === '') return null
  return /^\d+$/.test(t) ? Number(t) : undefined
}

interface Check {
  changes: Record<string, number>
  errors: Record<string, string>
}

/** The changes, and what is wrong with them. Checked again on the server. */
function check(form: SettingsForm, inputs: Inputs): Check {
  const fields = Object.fromEntries(form.fields.map((f) => [f.key, f]))
  const changes: Record<string, number> = {}
  const errors: Record<string, string> = {}
  const final = (key: string): number => {
    const v = valueOf(key, inputs)
    return typeof v === 'number' ? v : (initial(fields[key]) ?? 0)
  }
  const memMiB = Math.floor(form.memTotalBytes / 2 ** 20)
  for (const f of form.fields) {
    if (f.locked) continue
    const v = valueOf(f.key, inputs)
    const was = initial(f)
    if (v === undefined) {
      errors[f.key] = 'A whole number.'
      continue
    }
    if (v === null) {
      if (was !== null) errors[f.key] = 'Enter a number.'
      continue
    }
    if (v === was) continue
    switch (f.key) {
      case 'dbcache':
        if (v < 4) errors.dbcache = 'At least 4 MiB.'
        else if (memMiB > 0 && v > memMiB) errors.dbcache = `More than this server’s memory (${memMiB.toLocaleString('en-US')} MiB).`
        break
      case 'maxmempool':
        if (v < 5) errors.maxmempool = 'At least 5 MB.'
        break
      case 'prune': {
        const wasPruned = form.pruned === true || (form.pruned === null && (f.value ?? 0) > 0)
        if (v > 1 && v < MIN_PRUNE) errors.prune = `At least ${MIN_PRUNE} MiB.`
        else if (v === 0 && wasPruned)
          errors.prune = 'This node is pruned. Keeping all blocks again means downloading the whole blockchain; that isn’t possible from here.'
        break
      }
    }
    changes[f.key] = v
  }
  if (final('prune') > 0 && final('txindex') === 1) {
    const key = 'txindex' in changes ? 'txindex' : 'prune'
    errors[key] ??= 'A pruned node can’t keep a transaction index. Turn one of them off.'
  }
  return { changes, errors }
}

/** A value in a sentence: "2,048 MiB", "on", "off"; "the default" when unknown. */
function shortValue(key: string, v: number | null): string {
  if (v == null) return 'the default'
  if (FIELDS[key]?.kind === 'switch' && (key !== 'prune' || v === 0)) return v ? 'on' : 'off'
  return settingValue(key, v)
}

/** "Database cache 2,048 MiB → 4,096 MiB"; an unset value is its default. */
function changeLine(key: string, from: number | null, to: number, def: number | null): string {
  const label = SETTINGS[key]?.label ?? key
  return `${label} ${shortValue(key, from ?? def)} → ${shortValue(key, to)}`
}

type Result = { kind: 'conflict' } | { kind: 'error'; message: string } | null

interface Props {
  /** Restarts bitcoind with node control; null when node control is off. */
  restart: (() => void) | null
  /** A running node action, from node control. */
  progress: Progress | null
}

export function SettingsPage({ restart, progress }: Props) {
  const [form, setForm] = useState<SettingsForm | null>(null)
  const [off, setOff] = useState(false)
  const [loadError, setLoadError] = useState('')
  const [inputs, setInputs] = useState<Inputs>({})
  const [serverErrors, setServerErrors] = useState<Record<string, string>>({})
  const [result, setResult] = useState<Result>(null)
  const [saving, setSaving] = useState(false)
  const [confirm, setConfirm] = useState<'prune' | 'txindex' | 'restart' | null>(null)
  const top = useRef<HTMLDivElement>(null)

  const load = async () => {
    try {
      const f = await fetchSettings(new AbortController().signal)
      if (!f) {
        setOff(true)
        return
      }
      setForm(f)
      setInputs(inputsOf(f))
      setServerErrors({})
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

  // Until bitcoind has restarted, follow it; only "saved" changes, so the
  // form keeps the version of the file it was opened with.
  const waiting = !!form?.saved?.restartNeeded
  useEffect(() => {
    if (!waiting) return
    const abort = new AbortController()
    const timer = window.setInterval(async () => {
      try {
        const f = await fetchSettings(abort.signal)
        if (f) setForm((cur) => (cur ? { ...cur, saved: f.saved } : f))
      } catch {
        // Try again at the next tick.
      }
    }, POLL_MS)
    return () => {
      abort.abort()
      window.clearInterval(timer)
    }
  }, [waiting])

  if (off) {
    return (
      <div class="details">
        <a class="settings-back" href="#/details/settings">
          ← Details
        </a>
        <h1 class="details-title">Node settings</h1>
        <p class="details-note">
          Changing settings is off. To turn it on, run the installer with <code>NODEDASH_SETTINGS=1</code>; see the README.
        </p>
      </div>
    )
  }
  if (!form) {
    return (
      <div class="details">
        <a class="settings-back" href="#/details/settings">
          ← Details
        </a>
        <h1 class="details-title">Node settings</h1>
        {loadError && <p class="details-note">Settings unavailable: {loadError}</p>}
      </div>
    )
  }

  const fields = Object.fromEntries(form.fields.map((f) => [f.key, f]))
  const { changes, errors: localErrors } = check(form, inputs)
  const errors = { ...serverErrors, ...localErrors }
  const nChanges = Object.keys(changes).length
  const nErrors = Object.keys(errors).length
  const readOnly = !!form.error
  const canSave = !readOnly && !saving && nChanges > 0 && nErrors === 0

  const set = (key: string, value: string) => {
    setServerErrors((e) => {
      const rest = { ...e }
      delete rest[key === 'pruneTarget' ? 'prune' : key]
      return rest
    })
    setInputs((cur) => {
      const next = { ...cur, [key]: value }
      // Turning pruning on turns the index off: a pruned node can't keep it.
      if (key === 'prune' && value === '1' && cur.txindex === '1' && !fields.txindex.locked) next.txindex = '0'
      return next
    })
  }

  const turningOn = (key: string) => key in changes && changes[key] > 0 && (initial(fields[key]) ?? 0) === 0

  const save = async () => {
    setConfirm(null)
    setSaving(true)
    setResult(null)
    try {
      const f = await saveSettings(form.hash, changes)
      setForm(f)
      setInputs(inputsOf(f))
      setServerErrors({})
    } catch (e) {
      if (e instanceof SaveError && e.conflict) setResult({ kind: 'conflict' })
      else if (e instanceof SaveError && Object.keys(e.fields).length > 0) setServerErrors(e.fields)
      else setResult({ kind: 'error', message: e instanceof Error ? e.message : String(e) })
    } finally {
      setSaving(false)
      top.current?.scrollIntoView({ block: 'start' })
    }
  }

  const onSave = () => {
    if (!canSave) return
    if (turningOn('prune')) setConfirm('prune')
    else if (turningOn('txindex')) setConfirm('txindex')
    else save()
  }

  const saved = form.saved?.restartNeeded ? form.saved : null
  // The restart's confirmation repeats what pruning or the index will do.
  const savedOn = (key: string) => !!saved?.changes.some((c) => c.key === key && c.to > 0 && !(c.from ?? 0))
  const step = progress && progress.action !== 'stop' && progress.current < progress.steps.length ? progress.steps[progress.current] : null

  return (
    <div class="details settings" ref={top}>
      <a class="settings-back" href="#/details/settings">
        ← Details
      </a>
      <div class="settings-intro">
        <h1 class="details-title">Node settings</h1>
        <p>
          Saved to <code>{form.path}</code>, with a backup first. Changes apply when bitcoind restarts.
        </p>
      </div>

      {saved && (
        <div class="settings-card tone-card-warning" role="status">
          <strong>{step ? `${step}…` : 'Saved · restart bitcoind to apply'}</strong>
          <p>
            {saved.changes.map((c) => changeLine(c.key, c.from, c.to, fields[c.key]?.default ?? null)).join(' · ')}. A backup of the previous bitcoin.conf is
            kept.
          </p>
          {restart ? (
            !step && (
              <button type="button" class="btn btn-primary settings-card-btn" onClick={() => setConfirm('restart')}>
                Restart now
              </button>
            )
          ) : (
            <>
              <p>Run this on the server:</p>
              <pre>sudo systemctl restart {form.service || 'bitcoind'}</pre>
            </>
          )}
        </div>
      )}
      {result?.kind === 'conflict' && (
        <div class="settings-card tone-card-error" role="alert">
          <strong>Not saved: bitcoin.conf changed</strong>
          <p>
            Someone changed the file since you opened this page, perhaps over SSH. Reload to see the new values, then make your
            change again.
          </p>
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
          <p>{form.error}</p>
        </div>
      )}

      {GROUPS.map((g) => (
        <section class="tile settings-group" key={g.title} aria-label={g.title}>
          <h2 class="tile-title">{g.title}</h2>
          {g.keys.map((key) =>
            fields[key] ? (
              <Field
                key={key}
                field={fields[key]}
                form={form}
                inputs={inputs}
                error={errors[key]}
                changed={key in changes}
                disabled={readOnly || saving}
                onInput={set}
              />
            ) : null,
          )}
        </section>
      ))}

      <div class="settings-footer">
        <span class="settings-count">
          {nErrors > 0
            ? `Fix ${nErrors} ${nErrors === 1 ? 'field' : 'fields'} to save`
            : nChanges === 0
              ? 'No changes'
              : `${nChanges} ${nChanges === 1 ? 'change' : 'changes'}`}
        </span>
        <div class="settings-buttons">
          <a class="btn btn-secondary settings-btn" href="#/details/settings">
            Cancel
          </a>
          <button type="button" class="btn btn-primary settings-btn" disabled={!canSave} onClick={onSave}>
            {saving ? 'Saving…' : 'Save changes'}
          </button>
        </div>
      </div>

      {confirm === 'prune' && (
        <Dialog
          title="Delete old blocks?"
          button="Delete old blocks"
          danger
          onYes={save}
          onNo={() => setConfirm(null)}
          lines={[
            `Your node will keep only the last ${(changes.prune ?? MIN_PRUNE).toLocaleString('en-US')} MiB of blocks and delete the rest after the restart. It stays a full, validating node.`,
            'This can’t be undone here: to keep all blocks again, it has to download the whole blockchain again, which takes days.',
            ...(changes.txindex === 0 && initial(fields.txindex) === 1
              ? ['The transaction index will be turned off, since a pruned node can’t keep it.']
              : []),
          ]}
        />
      )}
      {confirm === 'txindex' && (
        <Dialog
          title="Build a transaction index?"
          button="Build the index"
          onYes={save}
          onNo={() => setConfirm(null)}
          lines={[
            'After the restart, your node goes through every block to build the index. That takes several hours and about 60 GB of disk.',
            'The node keeps running meanwhile.',
            ...(form.diskFreeBytes > 0 ? [`Your disk has ${formatBytes(form.diskFreeBytes)} free.`] : []),
          ]}
        />
      )}
      {confirm === 'restart' && restart && (
        <Dialog
          title="Restart the Bitcoin node?"
          button="Restart"
          onYes={() => {
            setConfirm(null)
            restart()
          }}
          onNo={() => setConfirm(null)}
          lines={[
            'Your node will be offline for a few minutes.',
            ...(savedOn('prune') ? ['Pruning is turned on: old blocks will be deleted when it starts.'] : []),
            ...(savedOn('txindex') ? ['The transaction index will be built: that takes several hours and about 60 GB.'] : []),
          ]}
        />
      )}
    </div>
  )
}

interface FieldProps {
  field: SettingField
  form: SettingsForm
  inputs: Inputs
  error: string | undefined
  changed: boolean
  disabled: boolean
  onInput: (key: string, value: string) => void
}

function Field({ field, form, inputs, error, changed, disabled, onInput }: FieldProps) {
  const key = field.key
  const meta = FIELDS[key]
  const label = SETTINGS[key]?.label ?? key
  const id = `setting-${key}`
  const was = initial(field)

  let note: ComponentChildren = null
  let tone = ''
  if (error) {
    note = error
    tone = 'tone-error'
  } else if (field.locked) {
    note = field.locked
  } else if (key === 'dbcache') {
    const mem = form.memTotalBytes
    const v = valueOf('dbcache', inputs)
    const parts = []
    if (mem > 0) parts.push(`This server has ${formatBytes(mem)} of memory.`)
    if (typeof v === 'number' && mem > 0 && v * 2 ** 20 > mem / 2) {
      tone = 'tone-warning'
      parts.push('That’s more than half of it.')
    }
    if (changed) parts.push(`Was ${shortValue(key, was)}.`)
    if (field.value == null && was == null && !changed) parts.push('Not set: Bitcoin Core’s default.')
    note = parts.join(' ') || null
  } else if (key === 'listen' && inputs.listen === '0') {
    note = 'Off: your node only connects out. It still syncs.'
  } else if (key === 'txindex' && changed && inputs.txindex === '0' && inputs.prune === '1') {
    note = 'Turned off: a pruned node can’t keep a transaction index.'
  } else if (changed) {
    note = `Was ${shortValue(key, was)}.`
  }

  let control: ComponentChildren
  if (field.locked) {
    control = (
      <span class="settings-locked">
        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
          <rect x="5" y="11" width="14" height="10" rx="2" />
          <path d="M8 11V8a4 4 0 0 1 8 0v3" />
        </svg>
        {settingValue(key, field.value)}
      </span>
    )
  } else if (meta.kind === 'switch') {
    const on = inputs[key] === '1'
    control = (
      <span class="settings-controls">
        {key === 'prune' && on && (
          <NumberInput
            id={`${id}-target`}
            label={`${label} target`}
            value={inputs.pruneTarget}
            unit="MiB"
            invalid={!!error}
            disabled={disabled}
            onInput={(v) => onInput('pruneTarget', v)}
          />
        )}
        <button
          type="button"
          role="switch"
          id={id}
          class="switch"
          aria-checked={on}
          aria-labelledby={`${id}-label`}
          disabled={disabled}
          onClick={() => onInput(key, on ? '0' : '1')}
        >
          <span class="switch-knob" />
        </button>
      </span>
    )
  } else {
    control = (
      <NumberInput
        id={id}
        label={label}
        value={inputs[key] ?? ''}
        unit={meta.unit}
        placeholder={key === 'dbcache' ? 'Default' : undefined}
        invalid={!!error}
        disabled={disabled}
        onInput={(v) => onInput(key, v)}
      />
    )
  }

  return (
    <div class="settings-field">
      <div class="settings-field-row">
        <div class="settings-label">
          <label id={`${id}-label`} for={meta.kind === 'number' && !field.locked ? id : undefined}>
            {label} <code class="detail-key">{key}</code>
          </label>
          <span class="detail-hint">{meta.hint}</span>
        </div>
        {control}
      </div>
      {note && (
        <p class={`settings-note ${tone}`} id={`${id}-note`} role={error ? 'alert' : undefined}>
          {note}
        </p>
      )}
    </div>
  )
}

interface NumberInputProps {
  id: string
  label: string
  value: string
  unit: string
  placeholder?: string
  invalid: boolean
  disabled: boolean
  onInput: (v: string) => void
}

function NumberInput({ id, label, value, unit, placeholder, invalid, disabled, onInput }: NumberInputProps) {
  return (
    <span class={`settings-input ${invalid ? 'invalid' : ''}`}>
      <input
        id={id}
        inputMode="numeric"
        autoComplete="off"
        spellcheck={false}
        aria-label={unit ? `${label} in ${unit}` : label}
        aria-invalid={invalid}
        aria-describedby={`${id}-note`}
        placeholder={placeholder}
        value={value}
        disabled={disabled}
        onInput={(e) => onInput((e.target as HTMLInputElement).value)}
      />
      {unit && <span class="settings-unit">{unit}</span>}
    </span>
  )
}

interface DialogProps {
  title: string
  lines: string[]
  /** A command to run, shown after the lines. */
  command?: string
  button: string
  danger?: boolean
  onYes: () => void
  onNo: () => void
}

/** A confirmation, as a sheet like node control's; Cancel has the focus. */
export function Dialog({ title, lines, command, button, danger, onYes, onNo }: DialogProps) {
  const cancel = useRef<HTMLButtonElement>(null)
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && onNo()
    document.addEventListener('keydown', onKey)
    cancel.current?.focus()
    return () => document.removeEventListener('keydown', onKey)
  }, [])
  return (
    <div class="sheet-layer">
      <button type="button" class="sheet-overlay" aria-label="Cancel" onClick={onNo} />
      <div class="sheet settings-dialog" role="alertdialog" aria-modal="true" aria-labelledby="settings-dialog-title">
        <span class="sheet-handle" aria-hidden="true" />
        <h2 id="settings-dialog-title">{title}</h2>
        {lines.map((l) => (
          <p key={l}>{l}</p>
        ))}
        {command && <pre class="settings-command">{command}</pre>}
        <div class="sheet-actions">
          <button type="button" class={`btn ${danger ? 'btn-danger' : 'btn-primary'}`} onClick={onYes}>
            {button}
          </button>
          <button type="button" class="btn btn-secondary" ref={cancel} onClick={onNo}>
            Cancel
          </button>
        </div>
      </div>
    </div>
  )
}
