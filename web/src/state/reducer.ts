import type { RunData } from '../api/progress'
import type { ChangedWhat, ServerEvent } from '../api/realtime'
import type { AppNotification, Entry, FeedPage, Report, SessionData, SessionState, Surface } from '../api/types'

// The reducer keeps the client's copy of the session in step with the
// server: the snapshot from GET /api/session plus the events that follow it
// over the socket. It is pure, so every ordering problem can be tested.

// RunInfo is the latest run lifecycle event (Appendix B.3). The headline is
// carried across a run's events, so the working line keeps its words while
// tools come and go.
export type RunInfo = RunData & { seq?: number }

// Sync tracks a snapshot fetch in flight. Events that arrive meanwhile are
// buffered and applied after it, but only those newer than the snapshot
// (FE-01). again asks the connection for another snapshot: a resync event,
// or a reset that came in during the fetch.
export type Sync = { pending: boolean; buffer: ServerEvent[]; again: boolean; seq?: number; epoch?: string }

export type Data = {
  session: SessionState
  feed: Entry[]
  hasMore: boolean
  // loaded is true once the first snapshot has arrived.
  loaded: boolean
  reports: Record<string, Report>
  notes: AppNotification[]
  run: RunInfo | null
  // A counter per kind of server-side change (WP-18), so whatever shows that
  // data can refetch when it moves.
  changed: Partial<Record<ChangedWhat, number>>
  sync: Sync
}

export const blankSession: SessionState = { status: 'idle', activity: '', surface: { kind: '' }, control: false, contextTokens: 0, compactions: 0 }

export const initialData: Data = {
  session: blankSession,
  feed: [],
  hasMore: false,
  loaded: false,
  reports: {},
  notes: [],
  run: null,
  changed: {},
  sync: { pending: false, buffer: [], again: false },
}

export type Action =
  | { type: 'syncStart' }
  | { type: 'snapshot'; snap: SessionData; notes?: AppNotification[] }
  | { type: 'syncFailed' }
  | { type: 'event'; ev: ServerEvent }
  | { type: 'earlier'; page: FeedPage }
  | { type: 'surface'; surface: Surface }
  | { type: 'session'; session: SessionState }
  | { type: 'report'; report: Report }
  | { type: 'notes'; notes: AppNotification[] }
  | { type: 'notesRead' }
  | { type: 'clear' }

export function reduce(d: Data, a: Action): Data {
  switch (a.type) {
    case 'syncStart':
      return { ...d, sync: { ...d.sync, pending: true, buffer: [], again: false } }
    case 'snapshot':
      return snapshot(d, a.snap, a.notes)
    case 'syncFailed': {
      // No snapshot to compare with: what came in is all there is.
      let n: Data = { ...d, sync: { ...d.sync, pending: false, buffer: [] } }
      for (const ev of d.sync.buffer) n = apply(n, ev)
      return n
    }
    case 'event':
      if (a.ev.type === 'hello' || a.ev.type === 'browser') return d
      if (d.sync.pending) return { ...d, sync: { ...d.sync, buffer: [...d.sync.buffer, a.ev] } }
      return apply(d, a.ev)
    case 'earlier': {
      const first = d.feed[0]?.id ?? Infinity
      return { ...d, feed: [...a.page.feed.filter((x) => x.id < first), ...d.feed], hasMore: a.page.hasMore }
    }
    case 'surface':
      return { ...d, session: { ...d.session, surface: a.surface } }
    case 'session':
      return { ...d, session: a.session }
    case 'report':
      return putReport(d, a.report)
    case 'notes':
      return { ...d, notes: a.notes }
    case 'notesRead':
      return d.notes.some((n) => !n.read) ? { ...d, notes: d.notes.map((n) => ({ ...n, read: true })) } : d
    case 'clear':
      return initialData
  }
}

// apply applies one live event.
function apply(d: Data, ev: ServerEvent): Data {
  const n = withSeq(d, ev)
  switch (ev.type) {
    case 'state':
      return ev.data ? { ...n, session: ev.data } : n
    case 'entry':
      return ev.data ? { ...n, feed: upsert(n.feed, ev.data) } : n
    case 'entryStatus': {
      const s = ev.data
      if (!s) return n
      const i = n.feed.findIndex((x) => x.id === s.id)
      if (i < 0 || n.feed[i].status === s.status) return n
      const feed = n.feed.slice()
      feed[i] = { ...feed[i], status: s.status }
      return { ...n, feed }
    }
    case 'report':
      // A new or revised report also counts as a change to the list of
      // reports, so an open list (Reflect, the home board) refetches.
      return ev.data ? bump(putReport(n, ev.data), 'reports') : n
    case 'notification': {
      const nt = ev.data
      if (!nt) return n
      return { ...n, notes: [nt, ...n.notes.filter((x) => x.id !== nt.id)] }
    }
    case 'reset':
      return { ...n, feed: [], hasMore: false }
    case 'resync':
      return { ...n, sync: { ...n.sync, again: true } }
    case 'run':
      return ev.data ? run(n, { ...ev.data, seq: ev.seq ?? ev.data.seq }) : n
    case 'changed': {
      const w = ev.data?.what
      return w ? bump(n, w) : n
    }
  }
  return n
}

function withSeq(d: Data, ev: ServerEvent): Data {
  if (typeof ev.seq !== 'number') return d
  if (d.sync.seq !== undefined && ev.seq <= d.sync.seq && (!ev.epoch || ev.epoch === d.sync.epoch)) return d
  return { ...d, sync: { ...d.sync, seq: ev.seq, epoch: ev.epoch ?? d.sync.epoch } }
}

// run records a lifecycle event: the phase and when the run started go on
// the session, the event itself on run.
function run(d: Data, ev: RunInfo): Data {
  const prev = d.run
  const same = prev && prev.runId === ev.runId
  const info: RunInfo = same ? { ...ev, headline: ev.headline ?? prev.headline } : ev
  const session: SessionState = { ...d.session, phase: ev.phase }
  if (ev.phase === 'started' || (!same && session.runStartedAt === undefined)) session.runStartedAt = ev.at
  return { ...d, run: info, session }
}

function bump(d: Data, what: ChangedWhat): Data {
  return { ...d, changed: { ...d.changed, [what]: (d.changed[what] ?? 0) + 1 } }
}

function putReport(d: Data, r: Report): Data {
  const cur = d.reports[r.id]
  if (cur && cur.updatedAt > r.updatedAt) return d
  return { ...d, reports: { ...d.reports, [r.id]: r } }
}

// newer says whether entry a should replace entry b: by revision when both
// have one, else by update time. A tie goes to a, which arrived later.
export function newer(a: Entry, b: Entry) {
  if (a.rev !== undefined && b.rev !== undefined) return a.rev >= b.rev
  return a.updatedAt >= b.updatedAt
}

// upsert puts an entry into the feed, which stays in id order: events from
// different goroutines can arrive slightly out of order. An older copy of an
// entry never replaces a newer one.
export function upsert(feed: Entry[], e: Entry): Entry[] {
  const i = feed.findIndex((x) => x.id === e.id)
  if (i >= 0) {
    if (!newer(e, feed[i])) return feed
    const n = feed.slice()
    n[i] = e
    return n
  }
  if (!feed.length || feed[feed.length - 1].id < e.id) return [...feed, e]
  const at = feed.findIndex((x) => x.id > e.id)
  return [...feed.slice(0, at), e, ...feed.slice(at)]
}

// mergeFeed merges a snapshot's page of the feed into what the client has.
// The snapshot is the truth for the range it covers. Older entries the user
// loaded with "Show earlier" are kept when they join up with it (FE-12); if
// they don't (more happened while away than one page holds), or the server
// has lost entries the client has (a reset, a new server), the snapshot
// replaces everything.
export function mergeFeed(cur: Entry[], curMore: boolean, snap: Entry[], snapMore: boolean): { feed: Entry[]; hasMore: boolean } {
  if (!cur.length) return { feed: snap, hasMore: snapMore }
  if (!snap.length) return { feed: [], hasMore: snapMore }
  const first = snap[0].id
  const last = snap[snap.length - 1].id
  const curLast = cur[cur.length - 1].id
  const overlaps = curLast >= first
  if (curLast > last || !overlaps || !snapMore) {
    return { feed: keepNewer(cur, snap), hasMore: snapMore }
  }
  const older = cur.filter((e) => e.id < first)
  return { feed: [...older, ...keepNewer(cur, snap)], hasMore: older.length ? curMore : snapMore }
}

// keepNewer returns the snapshot's entries, keeping the client's copy of any
// it has a newer revision of.
function keepNewer(cur: Entry[], snap: Entry[]) {
  if (!cur.length) return snap
  const byId = new Map(cur.map((e) => [e.id, e]))
  return snap.map((e) => {
    const c = byId.get(e.id)
    return c && !newer(e, c) ? c : e
  })
}

// fresh says whether a buffered event is newer than the snapshot. With
// sequence numbers that's exact. Without them every buffered event is
// applied: entries and reports only ever replace older copies, a state
// event older than the snapshot can only repeat it, and a status only moves
// forward.
export function fresh(ev: ServerEvent, snap: SessionData) {
  if (typeof ev.seq === 'number' && typeof snap.seq === 'number' && (!ev.epoch || !snap.epoch || ev.epoch === snap.epoch)) return ev.seq > snap.seq
  return true
}

function snapshot(d: Data, snap: SessionData, notes?: AppNotification[]): Data {
  const { feed, hasMore } = mergeFeed(d.feed, d.hasMore, snap.feed || [], snap.hasMore)
  let n: Data = {
    ...d,
    session: snap.state || d.session,
    feed,
    hasMore,
    loaded: true,
    notes: notes ?? d.notes,
    sync: { pending: false, buffer: [], again: d.sync.again, seq: snap.seq ?? d.sync.seq, epoch: snap.epoch ?? d.sync.epoch },
  }
  if (snap.report) n = putReport(n, snap.report)
  for (const ev of d.sync.buffer) {
    if (!fresh(ev, snap)) continue
    // A reset that came in while the snapshot was on its way may be older
    // or newer than it; only another snapshot can tell.
    if (ev.type === 'reset' && typeof ev.seq !== 'number') {
      n = { ...n, sync: { ...n.sync, again: true } }
      continue
    }
    n = apply(n, ev)
  }
  return n
}
