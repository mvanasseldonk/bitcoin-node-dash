// The brand (story #35): the halo mark, the operator's name for their node,
// and a favicon and page title that show the status.

import { useEffect } from 'preact/hooks'

/** NODEDASH_NAME, put in the page by the server; "Node" by default. */
export const NAME = document.querySelector<HTMLMetaElement>('meta[name="application-name"]')?.content || 'Node'

/** The halo: a disc with a ring around it, in the current text colour. */
export function Mark({ size = 20 }: { size?: number }) {
  return (
    <svg class="mark" width={size} height={size} viewBox="0 0 32 32" aria-hidden="true">
      <circle cx="16" cy="16" r="9.5" fill="currentColor" />
      <circle cx="16" cy="16" r="14" fill="none" stroke="currentColor" stroke-width="3" />
    </svg>
  )
}

/** The circle's tones; favicons are drawn per tone by tools/icons. */
export type Tone = 'healthy' | 'warning' | 'error' | 'unreachable' | 'starting' | 'loading' | 'busy' | 'stopped'

const FAVICON: Record<Tone, string> = {
  healthy: 'healthy',
  warning: 'warning',
  error: 'error',
  unreachable: 'unreachable',
  starting: 'unreachable',
  loading: 'unreachable',
  busy: 'busy',
  stopped: 'stopped',
}

/** Shows the status in the browser tab: the favicon's colour and the title. */
export function useBrand(tone: Tone, label: string) {
  useEffect(() => {
    document.title = label ? `${label} · ${NAME}` : NAME
  }, [label])
  useEffect(() => {
    const link = document.querySelector<HTMLLinkElement>('link[rel="icon"]')
    if (link) link.href = `/icons/favicon-${FAVICON[tone]}.svg`
  }, [tone])
}
