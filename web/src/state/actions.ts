import { api } from '../api/client'
import type { Tap, Via } from '../api/blocks'
import type { ServerEvent } from '../api/realtime'
import type { Attachment, Surface, User } from '../api/types'
import { focusComposer } from '../composer/focus'
import { submit } from '../composer/outbox'
import { notify } from '../sheets/toasts'
import { undo } from '../sheets/undo'
import { resync } from './connection'
import { clearExchange, userSwitch } from './exchange'
import { dispatch, setApp, setUI, store, type ReflectTab, type SettingsSection, type SheetName } from './store'

// Actions are what the UI can do. They live outside React, so any component
// calls them directly instead of having callbacks passed down to it. None of
// them makes a hidden turn: what reaches Alexa is always a visible message.

// ---------- toasts ----------

// toast shows a note that something went wrong. Good news and notes with
// an action (Undo) go through notify in sheets/toasts.
export function toast(title: string, body = '') {
  notify({ title, body: body || undefined, tone: 'bad' })
}

// plainly says why something failed in plain words, for a toast: never the
// server's shorthand ("not found", "Failed to fetch", "HTTP 502"). gone is
// what a 404 means for what was asked for. What the server words for
// people (Alexa is busy, or isn't waiting) is said as it is.
export function plainly(e: unknown, gone = 'That isn’t there any more.'): string {
  const status = (e as { status?: unknown } | null)?.status
  if (status === 404) return gone
  if (status === 409 && (e as Error).message) return (e as Error).message
  const offline = typeof navigator !== 'undefined' && navigator.onLine === false
  if (offline || (e instanceof TypeError && /fetch|network|load failed/i.test(e.message))) return 'You’re offline.'
  return 'Try again in a moment.'
}

const act = (fn: () => Promise<unknown>) => () => {
  fn().catch((e) => toast('Something went wrong', plainly(e)))
}

// ---------- messages ----------

// send sends a visible user message through the outbox, so it goes with a
// clientId (a retry is the same message) and the composer shows its
// receipt, Couldn’t send and Retry included. via says how the app made it
// when the user didn't type it. It resolves with whether it was sent.
export async function send(text: string, attachments: Attachment[], viaVoice: boolean, via?: Via) {
  const it = await submit({ text, attachments, voice: viaVoice, via }, 'ask')
  return it.state !== 'failed'
}

// answer answers what Alexa asked, as a visible message.
export const answer = (text: string) => send(text, [], false)

// reply sends a block action's reply, saying which action it was, so the
// server records the tap and Alexa reads which card it was on.
export async function reply(text: string, tap?: Tap) {
  const it = await submit({ text, via: tap ? 'button' : undefined, tap }, 'ask')
  return it.state !== 'failed'
}

export const takeOver = act(() => api.takeOver())
// handBack gives the browser back to Alexa, with an optional note for her;
// the note shows in the conversation as the user's own message.
export const handBack = (note = '') => act(() => api.handBack(note))()
export const stop = act(() => api.stop())

// ---------- the screen ----------

// Where focus was when the user opened a panel, to go back to when they
// close it.
let beforePanel: Element | null = null
const inPanel = (el: Element | null) => !!el?.closest('.panel-col, .psheet')

// show sets what the screen shows next to the exchange. The screen follows
// the session; this lets the user steer it too.
export function show(s: Surface) {
  // Whatever calls this (a click, the palette, a shortcut), it's the user's
  // own choice, so Alexa's next switch waits behind it.
  userSwitch()
  const was = store.get().data.session.surface.kind
  const active = typeof document === 'undefined' ? null : document.activeElement
  if (s.kind && !was && !inPanel(active)) beforePanel = active
  setUI({ sheet: null, menu: false })
  closeReflect()
  const prev = store.get().data.session.surface
  dispatch({ type: 'surface', surface: s })
  api.setSurface(s).catch((e) => {
    // The server kept what it had (a report or an entry that's gone sends
    // no state), so the screen goes back to it rather than disagreeing.
    // Going back is the user's own switch too: taken as Alexa's, it would
    // wait behind the one just made, leaving the panel on its error and
    // the pill offering what the user was reading as new.
    if (store.get().data.session.surface === s) {
      userSwitch()
      dispatch({ type: 'surface', surface: prev })
    }
    toast('Couldn’t open that', plainly(e, goneText[s.kind]))
  })
  // Closed from inside the panel (its Close, Escape): focus goes back to
  // where it was before it opened rather than to the page itself.
  if (!s.kind && inPanel(active)) restoreFocus()
  if (!s.kind) beforePanel = null
}

// What it means when what the user opened isn't on the server.
const goneText: Partial<Record<Surface['kind'], string>> = {
  report: 'That report isn’t there any more.',
  entry: 'That message isn’t there any more.',
}

// On a touch screen nothing focuses a text field for the user, which would
// raise the keyboard.
function restoreFocus() {
  const fine = typeof matchMedia === 'function' && matchMedia('(pointer: fine)').matches
  const el = beforePanel
  const field = el instanceof HTMLElement && (el.isContentEditable || /^(INPUT|TEXTAREA|SELECT)$/.test(el.tagName))
  if (el instanceof HTMLElement && el.isConnected && el !== document.body && !inPanel(el) && (fine || !field)) el.focus({ preventScroll: true })
  else if (fine) focusComposer()
}

export const openReport = (id: string) => show({ kind: 'report', id })
export const closePanel = () => show({ kind: '' })

// openRef opens what a notification is about: a report, or the entry of
// a question (D1). A question still waiting is answered on the main
// screen; one answered since is in Reflect, at its exchange.
export function openRef(ref?: string) {
  if (ref?.startsWith('report:')) return openReport(ref.slice(7))
  setUI({ sheet: null, menu: false })
  const id = ref?.startsWith('entry:') ? Number(ref.slice(6)) : 0
  const s = store.get().data.session
  if (!id || (s.status === 'needs' && s.needs?.entryId === id)) return closeReflect()
  openReflect('conversation')
  reveal(`.reflect [data-ask-q="${id}"]`, (q) => q.scrollIntoView({ block: 'center' }))
}

// openExchange opens Reflect at one exchange (the home board's Recent
// list), its header just under the day's pinned date.
export function openExchange(id: number) {
  openReflect('conversation')
  reveal(`.reflect [data-exchange="${id}"]`, (x, box) => {
    const pinned = box.querySelector<HTMLElement>('.day')?.offsetHeight ?? 0
    box.scrollTop += x.getBoundingClientRect().top - box.getBoundingClientRect().top - pinned
  })
}

// reveal scrolls Reflect to what selector finds once it's drawn. History
// opens at the newest on the frame its box gets a size, so this waits a
// frame more and then goes after it. Something further back than the feed
// loaded leaves Reflect at the newest.
function reveal(selector: string, place: (el: HTMLElement, box: HTMLElement) => void) {
  if (typeof document === 'undefined' || typeof requestAnimationFrame !== 'function') return
  let frames = 60
  const look = () => {
    const el = document.querySelector<HTMLElement>(selector)
    const box = el?.closest<HTMLElement>('.reflect-scroll')
    if (el && box?.clientHeight) requestAnimationFrame(() => place(el, box))
    else if (--frames > 0) requestAnimationFrame(look)
  }
  requestAnimationFrame(look)
}

// home is the mark: the panel closes and, once Alexa is done, the exchange
// is put away (it's in Reflect). One waiting on a question goes too: the
// board shows the question first, under Needs you, as stale() does.
export function home() {
  const s = store.get().data.session
  if (s.surface.kind) show({ kind: '' })
  if (s.status === 'idle' || (s.status === 'needs' && s.needs?.kind === 'question')) clearExchange()
}

// Reflect opens as a page of its own, so the browser's back button closes
// it.
export function openReflect(tab?: ReflectTab) {
  setUI(tab ? { reflectTab: tab, menu: false, sheet: null } : { menu: false, sheet: null })
  if (!history.state?.reflect) history.pushState({ reflect: true }, '')
  setUI({ reflect: true })
}

export function closeReflect() {
  if (history.state?.reflect) history.back()
  else setUI({ reflect: false })
}

export const setReflectTab = (t: ReflectTab) => setUI({ reflectTab: t })

// The back button arrived: Reflect follows the history entry.
export const syncReflect = () => setUI({ reflect: !!history.state?.reflect })

export const toggleMenu = () => setUI((u) => ({ menu: !u.menu }))
export const closeMenu = () => setUI({ menu: false })

// openSheet opens the Notifications inbox, or Settings at the section a
// name stands for (Memory, Logins, Scheduled, Users; Settings itself opens
// at its first screen).
export function openSheet(s: SheetName) {
  if (s !== 'notifications') return openSettings(s === 'settings' ? null : s)
  setUI({ menu: false, sheet: s })
  refreshNotes()
}

// openSettings opens Settings at a section, or moves it there if it's open.
export function openSettings(section: SettingsSection | null = null) {
  setUI({ menu: false, sheet: 'settings', section })
}

export function closeSheet() {
  if (store.get().ui.sheet === 'notifications') dispatch({ type: 'notesRead' })
  setUI({ sheet: null })
}

export const setVoice = (on: boolean) => setUI({ voice: on })

// ---------- data ----------

export function refreshNotes() {
  api
    .notifications()
    .then((notes) => dispatch({ type: 'notes', notes }))
    .catch(() => {})
}

export async function loadMore() {
  const f = store.get().data.feed
  if (!f.length) return
  try {
    dispatch({ type: 'earlier', page: await api.feed(f[0].id) })
  } catch (e) {
    toast('Couldn’t load earlier messages', plainly(e))
  }
}

// loadReport fetches a report this client hasn't seen yet. It resolves
// false when the report couldn't be loaded, and the panel says so with
// Try again (FE-34) instead of closing.
export function loadReport(id: string): Promise<boolean> {
  return api.report(id).then(
    (report) => {
      dispatch({ type: 'report', report })
      return true
    },
    () => false,
  )
}

export async function compact() {
  dispatch({ type: 'session', session: await api.compact() })
}

export async function resetConversation() {
  await api.reset()
  await resync()
}

export const setUser = (u: User) => setApp({ user: u })

// followChanges keeps what isn't in the conversation in step with other
// tabs and devices: the profile (a name or time zone changed elsewhere)
// and the notifications (read on another device). Sheets and lists refetch
// on their own through useChanged.
export function followChanges(ev: ServerEvent) {
  if (ev.type !== 'changed' || !ev.data) return
  if (ev.data.what === 'me')
    api
      .me()
      .then(setUser)
      .catch(() => {})
  if (ev.data.what === 'notifications') refreshNotes()
}

// signOut first sends the deletes still waiting on their Undo, while the
// session they need is still valid (FE-2).
export async function signOut() {
  await undo.flush().catch(() => {})
  await api.logout().catch(() => {})
  location.href = '/'
}

// signedOut puts the app back at sign-in, as when the session was revoked.
// It starts the page afresh, as signOut does, so nothing of this user's
// (the outbox and its Retry, the live browser, skipped questions, what the
// screen was following) is left for whoever signs in next.
export function signedOut() {
  if (store.get().boot !== 'ready') return
  dispatch({ type: 'clear' })
  setApp({ boot: 'loading', user: null })
  location.replace('/')
}
