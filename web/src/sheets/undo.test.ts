import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { actionMs, createToasts, exitMs, maxNotes } from './toasts'
import { createUndo, keepingAlive, undoMs } from './undo'

beforeEach(() => {
  vi.useFakeTimers()
})
afterEach(() => {
  vi.useRealTimers()
})

// flush lets the commit's promise settle under fake timers.
const settle = async () => {
  await Promise.resolve()
  await Promise.resolve()
}

describe('toasts', () => {
  it('times a note out, then removes it after the exit', () => {
    const t = createToasts()
    const end = vi.fn()
    t.notify({ title: 'Saved', tone: 'ok', ms: 1000, onEnd: end })
    expect(t.store.get()).toHaveLength(1)
    vi.advanceTimersByTime(999)
    expect(t.store.get()[0].leaving).toBeFalsy()
    vi.advanceTimersByTime(1)
    expect(t.store.get()[0].leaving).toBe(true)
    expect(end).toHaveBeenCalledWith('timeout')
    vi.advanceTimersByTime(exitMs)
    expect(t.store.get()).toHaveLength(0)
  })

  it('pauses while hovered and keeps the time that was left', () => {
    const t = createToasts()
    t.notify({ title: 'Hello', ms: 1000 })
    vi.advanceTimersByTime(600)
    t.pause()
    vi.advanceTimersByTime(5000)
    expect(t.store.get()[0].leaving).toBeFalsy()
    t.resume()
    vi.advanceTimersByTime(799)
    expect(t.store.get()[0].leaving).toBeFalsy()
    vi.advanceTimersByTime(1)
    expect(t.store.get()[0].leaving).toBe(true)
  })

  it('starts afresh once the hovered stack has emptied', () => {
    const t = createToasts()
    const a = t.notify({ title: 'Forgotten', ms: 1000 })
    t.pause()
    // The note goes from under the pointer, which never "leaves" it.
    t.dismiss(a)
    t.notify({ title: 'Next', ms: 1000 })
    vi.advanceTimersByTime(1000)
    expect(t.store.get().find((n) => n.title === 'Next')?.leaving).toBe(true)
  })

  it('runs an action once and says so', () => {
    const t = createToasts()
    const run = vi.fn()
    const end = vi.fn()
    const id = t.notify({ title: 'Deleted', action: { label: 'Undo', run }, onEnd: end })
    t.act(id)
    t.act(id)
    expect(run).toHaveBeenCalledTimes(1)
    expect(end).toHaveBeenCalledTimes(1)
    expect(end).toHaveBeenCalledWith('action')
  })

  it('keeps a note with an action long enough to reach it', () => {
    const t = createToasts()
    const end = vi.fn()
    t.notify({ title: 'Forgotten', ms: 3000, action: { label: 'Undo', run: () => {} }, onEnd: end })
    vi.advanceTimersByTime(actionMs - 1)
    expect(end).not.toHaveBeenCalled()
    vi.advanceTimersByTime(1)
    expect(end).toHaveBeenCalledWith('timeout')
  })

  it('keeps at most three on screen, pushing out the oldest', () => {
    const t = createToasts()
    const first = vi.fn()
    t.notify({ title: '1', onEnd: first })
    for (let i = 2; i <= maxNotes + 1; i++) t.notify({ title: String(i) })
    expect(first).toHaveBeenCalledWith('timeout')
    expect(t.store.get().filter((n) => !n.leaving).map((n) => n.title)).toEqual(['2', '3', '4'])
  })

  it('folds the same plain note into the one showing and restarts it', () => {
    const t = createToasts()
    const a = t.notify({ title: 'Can’t reach the server', body: 'Bad Gateway', tone: 'bad', ms: 1000 })
    vi.advanceTimersByTime(900)
    const b = t.notify({ title: 'Can’t reach the server', body: 'Bad Gateway', tone: 'bad', ms: 1000 })
    expect(b).toBe(a)
    expect(t.store.get()).toHaveLength(1)
    vi.advanceTimersByTime(900)
    expect(t.store.get()[0].leaving).toBeFalsy()
    // A note with an action is its own thing, even with the same words.
    t.notify({ title: 'Can’t reach the server', body: 'Bad Gateway', tone: 'bad', action: { label: 'Retry', run: () => {} } })
    expect(t.store.get()).toHaveLength(2)
  })

  it('gives errors longer than good news', () => {
    const t = createToasts()
    t.notify({ title: 'ok', tone: 'ok' })
    t.notify({ title: 'bad', tone: 'bad' })
    vi.advanceTimersByTime(5000)
    expect(t.store.get().find((n) => n.title === 'ok')?.leaving).toBe(true)
    expect(t.store.get().find((n) => n.title === 'bad')?.leaving).toBeFalsy()
  })
})

describe('undo', () => {
  it('hides at once and deletes only when the Undo has gone', async () => {
    const t = createToasts()
    const u = createUndo(t)
    const commit = vi.fn(() => Promise.resolve())
    const done = vi.fn()
    u.remove({ key: 'memory:1', title: 'Forgotten', commit, done })
    expect(u.pending.get().has('memory:1')).toBe(true)
    expect(t.store.get()[0].action?.label).toBe('Undo')
    vi.advanceTimersByTime(undoMs - 1)
    expect(commit).not.toHaveBeenCalled()
    vi.advanceTimersByTime(1)
    expect(commit).toHaveBeenCalledTimes(1)
    await settle()
    expect(done).toHaveBeenCalled()
    expect(u.pending.get().has('memory:1')).toBe(false)
  })

  it('puts it back on Undo and never deletes it', async () => {
    const t = createToasts()
    const u = createUndo(t)
    const commit = vi.fn(() => Promise.resolve())
    u.remove({ key: 'memory:2', title: 'Forgotten', commit })
    t.act(t.store.get()[0].id)
    expect(u.pending.get().has('memory:2')).toBe(false)
    vi.advanceTimersByTime(undoMs * 2)
    await settle()
    expect(commit).not.toHaveBeenCalled()
  })

  it('deletes at once when the note is dismissed', () => {
    const t = createToasts()
    const u = createUndo(t)
    const commit = vi.fn(() => Promise.resolve())
    u.remove({ key: 'file:a.txt', title: 'Deleted', commit })
    t.dismiss(t.store.get()[0].id)
    expect(commit).toHaveBeenCalledTimes(1)
  })

  it('brings the item back when the delete fails', async () => {
    const t = createToasts()
    const u = createUndo(t)
    const failed = vi.fn()
    u.remove({ key: 'schedule:x', title: 'Deleted', commit: () => Promise.reject(new Error('offline')), failed })
    vi.advanceTimersByTime(undoMs)
    await settle()
    expect(u.pending.get().has('schedule:x')).toBe(false)
    expect(failed).toHaveBeenCalledWith(expect.objectContaining({ message: 'offline' }))
  })

  it('flushes everything waiting, once each', async () => {
    const t = createToasts()
    const u = createUndo(t)
    const a = vi.fn(() => Promise.resolve())
    const b = vi.fn(() => Promise.resolve())
    u.remove({ key: 'memory:1', title: 'A', commit: a })
    u.remove({ key: 'memory:2', title: 'B', commit: b })
    u.flush()
    vi.advanceTimersByTime(undoMs * 2)
    await settle()
    expect(a).toHaveBeenCalledTimes(1)
    expect(b).toHaveBeenCalledTimes(1)
  })

  // FE-2: Sign out waits for what's still waiting before the session goes.
  it('settles a flush only once every delete has', async () => {
    vi.useRealTimers()
    const t = createToasts()
    const u = createUndo(t)
    let finish = () => {}
    const commit = vi.fn(() => new Promise<void>((r) => (finish = r)))
    const done = vi.fn()
    u.remove({ key: 'memory:4', title: 'A', commit, done })
    let flushed = false
    const p = u.flush().then(() => (flushed = true))
    await Promise.resolve()
    expect(commit).toHaveBeenCalledTimes(1)
    expect(flushed).toBe(false)
    finish()
    await p
    expect(done).toHaveBeenCalled()
    expect(u.pending.get().has('memory:4')).toBe(false)
    await expect(u.flush()).resolves.toBeUndefined()
  })

  it('ignores a second delete of the same thing while it waits', () => {
    const t = createToasts()
    const u = createUndo(t)
    const commit = vi.fn(() => Promise.resolve())
    u.remove({ key: 'memory:3', title: 'A', commit })
    u.remove({ key: 'memory:3', title: 'A', commit })
    expect(t.store.get()).toHaveLength(1)
  })
})

// FE-2: the deletes a closing page flushes must outlive it.
describe('keepingAlive', () => {
  it('marks the requests it starts keepalive, and puts fetch back', async () => {
    const g = globalThis as { fetch: typeof fetch }
    const plain = g.fetch
    const seen: (RequestInit | undefined)[] = []
    const stub = vi.fn((_: RequestInfo | URL, init?: RequestInit) => {
      seen.push(init)
      return Promise.resolve(new Response(null, { status: 204 }))
    })
    g.fetch = stub as unknown as typeof fetch
    try {
      const t = createToasts()
      const u = createUndo(t)
      u.remove({ key: 'memory:5', title: 'A', commit: () => fetch('/api/memory/5', { method: 'DELETE', headers: { 'X-Alexa': '1' } }) })
      const out = keepingAlive(() => u.flush())
      expect(g.fetch).toBe(stub)
      await out
      expect(seen).toEqual([{ method: 'DELETE', headers: { 'X-Alexa': '1' }, keepalive: true }])
      // Afterwards fetch is as it was.
      await fetch('/api/me')
      expect(seen[1]).toBeUndefined()
    } finally {
      g.fetch = plain
    }
  })

  it('puts fetch back when what it runs throws', () => {
    const g = globalThis as { fetch: typeof fetch }
    const plain = g.fetch
    expect(() =>
      keepingAlive(() => {
        throw new Error('boom')
      }),
    ).toThrow('boom')
    expect(g.fetch).toBe(plain)
  })
})
