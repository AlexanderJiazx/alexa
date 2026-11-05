import { useEffect, useMemo } from 'react'
import { api } from '../api/client'
import type { Entry, SessionState, Surface } from '../api/types'
import type { RunInfo } from './reducer'
import { quietJob } from '../reflect/exchanges'
import { useFeed, useLoaded } from './selectors'
import { createStore, dispatch, store, useApp, useSelector, type AppState } from './store'

// The main screen shows the current topic. An exchange starts with each
// message that sets Alexa to work while she's idle; the server decides
// whether it carries on the topic before it (data.followUp) or starts a
// new one, so the screen shows the topic's exchanges up to now. Everything
// before is in Reflect. A finished topic you've seen is put to rest when
// you come back to it much later, and you can rest it yourself (the mark,
// top left): the home board shows until something new comes in, and then
// the screen shows that topic, earlier exchanges and all if it carries on.
//
// This file also decides which panel the screen shows. The session's
// surface says what Alexa wants on screen; the client follows it, except
// when following would pull the screen out from under the user (Appendix
// C.2): then the panel stays and a pill offers the switch instead.

// How long after it finished an exchange you've seen stays on screen
// (Appendix C.5: the home shows once the last one ended 6 hours ago).
const fresh = 6 * 3600e3

// startBefore returns where the exchange ending at feed[end] begins: the
// last entry marked as a start. Entries from before exchanges were marked
// begin at the last message or scheduled job. In a marked feed, entries
// before the first mark are taken as the end of an exchange that began
// before the loaded page (needsEarlier fetches more): a message there may
// have been one sent while Alexa worked, not a start. The server, which
// reads a long window, splits them at messages instead.
function startBefore(feed: Entry[], end: number, marked: boolean) {
  for (let i = end - 1; i >= 0; i--) if (feed[i].data?.start) return i
  if (marked) return 0
  for (let i = end - 1; i >= 0; i--) if (feed[i].kind === 'user' || feed[i].data?.scheduled) return i
  return 0
}

const isMarked = (feed: Entry[]) => feed.some((e) => e.data?.start)

// exchangeStart returns where the latest exchange begins in the feed.
export const exchangeStart = (feed: Entry[]) => startBefore(feed, feed.length, isMarked(feed))

// carriesOn says whether an exchange carries on the topic before it: the
// server says so, or, while it's still deciding, that topic was on screen
// (not put to rest) when the message went.
function carriesOn(first: Entry, before: Entry[] | undefined, rested: number) {
  if (first.data?.followUp) return true
  if (first.data?.topic !== 'pending' || !before?.length) return false
  return before[before.length - 1].id > rested
}

export type Split = {
  shown: Entry[] // the latest exchange the screen shows
  topic: Entry[] // the topic it's part of, from its first exchange on
  job: Entry[] | null // the latest exchange, when it's a quiet scheduled job
}

// split reads the end of the feed. A quiet scheduled job's activity is its
// own (IX-F5): it stays off the screen, the exchange before it doesn't take
// on its working line, and one between a topic's exchanges is left out of
// it. rested is the mark the topic was put to rest at, if any.
export function split(feed: Entry[], rested = 0): Split {
  const marked = isMarked(feed)
  const last = feed[feed.length - 1]
  if (last && rested > last.id) rested = 0 // from before the entries were renumbered
  const parts: Entry[][] = []
  let job: Entry[] | null = null
  let end = feed.length
  while (end > 0) {
    const start = startBefore(feed, end, marked)
    const ex = feed.slice(start, end)
    end = start
    if (quietJob(ex)) {
      if (!parts.length) job ??= ex
      continue
    }
    if (parts.length && !carriesOn(parts[0][0], ex, rested)) break
    parts.unshift(ex)
  }
  return { shown: parts[parts.length - 1] ?? [], topic: parts.flat(), job }
}

// current returns the latest exchange the main screen shows. A quiet
// scheduled job stays off it, and the exchange before it stays in view.
export const current = (feed: Entry[]) => split(feed).shown

// jobLabel is what a scheduled job is called: its note reads "Scheduled:
// <name>".
export function jobLabel(job: Entry[]) {
  const t = job[0]?.text ?? ''
  return t.replace(/^Scheduled:\s*/, '') || 'A scheduled job'
}

// onScreen returns what the main screen shows: the current topic, unless
// it was put to rest and nothing has come in since. A mark from before the
// conversation's entries were renumbered (a new server) is ignored.
export function onScreen(feed: Entry[], rested: number): Entry[] {
  const { topic } = split(feed, rested)
  const end = topic[topic.length - 1]
  const last = feed[feed.length - 1]
  if (!end || (last && rested > last.id)) return topic
  return end.id > rested ? topic : []
}

// needsEarlier says whether the loaded feed may not reach back to where the
// current topic began: no entry in it starts an exchange, or the topic
// carries on from before the first one loaded, and there's more before it.
export function needsEarlier(feed: Entry[], hasMore: boolean) {
  if (!hasMore || !feed.length) return false
  if (!isMarked(feed)) return true
  const first = split(feed).topic[0]
  const d = first?.data
  return first === feed[0] && (!d?.start || !!d.followUp || d.topic === 'pending')
}

// latestSaid is the last thing Alexa said since the user last spoke, for the
// caption.
export function latestSaid(entries: Entry[]): Entry | null {
  for (let i = entries.length - 1; i >= 0; i--) {
    const k = entries[i].kind
    if (k === 'user') break
    if (k === 'say' || k === 'error') return entries[i]
  }
  return null
}

// followUps is what Alexa suggests the user might say next: at most three
// chips under its final say, once it has stopped working. Anything after
// the say (a new message, a new exchange) takes them away.
export function followUps(entries: Entry[], working: boolean): { id: number; items: string[] } | null {
  const last = entries[entries.length - 1]
  if (last?.kind !== 'say' || (working && !last.data?.final)) return null
  const seen = new Set<string>()
  const items: string[] = []
  for (const raw of last.data?.suggestions ?? []) {
    const s = typeof raw === 'string' ? raw.trim() : ''
    const k = s.toLowerCase()
    if (!s || seen.has(k)) continue
    seen.add(k)
    items.push(s)
    if (items.length === 3) break
  }
  return items.length ? { id: last.id, items } : null
}

// ---------- what was put away and what was seen, per device ----------

const read = (key: string) => {
  try {
    return Number(localStorage.getItem(key)) || 0
  } catch {
    return 0
  }
}

const write = (key: string, id: number) => {
  try {
    localStorage.setItem(key, String(id))
  } catch {
    /* storage unavailable */
  }
}

// rested is the last entry when the topic on screen was put to rest. It's
// kept under the key the hard cut it replaced used, so a device that had
// put an exchange away keeps the board.
const marks = createStore({ user: 0, rested: 0 })
let seen = 0

const userId = () => store.get().user?.id ?? 0
const lastId = () => {
  const f = store.get().data.feed
  return f.length ? f[f.length - 1].id : 0
}

function setRested(id: number) {
  marks.set((m) => ({ ...m, rested: id }))
  write(`alexa-hidden-${userId()}`, id)
}

// clearExchange puts the topic on screen to rest (it's in Reflect): the
// board shows until something new comes in.
export const clearExchange = () => setRested(lastId())

// restoreExchange brings a rested topic back on screen, to pick it up
// again from the exchange that starts at start.
export const restoreExchange = (start: Entry) => setRested(Math.min(marks.get().rested, start.id - 1))

// restedAt is the mark, for code outside React.
export const restedAt = () => marks.get().rested

// What's on screen while the app is in view counts as seen.
function markSeen() {
  const id = lastId()
  if (document.visibilityState !== 'visible' || id === seen) return
  seen = id
  write(`alexa-seen-${userId()}`, id)
}

// stale says whether the topic on screen is due to be put to rest: you've
// seen how it ends, and that was more than six hours ago (Appendix C.5).
// One still waiting on your answer that long goes too: the board greets
// you with the question first, under Needs you, and answering it there
// brings the topic back (WP-08).
export function stale(s: SessionState, ex: Entry[], seenId: number, now: number) {
  const end = ex[ex.length - 1]
  if (!end || end.id > seenId || now - end.updatedAt <= fresh) return false
  return s.status === 'idle' || (s.status === 'needs' && s.needs?.kind === 'question')
}

// Coming back to a topic you've seen, long after it finished, you're
// greeted afresh.
function tidy() {
  const d = store.get().data
  const shown = onScreen(d.feed, marks.get().rested)
  if (stale(d.session, shown, seen, Date.now())) clearExchange()
}

// useExchange returns the entries the main screen shows: the current
// topic, or nothing while it rests.
export function useExchange(): Entry[] {
  const feed = useFeed()
  const rested = useSelector(marks, (m) => m.rested)
  return useMemo(() => onScreen(feed, rested), [feed, rested])
}

// topicStartId is the first entry of the topic on screen, or 0.
export const topicStartId = (feed: Entry[]) => split(feed, marks.get().rested).topic[0]?.id ?? 0

// useBackgroundJob is the scheduled job Alexa is busy with off screen, if
// any: the stage shows it as a quiet chip, not as the exchange's work.
export function useBackgroundJob(): string | null {
  return useApp((s) => {
    if (s.data.session.status !== 'working') return null
    const { job } = split(s.data.feed)
    return job ? jobLabel(job) : null
  })
}

// useExchangeKeeper keeps the marks: it reads them for the signed-in user
// and puts a seen exchange away when its time is up. The stage runs it once.
export function useExchangeKeeper() {
  const uid = useApp((s) => s.user?.id ?? 0)
  const loaded = useLoaded()
  const last = useApp((s) => s.data.feed[s.data.feed.length - 1]?.id ?? 0)

  useEffect(() => {
    marks.set({ user: uid, rested: read(`alexa-hidden-${uid}`) })
    seen = read(`alexa-seen-${uid}`)
  }, [uid])

  useEffect(() => {
    if (!loaded) return
    tidy()
    markSeen()
    const onVis = () => {
      if (document.visibilityState !== 'visible') return
      tidy()
      markSeen()
    }
    document.addEventListener('visibilitychange', onVis)
    const timer = setInterval(tidy, 5 * 60e3)
    return () => {
      document.removeEventListener('visibilitychange', onVis)
      clearInterval(timer)
    }
  }, [loaded, uid])

  useEffect(() => {
    if (loaded) markSeen()
  }, [loaded, last])
}

// ---------- following the session's surface (Appendix C.1, C.2) ----------

// surfaceKey says which panel a session's surface calls for: browser,
// report:<id> or entry:<id>. The browser shows whenever the user is in
// control of it.
export function surfaceKey(s: SessionState): string | null {
  const { surface, control } = s
  if (surface.kind === 'browser' || control) return 'browser'
  if ((surface.kind === 'report' || surface.kind === 'entry') && surface.id) return `${surface.kind}:${surface.id}`
  return null
}

// surfaceOf turns a panel key back into a surface.
export function surfaceOf(key: string | null): Surface {
  if (!key) return { kind: '' }
  const i = key.indexOf(':')
  if (i < 0) return { kind: key as Surface['kind'] }
  return { kind: key.slice(0, i) as Surface['kind'], id: key.slice(i + 1) }
}

// A take-over or a sign-in needs the user in the browser, so it shows
// whatever else is going on (R8).
const forced = (s: SessionState) => surfaceKey(s) === 'browser' && (s.control || (s.status === 'needs' && (s.needs?.kind === 'takeover' || s.needs?.kind === 'login')))

// Follow is what the panel slot shows. shown is on screen; server is what
// the session last asked for; pending is a switch held back, which the
// pill offers; userAt is when the user last chose the surface themselves.
export type Follow = { shown: string | null; server: string | null; pending: string | null; userAt: number; synced: boolean }

// Guards are what's true of the screen at the moment Alexa switches it.
export type Guards = {
  typing: boolean // the composer has text
  scrolled: boolean // the exchange column is scrolled up more than 120px
  overlay: boolean // Reflect, a sheet or the menu is open
  // The switch comes from a scheduled job at work, which never moves the
  // screen (R6). A server that still moves it for one is held back here.
  background?: boolean
}

export const followStart: Follow = { shown: null, server: null, pending: null, userAt: 0, synced: false }

// A switch of the user's own makes Alexa's next one wait this long.
export const settleMs = 10e3

// holdReason says why a switch Alexa asks for should wait, or '' when it
// can go ahead.
export function holdReason(f: Follow, g: Guards, now: number): '' | 'typing' | 'scrolled' | 'overlay' | 'background' | 'recent' {
  if (g.typing) return 'typing'
  if (g.scrolled) return 'scrolled'
  if (g.overlay) return 'overlay'
  if (g.background) return 'background'
  if (f.userAt && now - f.userAt < settleMs) return 'recent'
  return ''
}

export type Change = { by: 'user' | 'server' | 'forced'; now: number; guards: Guards }

// follow works out what the panel slot shows once the session's surface is
// next. The user's own switches, and the ones the user must act on, always
// go through. Alexa's switches go through unless one of the guards holds;
// then the panel stays and the switch waits as pending. A close only closes
// the panel it was about, so a panel kept on screen survives Alexa closing
// the one it held back.
export function follow(f: Follow, next: string | null, c: Change): Follow {
  if (c.by !== 'server' || !f.synced) return same(f, { shown: next, server: next, pending: null, userAt: c.by === 'user' ? c.now : f.userAt, synced: true })
  if (next === f.server) return f
  if (next === null) return { ...f, shown: f.shown === f.server ? null : f.shown, server: null, pending: null }
  if (next === f.shown) return { ...f, server: next, pending: null }
  if (holdReason(f, c.guards, c.now)) return { ...f, server: next, pending: next }
  return { ...f, shown: next, server: next, pending: null }
}

// newExchange lets go of a panel kept on screen against the session's
// wishes when a new topic starts, and of the pill: the screen shows what
// the session does (R2 has closed its panel, unless the user held it, or
// the new request's run has put something there already: the topic can be
// settled a few seconds after the message).
export function newExchange(f: Follow): Follow {
  if (f.shown === f.server && !f.pending) return f
  return { ...f, shown: f.server, pending: null }
}

// offerJob is the pill for a scheduled job in the browser (R6, IX-F5). The
// job never moves the screen, so while the user reads a report (or any
// panel but the browser) and the job browses, the pill offers to show it
// instead. The stage offers it once per job.
export function offerJob(f: Follow, browsing: boolean): Follow {
  if (!browsing || !f.synced || f.pending || !f.shown || f.shown === 'browser') return f
  return { ...f, pending: 'browser' }
}

// settle drops a browser pill once Alexa is idle: she isn't in the browser
// any more. A report's pill stays, since the report is still new.
export function settle(f: Follow, idle: boolean): Follow {
  return idle && f.pending === 'browser' ? { ...f, pending: null } : f
}

// backgroundJob is the scheduled job Alexa is at work on, if the latest
// exchange is one: from its note to now, said or quiet.
export function backgroundJob(session: SessionState, feed: Entry[]): Entry[] | null {
  if (session.status === 'idle') return null
  const start = exchangeStart(feed)
  return feed[start]?.data?.scheduled ? feed.slice(start) : null
}

// What a step does in the browser, as opposed to reading a page without it.
const browserKinds = new Set(['browse', 'click', 'type'])

// inBrowser says whether a job's latest work is in the browser: the tool
// running now, or the last step it took.
export function inBrowser(job: Entry[], run: RunInfo | null): boolean {
  if (run?.phase === 'tool' && run.tool) return browserKinds.has(run.tool.kind)
  for (let i = job.length - 1; i >= 0; i--) {
    const step = job[i].data?.step
    if (job[i].kind === 'step') return !!step && (browserKinds.has(step.kind) || step.tool.startsWith('browser'))
  }
  return false
}

function same(f: Follow, n: Follow): Follow {
  return f.shown === n.shown && f.server === n.server && f.pending === n.pending && f.userAt === n.userAt && f.synced === n.synced ? f : n
}

export const followStore = createStore<Follow>(followStart)

// The surface changes that are the user's own come from actions.show (and
// the pill and the Following chip here), run inside the click or key press
// that asked for them. userSwitch marks the next change as the user's; a
// change made while a user's event is being handled counts too, unless that
// event is the socket's.
let userNext = false
export function userSwitch() {
  userNext = true
  queueMicrotask(() => {
    userNext = false
  })
}

function byUser() {
  if (userNext) return true
  const ev = typeof window === 'undefined' ? undefined : window.event
  if (!ev) return false
  return !(ev.type === 'message' && typeof WebSocket !== 'undefined' && ev.target instanceof WebSocket)
}

const overlayOpen = (s: AppState) => s.ui.reflect || !!s.ui.sheet || s.ui.menu

// primeFollowing starts the panel slot afresh, from the session as it is: a
// stage that opens on a loaded session shows its panel from the first
// frame. The stage calls it once as it mounts, before startFollowing.
export function primeFollowing() {
  const d = store.get().data
  const key = d.loaded ? surfaceKey(d.session) : null
  followStore.set(d.loaded ? { ...followStart, shown: key, server: key, synced: true } : followStart)
}

// startFollowing keeps followStore in step with the session. readGuards
// reports what's on screen (the composer's text, the column's scroll) at
// the moment of a switch. It returns the unsubscribe.
export function startFollowing(readGuards: () => Pick<Guards, 'typing' | 'scrolled'>) {
  let session: SessionState | null = null
  let feed: Entry[] | null = null
  let run: RunInfo | null = null
  let startId = -1
  // The job the pill was last offered for, so it's offered once per job.
  let offered = 0
  const check = () => {
    const s = store.get()
    const d = s.data
    if (!d.loaded) return
    const moved = d.feed !== feed || d.session !== session || d.run !== run
    if (d.feed !== feed) {
      feed = d.feed
      // A new topic lets go of what was kept on screen; a follow-up keeps it.
      const id = topicStartId(feed)
      if (startId >= 0 && id !== startId && followStore.get().synced) followStore.set(newExchange)
      startId = id
    }
    run = d.run
    if (!moved) return
    const job = backgroundJob(d.session, d.feed)
    if (d.session !== session) {
      session = d.session
      const by = byUser() ? 'user' : forced(session) ? 'forced' : 'server'
      const guards = by === 'server' ? { ...readGuards(), overlay: overlayOpen(s), background: !!job } : { typing: false, scrolled: false, overlay: false }
      followStore.set((f) => settle(follow(f, surfaceKey(session!), { by, now: Date.now(), guards }), session!.status === 'idle'))
    }
    if (job && job[0].id !== offered) {
      if (followStore.get().pending !== 'browser' && inBrowser(job, d.run)) followStore.set((f) => offerJob(f, true))
      // Offered, by this or by a switch held back: once is enough.
      if (followStore.get().pending === 'browser') offered = job[0].id
    }
  }
  check()
  return store.subscribe(check)
}

// useFollow reads what the panel slot shows and what's held back.
export function useFollow<T>(select: (f: Follow) => T): T {
  return useSelector(followStore, select)
}

// showPending takes the switch the pill offers. The browser keeps
// following Alexa from there; a report is the user's to read, so it holds.
export function showPending() {
  const key = followStore.get().pending
  if (!key) return
  const s = surfaceOf(key)
  userSwitch()
  dispatch({ type: 'surface', surface: s })
  api.setSurface(s.kind === 'browser' ? { ...s, follow: true } : s).catch(() => {})
}

// dismissPending puts the pill away and leaves the screen as it is.
export const dismissPending = () => followStore.set((f) => (f.pending ? { ...f, pending: null } : f))

// isFollowing says whether Alexa's work moves the screen: the server's
// follow flag, or (from a server without it) whether the user holds it.
export const isFollowing = (s: SessionState) => s.follow ?? !s.surface.held

// setFollowing turns follow mode on or off from the panel's Following chip:
// off holds what's on screen, on lets Alexa move it again.
export function setFollowing(on: boolean) {
  const d = store.get().data
  const s = d.session.surface
  userSwitch()
  dispatch({ type: 'session', session: { ...d.session, follow: on } })
  api.setSurface({ kind: s.kind, id: s.id, follow: on }).catch(() => {})
}
