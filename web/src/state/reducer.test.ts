import { describe, expect, it } from 'vitest'
import type { ServerEvent } from '../api/realtime'
import type { Entry, SessionData, SessionState } from '../api/types'
import { blankSession, initialData, mergeFeed, reduce, upsert, type Action, type Data } from './reducer'

const entry = (id: number, updatedAt = 100, rest: Partial<Entry> = {}): Entry => ({ id, kind: 'say', text: `e${id}`, createdAt: updatedAt, updatedAt, ...rest })
const session = (s: Partial<SessionState>): SessionState => ({ ...blankSession, ...s })
const snap = (feed: Entry[], rest: Partial<SessionData> = {}): SessionData => ({ state: session({}), feed, hasMore: false, ...rest })
const ev = (e: ServerEvent): Action => ({ type: 'event', ev: e })
const run = (d: Data, ...actions: Action[]) => actions.reduce(reduce, d)

describe('snapshot and live events (FE-01)', () => {
  it('lets events that arrived during the fetch win over an older snapshot', () => {
    const d = run(
      initialData,
      { type: 'syncStart' },
      ev({ type: 'entry', data: entry(5, 200, { text: 'new' }) }),
      ev({ type: 'entry', data: entry(6, 210) }),
      ev({ type: 'state', data: session({ status: 'idle', activity: '' }) }),
      { type: 'snapshot', snap: snap([entry(4), entry(5, 100, { text: 'old' })], { state: session({ status: 'working', activity: 'Reading' }) }) },
    )
    expect(d.feed.map((e) => e.id)).toEqual([4, 5, 6])
    expect(d.feed[1].text).toBe('new')
    expect(d.session.status).toBe('idle')
    expect(d.loaded).toBe(true)
    expect(d.sync.pending).toBe(false)
    expect(d.sync.buffer).toEqual([])
  })

  it('keeps the snapshot over a buffered event that is older than it', () => {
    const d = run(initialData, { type: 'syncStart' }, ev({ type: 'entry', data: entry(5, 90, { text: 'stale' }) }), { type: 'snapshot', snap: snap([entry(5, 100, { text: 'fresh' })]) })
    expect(d.feed[0].text).toBe('fresh')
  })

  it('applies only events after the snapshot by seq when both have one', () => {
    const d = run(
      initialData,
      { type: 'syncStart' },
      ev({ type: 'state', seq: 9, epoch: 'a', data: session({ status: 'working' }) }),
      ev({ type: 'entry', seq: 11, epoch: 'a', data: entry(7, 50) }),
      { type: 'snapshot', snap: snap([entry(6)], { seq: 10, epoch: 'a', state: session({ status: 'needs' }) }) },
    )
    expect(d.session.status).toBe('needs')
    expect(d.feed.map((e) => e.id)).toEqual([6, 7])
    expect(d.sync.seq).toBe(11)
    expect(d.sync.epoch).toBe('a')
  })

  it('applies an answered status that came in during the fetch', () => {
    const d = run(initialData, { type: 'syncStart' }, ev({ type: 'entryStatus', data: { id: 3, status: 'answered' } }), { type: 'snapshot', snap: snap([entry(3, 100, { kind: 'ask', status: 'open' })]) })
    expect(d.feed[0].status).toBe('answered')
  })

  it('asks for another snapshot when a reset came in during the fetch', () => {
    const d = run(initialData, { type: 'syncStart' }, ev({ type: 'reset' }), { type: 'snapshot', snap: snap([entry(1)]) })
    expect(d.feed.map((e) => e.id)).toEqual([1])
    expect(d.sync.again).toBe(true)
  })

  it('applies what was buffered when the snapshot fails', () => {
    const d = run(initialData, { type: 'syncStart' }, ev({ type: 'entry', data: entry(2) }), { type: 'syncFailed' })
    expect(d.feed.map((e) => e.id)).toEqual([2])
    expect(d.sync.pending).toBe(false)
  })

  it('applies events straight away when no fetch is in flight', () => {
    const d = run(initialData, ev({ type: 'entry', data: entry(1) }))
    expect(d.feed).toHaveLength(1)
    expect(d.sync.buffer).toEqual([])
  })

  it('keeps the newer copy of the report on screen', () => {
    const r = (updatedAt: number, title: string) => ({ id: 'r1', title, format: 'markdown' as const, createdAt: 1, updatedAt })
    const d = run(initialData, { type: 'syncStart' }, ev({ type: 'report', data: r(200, 'new') }), { type: 'snapshot', snap: snap([], { report: r(100, 'old') }) })
    expect(d.reports.r1.title).toBe('new')
  })
})

describe('entries', () => {
  it('keeps the feed in id order when events arrive out of order', () => {
    let f = upsert([], entry(3))
    f = upsert(f, entry(1))
    f = upsert(f, entry(2))
    f = upsert(f, entry(4))
    expect(f.map((e) => e.id)).toEqual([1, 2, 3, 4])
  })

  it('never replaces an entry with an older copy', () => {
    const f = upsert(upsert([], entry(1, 200, { text: 'b' })), entry(1, 100, { text: 'a' }))
    expect(f[0].text).toBe('b')
  })

  it('goes by revision when both copies have one', () => {
    const f = upsert(upsert([], entry(1, 100, { rev: 3, text: 'b' })), entry(1, 900, { rev: 2, text: 'a' }))
    expect(f[0].text).toBe('b')
    expect(upsert(f, entry(1, 50, { rev: 4, text: 'c' }))[0].text).toBe('c')
  })

  it('leaves unchanged entries as they were, so they don’t re-render', () => {
    const a = entry(1)
    const d = run(initialData, ev({ type: 'entry', data: a }), ev({ type: 'entry', data: entry(2) }))
    expect(d.feed[0]).toBe(a)
  })

  it('patches a status', () => {
    const d = run(initialData, ev({ type: 'entry', data: entry(1, 100, { kind: 'ask', status: 'open' }) }), ev({ type: 'entryStatus', data: { id: 1, status: 'answered' } }))
    expect(d.feed[0].status).toBe('answered')
  })

  it('prepends earlier pages without duplicates', () => {
    const d = run(initialData, { type: 'snapshot', snap: snap([entry(5), entry(6)], { hasMore: true }) }, { type: 'earlier', page: { feed: [entry(3), entry(4), entry(5)], hasMore: false } })
    expect(d.feed.map((e) => e.id)).toEqual([3, 4, 5, 6])
    expect(d.hasMore).toBe(false)
  })

  it('clears the feed on reset', () => {
    const d = run(initialData, ev({ type: 'entry', data: entry(1) }), ev({ type: 'reset' }))
    expect(d.feed).toEqual([])
  })
})

describe('resync (FE-12)', () => {
  it('marks a resync so the connection fetches a snapshot', () => {
    expect(run(initialData, ev({ type: 'resync' })).sync.again).toBe(true)
    expect(run(initialData, ev({ type: 'resync' }), { type: 'syncStart' }).sync.again).toBe(false)
  })

  it('keeps older history the user loaded when the snapshot joins up with it', () => {
    const cur = [entry(1), entry(2), entry(3), entry(4)]
    const m = mergeFeed(cur, false, [entry(3), entry(4), entry(5)], true)
    expect(m.feed.map((e) => e.id)).toEqual([1, 2, 3, 4, 5])
    expect(m.hasMore).toBe(false)
  })

  it('replaces the feed when the snapshot doesn’t join up (too much happened while away)', () => {
    const m = mergeFeed([entry(1), entry(2)], false, [entry(10), entry(11)], true)
    expect(m.feed.map((e) => e.id)).toEqual([10, 11])
    expect(m.hasMore).toBe(true)
  })

  it('replaces the feed when the server no longer has what the client has', () => {
    const m = mergeFeed([entry(1), entry(2), entry(9)], false, [entry(1), entry(2)], true)
    expect(m.feed.map((e) => e.id)).toEqual([1, 2])
  })

  it('drops entries the snapshot covers but doesn’t have', () => {
    const m = mergeFeed([entry(1), entry(2), entry(3)], false, [entry(2), entry(4)], true)
    expect(m.feed.map((e) => e.id)).toEqual([1, 2, 4])
  })

  it('takes the whole snapshot when it holds the whole feed', () => {
    const m = mergeFeed([entry(1), entry(2)], false, [entry(2), entry(3)], false)
    expect(m.feed.map((e) => e.id)).toEqual([2, 3])
  })

  it('converges after a resync: the new snapshot replaces stale state', () => {
    let d = run(initialData, { type: 'snapshot', snap: snap([entry(1), entry(2, 100, { kind: 'ask', status: 'open' })], { state: session({ status: 'needs' }) }) })
    d = run(d, ev({ type: 'resync' }), { type: 'syncStart' }, { type: 'snapshot', snap: snap([entry(1), entry(2, 150, { kind: 'ask', status: 'answered' }), entry(3)], { state: session({ status: 'idle' }) }) })
    expect(d.feed.map((e) => [e.id, e.status])).toEqual([
      [1, undefined],
      [2, 'answered'],
      [3, undefined],
    ])
    expect(d.session.status).toBe('idle')
    expect(d.sync.again).toBe(false)
  })
})

describe('run events', () => {
  it('records the phase, when the run started and the headline across tools', () => {
    const d = run(
      initialData,
      ev({ type: 'run', seq: 1, data: { runId: 'r1', at: 1000, phase: 'started' } }),
      ev({ type: 'run', seq: 2, data: { runId: 'r1', at: 1500, phase: 'thinking', headline: 'Comparing prices' } }),
      ev({ type: 'run', seq: 3, data: { runId: 'r1', at: 2000, phase: 'tool', tool: { name: 'browser_click', label: 'Clicking Add to cart', kind: 'click', host: 'bestbuy.com' } } }),
    )
    expect(d.session.phase).toBe('tool')
    expect(d.session.runStartedAt).toBe(1000)
    expect(d.run?.headline).toBe('Comparing prices')
    expect(d.run?.tool?.host).toBe('bestbuy.com')
    expect(d.run?.seq).toBe(3)
  })

  it('starts afresh for a new run', () => {
    const d = run(
      initialData,
      ev({ type: 'run', data: { runId: 'r1', at: 1000, phase: 'thinking', headline: 'Old' } }),
      ev({ type: 'run', data: { runId: 'r2', at: 5000, phase: 'started' } }),
    )
    expect(d.run?.headline).toBeUndefined()
    expect(d.session.runStartedAt).toBe(5000)
  })

  it('counts server-side changes', () => {
    const d = run(initialData, ev({ type: 'changed', data: { what: 'memory' } }), ev({ type: 'changed', data: { what: 'memory' } }))
    expect(d.changed.memory).toBe(2)
  })

  it('counts a new or revised report as a change to the list of reports', () => {
    const r = { id: 'r1', title: 'Lisbon', format: 'markdown' as const, createdAt: 1, updatedAt: 1 }
    const d = run(initialData, ev({ type: 'report', data: r }), ev({ type: 'report', data: { ...r, updatedAt: 2 } }))
    expect(d.changed.reports).toBe(2)
    expect(d.reports.r1.updatedAt).toBe(2)
  })
})

describe('local actions', () => {
  it('sets the surface optimistically', () => {
    expect(run(initialData, { type: 'surface', surface: { kind: 'report', id: 'r1' } }).session.surface).toEqual({ kind: 'report', id: 'r1' })
  })

  it('marks notifications read', () => {
    const d = run(initialData, { type: 'notes', notes: [{ id: 1, title: 't', body: '', read: false, createdAt: 1 }] }, { type: 'notesRead' })
    expect(d.notes[0].read).toBe(true)
  })

  it('upserts a notification at the top', () => {
    const n = (id: number) => ({ id, title: `n${id}`, body: '', read: false, createdAt: id })
    const d = run(initialData, { type: 'notes', notes: [n(1), n(2)] }, ev({ type: 'notification', data: n(2) }))
    expect(d.notes.map((x) => x.id)).toEqual([2, 1])
  })
})
