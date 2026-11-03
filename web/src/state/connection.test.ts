import { describe, expect, it } from 'vitest'
import type { ServerEvent } from '../api/realtime'
import type { SessionData } from '../api/types'
import { attentionEvery, backoff, createConnection, isGap, type ConnectionDeps, type Env, type SocketLike } from './connection'
import { blankSession, initialData, reduce, type Action, type Data } from './reducer'

// A socket the test drives by hand.
class FakeSocket implements SocketLike {
  readyState = 0
  sent: unknown[] = []
  onopen: SocketLike['onopen'] = null
  onmessage: SocketLike['onmessage'] = null
  onclose: SocketLike['onclose'] = null
  constructor(public path: string) {}
  send(data: string) {
    this.sent.push(JSON.parse(data))
  }
  // close starts closing; the close event comes later, as in a browser.
  close() {
    if (this.readyState < 2) this.readyState = 2
  }
  open() {
    this.readyState = 1
    this.onopen?.({})
  }
  closed(code = 1006) {
    this.readyState = 3
    this.onclose?.({ code })
  }
  message(ev: ServerEvent) {
    this.onmessage?.({ data: JSON.stringify(ev) })
  }
}

function harness(opts: { snapshot?: () => Promise<SessionData> } = {}) {
  const sockets: FakeSocket[] = []
  const timers: { fn: () => void; ms: number; id: number }[] = []
  let nextTimer = 0
  const listeners = new Map<string, () => void>()
  let visible = true
  let data: Data = initialData
  const statuses: string[] = []
  let snapshots = 0
  let unauthorized = 0
  const env: Env = {
    visible: () => visible,
    focused: () => visible,
    online: () => true,
    setTimeout: (fn, ms) => {
      const id = ++nextTimer
      timers.push({ fn, ms, id })
      return id
    },
    clearTimeout: (t) => {
      const i = timers.findIndex((x) => x.id === t)
      if (i >= 0) timers.splice(i, 1)
    },
    listen: (target, type, fn) => {
      listeners.set(`${target}:${type}`, fn)
      return () => listeners.delete(`${target}:${type}`)
    },
  }
  const deps: ConnectionDeps = {
    open: (path) => {
      const s = new FakeSocket(path)
      sockets.push(s)
      return s
    },
    snapshot: async () => {
      snapshots++
      return { snap: opts.snapshot ? await opts.snapshot() : { state: blankSession, feed: [], hasMore: false } }
    },
    dispatch: (a: Action) => {
      data = reduce(data, a)
    },
    sync: () => data.sync,
    browser: () => {},
    status: (s) => statuses.push(s),
    unauthorized: () => unauthorized++,
    env,
    helloWait: 1500,
  }
  const c = createConnection(deps)
  return {
    c,
    sockets,
    timers,
    statuses,
    get data() {
      return data
    },
    get snapshots() {
      return snapshots
    },
    get unauthorized() {
      return unauthorized
    },
    last: () => sockets[sockets.length - 1],
    // live counts sockets that aren't closing or closed.
    live: () => sockets.filter((s) => s.readyState < 2).length,
    setVisible(v: boolean) {
      visible = v
      listeners.get('document:visibilitychange')?.()
    },
    fire(ms?: number) {
      const t = ms === undefined ? timers.shift() : timers.splice(timers.findIndex((x) => x.ms === ms), 1)[0]
      t?.fn()
    },
  }
}

const flush = () => new Promise((r) => setTimeout(r, 0))

describe('connection generations (FE-02)', () => {
  it('keeps exactly one socket open through rapid visibility toggles', () => {
    const h = harness()
    h.c.start()
    h.last().open()
    for (let i = 0; i < 5; i++) {
      // The phone sleeps: the socket starts closing but hasn't closed yet.
      h.last().close()
      h.setVisible(false)
      h.setVisible(true)
    }
    expect(h.live()).toBe(1)
    // The stale sockets' late closes don't schedule more reconnects.
    const before = h.sockets.length
    h.sockets.slice(0, -1).forEach((s) => s.closed())
    expect(h.timers.filter((t) => t.ms !== 1500)).toEqual([])
    h.timers.splice(0).forEach((t) => t.fn())
    expect(h.sockets.length).toBe(before)
    expect(h.live()).toBe(1)
  })

  it('ignores messages from a stale socket', async () => {
    const h = harness()
    h.c.start()
    const old = h.last()
    old.open()
    old.message({ type: 'hello' })
    await flush()
    old.close()
    h.setVisible(true)
    old.message({ type: 'entry', data: { id: 1, kind: 'say', text: 'late', createdAt: 1, updatedAt: 1 } })
    expect(h.data.feed).toEqual([])
  })

  it('reconnects with backoff after a drop, and says reconnecting', () => {
    const h = harness()
    h.c.start()
    h.last().open()
    h.last().message({ type: 'hello' })
    h.last().closed()
    expect(h.statuses.at(-1)).toBe('reconnecting')
    const t = h.timers.find((x) => x.ms === backoff(1))
    expect(t).toBeTruthy()
    h.fire(backoff(1))
    expect(h.sockets).toHaveLength(2)
  })

  it('goes back to sign-in on close code 4401 and doesn’t reconnect', () => {
    const h = harness()
    h.c.start()
    h.last().open()
    h.last().closed(4401)
    expect(h.unauthorized).toBe(1)
    expect(h.timers).toEqual([])
  })

  it('reconnects at once, with its cursor, after close code 4000 (fell behind)', () => {
    const h = harness()
    h.c.start()
    h.last().open()
    h.last().message({ type: 'hello', seq: 4, epoch: 'e1' })
    h.last().closed(4000)
    expect(h.timers.find((x) => x.ms === backoff(1))).toBeUndefined()
    h.fire(0)
    expect(h.sockets).toHaveLength(2)
    expect(h.last().path).toContain('after=e1%3A4')
  })

  it('stops for good', () => {
    const h = harness()
    h.c.start()
    h.last().open()
    h.c.stop()
    h.last().closed()
    expect(h.timers).toEqual([])
    expect(h.live()).toBe(0)
  })
})

describe('sync on hello (FE-01)', () => {
  it('fetches a snapshot on the first hello and says live', async () => {
    const h = harness()
    h.c.start()
    expect(h.statuses[0]).toBe('connecting')
    h.last().open()
    expect(h.snapshots).toBe(0)
    h.last().message({ type: 'hello' })
    await flush()
    expect(h.snapshots).toBe(1)
    expect(h.statuses).toContain('live')
    expect(h.data.loaded).toBe(true)
  })

  it('syncs even when a server says no hello', async () => {
    const h = harness()
    h.c.start()
    h.last().open()
    h.fire(1500)
    await flush()
    expect(h.snapshots).toBe(1)
  })

  it('buffers events while the snapshot loads and keeps the newer ones', async () => {
    let release: (s: SessionData) => void = () => {}
    const h = harness({ snapshot: () => new Promise((r) => (release = r)) })
    h.c.start()
    h.last().open()
    h.last().message({ type: 'hello' })
    h.last().message({ type: 'entry', data: { id: 2, kind: 'say', text: 'live', createdAt: 5, updatedAt: 5 } })
    expect(h.data.sync.pending).toBe(true)
    release({ state: blankSession, feed: [{ id: 1, kind: 'user', text: 'hi', createdAt: 1, updatedAt: 1 }], hasMore: false })
    await flush()
    expect(h.data.feed.map((e) => e.id)).toEqual([1, 2])
  })

  it('tells the server whether the user is looking, in both protocols', () => {
    const h = harness()
    h.c.start()
    h.last().open()
    expect(h.last().sent).toEqual([
      { type: 'attention', visible: true, focused: true },
      { type: 'visibility', visible: true },
    ])
    h.setVisible(false)
    expect(h.last().sent.slice(-1)).toEqual([{ type: 'visibility', visible: false }])
  })

  it('repeats attention while the socket is open, as the server forgets it after two minutes', () => {
    const h = harness()
    h.c.start()
    h.last().open()
    const sent = h.last().sent.length
    h.fire(attentionEvery)
    h.fire(attentionEvery)
    expect(h.last().sent.slice(sent)).toEqual([
      { type: 'attention', visible: true, focused: true },
      { type: 'attention', visible: true, focused: true },
    ])
    h.last().closed()
    expect(h.timers.some((t) => t.ms === attentionEvery)).toBe(false)
  })
})

describe('event protocol v2 (WP-18)', () => {
  it('reconnects with ?after=epoch:seq and lets the server replay', async () => {
    const h = harness()
    h.c.start()
    h.last().open()
    h.last().message({ type: 'hello', data: { epoch: 'e1', seq: 4 } })
    await flush()
    h.last().message({ type: 'state', seq: 5, epoch: 'e1', data: blankSession })
    h.last().closed()
    h.fire(backoff(1))
    expect(h.last().path).toBe(`/api/ws?after=${encodeURIComponent('e1:5')}`)
    h.last().open()
    h.last().message({ type: 'hello', data: { epoch: 'e1', seq: 9 } })
    await flush()
    expect(h.snapshots).toBe(1)
  })

  it('takes a snapshot after a server restart', async () => {
    const h = harness()
    h.c.start()
    h.last().open()
    h.last().message({ type: 'hello', data: { epoch: 'e1', seq: 4 } })
    await flush()
    h.last().closed()
    h.fire(backoff(1))
    h.last().open()
    h.last().message({ type: 'hello', data: { epoch: 'e2', seq: 0 } })
    // The server follows a restart's hello with resync; one snapshot does.
    h.last().message({ type: 'resync', epoch: 'e2' })
    await flush()
    await flush()
    expect(h.snapshots).toBe(2)
  })

  it('resyncs on a gap in seq, and when the server says so', async () => {
    const h = harness()
    h.c.start()
    h.last().open()
    h.last().message({ type: 'hello', data: { epoch: 'e1', seq: 4 } })
    await flush()
    h.last().message({ type: 'state', seq: 5, epoch: 'e1', data: blankSession })
    await flush()
    expect(h.snapshots).toBe(1)
    h.last().message({ type: 'state', seq: 8, epoch: 'e1', data: blankSession })
    await flush()
    expect(h.snapshots).toBe(2)
    h.last().message({ type: 'resync' })
    await flush()
    expect(h.snapshots).toBe(3)
  })

  it('tries a failed first snapshot again while the socket is open', async () => {
    let fail = true
    const h = harness({
      snapshot: async () => {
        if (fail) throw new Error('502')
        return { state: blankSession, feed: [], hasMore: false }
      },
    })
    h.c.start()
    h.last().open()
    h.last().message({ type: 'hello', data: { epoch: 'e1', seq: 1 } })
    await flush()
    expect(h.snapshots).toBe(1)
    expect(h.data.loaded).toBe(false)
    h.fire(backoff(1))
    await flush()
    expect(h.snapshots).toBe(2)
    expect(h.data.loaded).toBe(false)
    fail = false
    h.fire(backoff(2))
    await flush()
    expect(h.snapshots).toBe(3)
    expect(h.data.loaded).toBe(true)
    // Once it's in, nothing more is scheduled.
    expect(h.timers.some((t) => t.ms === backoff(3))).toBe(false)
  })

  it('fills a gap that a failed snapshot left, without waiting for another gap', async () => {
    const e = (id: number) => ({ id, kind: 'say', text: `m${id}`, createdAt: id, updatedAt: id })
    let fail = false
    let server: ReturnType<typeof e>[] = []
    const h = harness({
      snapshot: async () => {
        if (fail) throw new Error('503')
        return { state: blankSession, feed: server, hasMore: false, epoch: 'e1', seq: server.length }
      },
    })
    h.c.start()
    h.last().open()
    h.last().message({ type: 'hello', data: { epoch: 'e1', seq: 0 } })
    await flush()
    server = [e(1), e(2), e(3)]
    h.last().message({ type: 'entry', seq: 1, epoch: 'e1', data: e(1) })
    fail = true
    // seq 2 never arrives: the gap asks for a snapshot, which fails.
    h.last().message({ type: 'entry', seq: 3, epoch: 'e1', data: e(3) })
    await flush()
    expect(h.data.feed.map((x) => x.id)).toEqual([1, 3])
    fail = false
    h.fire(backoff(1))
    await flush()
    expect(h.data.feed.map((x) => x.id)).toEqual([1, 2, 3])
  })

  it('takes a snapshot on reconnecting when the last one failed, even where the server could replay', async () => {
    let fail = false
    const h = harness({
      snapshot: async () => {
        if (fail) throw new Error('503')
        return { state: blankSession, feed: [], hasMore: false }
      },
    })
    h.c.start()
    h.last().open()
    h.last().message({ type: 'hello', data: { epoch: 'e1', seq: 4 } })
    await flush()
    fail = true
    h.last().message({ type: 'state', seq: 6, epoch: 'e1', data: blankSession })
    await flush()
    expect(h.snapshots).toBe(2)
    h.last().closed()
    fail = false
    h.fire(backoff(1))
    h.last().open()
    h.last().message({ type: 'hello', data: { epoch: 'e1', seq: 9 } })
    await flush()
    expect(h.snapshots).toBe(3)
  })

  it('checks the gap helper and the backoff', () => {
    expect(isGap(undefined, 9)).toBe(false)
    expect(isGap(4, 5)).toBe(false)
    expect(isGap(4, 6)).toBe(true)
    expect([1, 2, 3, 6, 9].map(backoff)).toEqual([1000, 2000, 4000, 15000, 15000])
  })
})
