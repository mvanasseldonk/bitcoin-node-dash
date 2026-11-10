// The theme switch (story #42): System, Light or Dark, remembered in this
// browser only. index.html applies the stored choice before the first paint;
// this module changes it.

import { useEffect, useRef, useState } from 'preact/hooks'

export type Theme = 'system' | 'light' | 'dark'

/** Also read by the inline script in index.html. */
const KEY = 'nodedash-theme'
const BAR = { light: '#F2F3F5', dark: '#0E1014' }

const OPTIONS: { id: Theme; label: string }[] = [
  { id: 'system', label: 'System' },
  { id: 'light', label: 'Light' },
  { id: 'dark', label: 'Dark' },
]

function stored(): Theme {
  try {
    const v = localStorage.getItem(KEY)
    return v === 'light' || v === 'dark' ? v : 'system'
  } catch {
    return 'system' // storage blocked, e.g. a private window
  }
}

/** Sets the page's colours and the browser bar for t. */
function apply(t: Theme) {
  const root = document.documentElement
  if (t === 'system') delete root.dataset.theme
  else root.dataset.theme = t
  // Two theme-color tags, one per system scheme; a fixed choice sets both.
  for (const m of document.querySelectorAll<HTMLMetaElement>('meta[name="theme-color"]')) {
    const scheme = m.media.includes('dark') ? 'dark' : 'light'
    m.content = BAR[t === 'system' ? scheme : t]
  }
}

function save(t: Theme) {
  try {
    if (t === 'system') localStorage.removeItem(KEY)
    else localStorage.setItem(KEY, t)
  } catch {
    // Not remembered, but still applied for this visit.
  }
  apply(t)
}

function Icon({ theme }: { theme: Theme }) {
  const common = {
    width: 20,
    height: 20,
    viewBox: '0 0 24 24',
    fill: 'none',
    stroke: 'currentColor',
    'stroke-width': 1.75,
    'stroke-linecap': 'round' as const,
    'stroke-linejoin': 'round' as const,
    'aria-hidden': true,
  }
  if (theme === 'light') {
    return (
      <svg {...common}>
        <circle cx="12" cy="12" r="4" />
        <path d="M12 2.5v2M12 19.5v2M4.6 4.6l1.4 1.4M18 18l1.4 1.4M2.5 12h2M19.5 12h2M4.6 19.4L6 18M18 6l1.4-1.4" />
      </svg>
    )
  }
  if (theme === 'dark') {
    return (
      <svg {...common}>
        <path d="M20 14.5A8 8 0 0 1 9.5 4a8 8 0 1 0 10.5 10.5z" />
      </svg>
    )
  }
  return (
    <svg {...common}>
      <circle cx="12" cy="12" r="8.5" />
      <path d="M12 3.5v17a8.5 8.5 0 0 0 0-17z" fill="currentColor" stroke="none" />
    </svg>
  )
}

/** A header button whose icon shows the choice; it opens a small menu. */
export function ThemeSwitch() {
  const [theme, setTheme] = useState<Theme>(stored)
  const [open, setOpen] = useState(false)
  const button = useRef<HTMLButtonElement>(null)
  const menu = useRef<HTMLDivElement>(null)

  // Opening the menu focuses the current choice.
  useEffect(() => {
    if (open) menu.current?.querySelector<HTMLButtonElement>('[aria-checked="true"]')?.focus()
  }, [open])

  // A click outside closes the menu.
  useEffect(() => {
    if (!open) return
    const onDown = (e: PointerEvent) => {
      const t = e.target as Node
      if (!menu.current?.contains(t) && !button.current?.contains(t)) setOpen(false)
    }
    document.addEventListener('pointerdown', onDown)
    return () => document.removeEventListener('pointerdown', onDown)
  }, [open])

  const close = () => {
    setOpen(false)
    button.current?.focus()
  }
  const pick = (t: Theme) => {
    setTheme(t)
    save(t)
    close()
  }
  const onKey = (e: KeyboardEvent) => {
    if (e.key === 'Escape' || e.key === 'Tab') {
      if (e.key === 'Escape') e.preventDefault()
      close()
      return
    }
    if (e.key !== 'ArrowDown' && e.key !== 'ArrowUp') return
    e.preventDefault()
    const items = [...(menu.current?.querySelectorAll<HTMLButtonElement>('button') ?? [])]
    const i = items.indexOf(document.activeElement as HTMLButtonElement)
    const next = (i + (e.key === 'ArrowDown' ? 1 : items.length - 1)) % items.length
    items[next]?.focus()
  }

  const label = OPTIONS.find((o) => o.id === theme)?.label
  return (
    <div class="theme">
      <button
        ref={button}
        type="button"
        class="theme-btn"
        aria-label={`Theme: ${label}`}
        aria-haspopup="menu"
        aria-expanded={open}
        onClick={() => setOpen(!open)}
      >
        <Icon theme={theme} />
      </button>
      {open && (
        <div ref={menu} class="theme-menu" role="menu" aria-label="Theme" onKeyDown={onKey}>
          {OPTIONS.map((o) => (
            <button key={o.id} type="button" role="menuitemradio" aria-checked={o.id === theme} class="theme-option" onClick={() => pick(o.id)}>
              {o.label}
              {o.id === theme && (
                <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
                  <path d="M5 12.5l4.5 4.5L19 7.5" />
                </svg>
              )}
            </button>
          ))}
        </div>
      )}
    </div>
  )
}
