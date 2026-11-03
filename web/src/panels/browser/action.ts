import type { StepData } from '../../api/progress'
import type { Entry } from '../../api/types'

// What Alexa is doing on the page, for the overlay next to its pointer:
// a verb, the element's label (StepData.target), and what it's typing.
// Built from the running step's metadata (Appendix B.2), so it says only
// what the executor saw.

export type Typing = { text: string; secret: boolean }
// page: the action is about the page, not an element (opening an address,
// going back, reading), so it isn't shown at the pointer, and its label is
// a host rather than an element's name.
export type Action = { verb: string; label?: string; typing?: Typing; page?: boolean }

const pageTools = new Set(['browser_open', 'browser_history', 'browser_state', 'browser_read', 'browser_answer_dialog'])

// verb pairs: with an element to name, and alone.
const verbs: Record<string, [string, string]> = {
  browser_click: ['Clicking', 'Clicking'],
  browser_click_at: ['Clicking', 'Clicking'],
  browser_type: ['Typing in', 'Typing'],
  browser_key: ['Pressing a key in', 'Pressing a key'],
  browser_hover: ['Pointing at', 'Pointing'],
  browser_scroll: ['Scrolling', 'Scrolling'],
  browser_select: ['Choosing in', 'Choosing'],
  browser_drag: ['Dragging', 'Dragging'],
  browser_upload: ['Uploading to', 'Uploading'],
  browser_login: ['Signing in to', 'Signing in'],
  browser_open: ['Opening', 'Opening a page'],
  browser_history: ['Going back', 'Going back'],
  browser_answer_dialog: ['Answering', 'Answering a dialog'],
  browser_state: ['Looking at', 'Looking at the page'],
  browser_read: ['Reading', 'Reading the page'],
}

const byKind: Record<string, [string, string]> = {
  click: verbs.browser_click,
  type: verbs.browser_type,
  browse: ['Browsing', 'Browsing'],
}

// isBrowserStep says whether a step acts in the shared browser.
export const isBrowserStep = (s?: StepData) => !!s && (s.tool.startsWith('browser_') || s.kind === 'click' || s.kind === 'type')

// runningBrowserStep is the browser step in flight, if any: the last step
// entry of the feed, while it's running. It returns the entry itself, so a
// selector built on it changes only when that entry does.
export function runningBrowserStep(feed: Entry[]): Entry | null {
  for (let i = feed.length - 1, n = 0; i >= 0 && n < 12; i--, n++) {
    const e = feed[i]
    if (e.kind !== 'step') continue
    return e.status === 'running' && isBrowserStep(e.data?.step) ? e : null
  }
  return null
}

// secretMask is what a secret looks like as it's typed: always eight dots,
// so not even its length shows.
export const secretMask = '••••••••'

// actionFor is the overlay for a running step, or null when there's
// nothing worth saying.
export function actionFor(e: Entry | null): Action | null {
  const s = e?.data?.step
  if (!e || !s) return null
  const pair = verbs[s.tool] ?? byKind[s.kind]
  if (!pair) return null
  const label = s.tool === 'browser_open' ? s.host || undefined : s.target || undefined
  const verb = label ? pair[0] : pair[1]
  // A login always types a password; anything marked secret never shows.
  const secret = s.tool === 'browser_login' || !!s.secret
  let typing: Typing | undefined
  if (secret && (s.tool === 'browser_type' || s.tool === 'browser_login')) typing = { text: secretMask, secret: true }
  else if (s.tool === 'browser_type' && s.text) typing = { text: s.text, secret: false }
  const a: Action = { verb, label, typing }
  if (pageTools.has(s.tool)) a.page = true
  return a
}
