// The brand (story #35): the halo mark, the operator's name for their node,
// and a favicon and page title that show the status.

import { useEffect, useState } from 'preact/hooks';

const meta = (name: string) => document.querySelector<HTMLMetaElement>(`meta[name="${name}"]`)?.content

/**
 * NODEDASH_NAME, or the name set on the dashboard settings page (#61), put
 * in the page by the server; "Node" by default. Read it with useName: a save
 * on that page changes it without a reload.
 */
let name = meta('application-name') || 'Node'

/** Whether to show the link to Ancoro (story #44); off with NODEDASH_ANCORO=0 or on the settings page. */
let ancoro = meta('nodedash-ancoro') !== 'off'

const listeners = new Set<() => void>()

/** After a save on the dashboard settings page: the header, the tab title and the Ancoro line follow. */
export function setBrand(next: { name: string; ancoro: boolean }) {
  name = next.name || 'Node'
  ancoro = next.ancoro
  listeners.forEach((l) => l())
}

function useBrandValue<T>(read: () => T): T {
  const [v, setV] = useState(read)
  useEffect(() => {
    const on = () => setV(read)
    listeners.add(on)
    return () => {
      listeners.delete(on)
    }
  }, [])
  return v
}

export const useName = () => useBrandValue(() => name)
export const useAncoro = () => useBrandValue(() => ancoro)

/**
 * A quiet line under the tiles pointing to Ancoro, a transaction checker.
 * noreferrer: Ancoro doesn't learn where the dashboard runs. Nothing is
 * loaded from Ancoro until the link is clicked.
 */
export function AncoroLine() {
  return (
    <p class='ancoro'>
      Waiting on an transaction?{' '}
      <a
        href='https://www.ancoro.site'
        target='_blank'
        rel='noopener noreferrer'
      >
        Check it on Ancoro
        <span aria-hidden='true'> ↗</span>
        <span class='sr-only'> (opens in a new tab)</span>
      </a>
    </p>
  );
}

/** The halo: a disc with a ring around it, in the current text colour. */
export function Mark({ size = 20 }: { size?: number }) {
  return (
    <svg
      class='mark'
      width={size}
      height={size}
      viewBox='0 0 32 32'
      aria-hidden='true'
    >
      <circle cx='16' cy='16' r='9.5' fill='currentColor' />
      <circle
        cx='16'
        cy='16'
        r='14'
        fill='none'
        stroke='currentColor'
        stroke-width='3'
      />
    </svg>
  );
}

/** The circle's tones; favicons are drawn per tone by tools/icons. */
export type Tone =
  | 'healthy'
  | 'warning'
  | 'error'
  | 'unreachable'
  | 'starting'
  | 'loading'
  | 'busy'
  | 'stopped';

const FAVICON: Record<Tone, string> = {
  healthy: 'healthy',
  warning: 'warning',
  error: 'error',
  unreachable: 'unreachable',
  starting: 'unreachable',
  loading: 'unreachable',
  busy: 'busy',
  stopped: 'stopped',
};

/** Shows the status in the browser tab: the favicon's colour and the title. */
export function useBrand(tone: Tone, label: string) {
  const name = useName();
  useEffect(() => {
    document.title = label ? `${label} · ${name}` : name;
  }, [label, name]);
  useEffect(() => {
    const link = document.querySelector<HTMLLinkElement>('link[rel="icon"]');
    if (link) link.href = `/icons/favicon-${FAVICON[tone]}.svg`;
  }, [tone]);
}
