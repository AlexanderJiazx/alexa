import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { Entry } from '../api/types'
import { blankSession } from '../state/reducer'
import { store } from '../state/store'
import { landed, observe, outbox, resend, retry, retryFor, sameMessage, setTransport, settled, submit, type Item } from './outbox'
import { voiced } from './spoken'

// The composer's outbox (WP-10): optimistic sends with a clientId, a
// failed send that goes again under the same id (so the server can't store
// it twice), and the queued and seen receipts.

const entry = (id: number, kind: string): Entry => ({ id, kind, text: '', createdAt: 0, updatedAt: 0 })
const queued = (more: Partial<Item> = {}): Item => ({ clientId: 'c1', msg: { text: 'hi' }, from: 'composer', state: 'queued', at: 0, entryId: 10, ...more })

describe('settled', () => {
  it('is queued when sent mid-run, sent otherwise', () => {
    const it0: Item = { clientId: 'c1', msg: { text: 'hi' }, from: 'composer', state: 'sending', at: 0 }
    expect(settled([it0], 'c1', entry(10, 'user'), true)[0]).toMatchObject({ state: 'queued', entryId: 10 })
    expect(settled([it0], 'c1', entry(10, 'user'), false)[0].state).toBe('sent')
  })
})

describe('observe', () => {
  it('marks a queued message seen once Alexa says something after it', () => {
    const items = [queued()]
    expect(observe(items, { feed: [entry(11, 'step')], status: 'working', now: 0 })).toBe(items)
    expect(observe(items, { feed: [entry(11, 'step'), entry(12, 'say')], status: 'working', now: 0 })[0].state).toBe('seen')
  })
  it('marks it seen when the session’s queue, having counted it, empties', () => {
    let items = observe([queued()], { feed: [], queued: 1, status: 'working', now: 0 })
    expect(items[0]).toMatchObject({ state: 'queued', counted: true })
    items = observe(items, { feed: [], queued: 0, status: 'working', now: 0 })
    expect(items[0].state).toBe('seen')
  })
  it('stops claiming queued a while after the run ended', () => {
    let items = observe([queued()], { feed: [], status: 'idle', now: 1000 })
    expect(items[0]).toMatchObject({ state: 'queued', idleSince: 1000 })
    items = observe(items, { feed: [], status: 'idle', now: 3000 })
    expect(items[0].state).toBe('queued')
    items = observe(items, { feed: [], status: 'idle', now: 5500 })
    expect(items[0].state).toBe('sent')
  })
})

describe('sameMessage', () => {
  it('compares text, attachments and whether it interrupts', () => {
    const file = { path: 'a.png', name: 'a.png', mime: 'image/png', size: 1 }
    expect(sameMessage({ text: 'hi' }, { text: 'hi' })).toBe(true)
    expect(sameMessage({ text: 'hi' }, { text: 'hi', interrupt: true })).toBe(false)
    expect(sameMessage({ text: 'hi', attachments: [file] }, { text: 'hi' })).toBe(false)
  })
})

describe('resend and landed', () => {
  const failed = (more: Partial<Item> = {}): Item => ({ clientId: 'c1', msg: { text: 'Yes' }, from: 'ask', state: 'failed', at: 1000, after: 10, ...more })
  const sent = (id: number, clientId: string): Entry => ({ ...entry(id, 'user'), text: 'Yes', data: { clientId } })

  it('reuses a failed message’s id for the same words a moment later', () => {
    expect(resend([failed()], { text: 'Yes' }, 'ask', [entry(10, 'ask')], 5000)?.clientId).toBe('c1')
  })
  it('makes a new message once Alexa has said something since', () => {
    expect(resend([failed()], { text: 'Yes' }, 'ask', [entry(10, 'ask'), entry(12, 'ask')], 5000)).toBeUndefined()
  })
  it('makes a new message once the failed one is old', () => {
    expect(resend([failed()], { text: 'Yes' }, 'ask', [entry(10, 'ask')], 1000 + retryFor)).toBeUndefined()
  })
  it('makes a new message when the failed one reached the server after all', () => {
    expect(resend([failed()], { text: 'Yes' }, 'ask', [entry(10, 'ask'), sent(11, 'c1')], 5000)).toBeUndefined()
  })
  it('settles a failed message whose entry shows up in the feed', () => {
    const items = [failed()]
    expect(landed(items, [entry(10, 'ask')])).toBe(items)
    expect(landed(items, [entry(10, 'ask'), sent(11, 'c1')])[0]).toMatchObject({ state: 'sent', entryId: 11, error: undefined })
  })
})

describe('submit and retry', () => {
  let restore = () => {}
  beforeEach(() => {
    outbox.set({ items: [] })
    store.set((s) => ({ ...s, data: { ...s.data, feed: [], session: { ...blankSession } } }))
    vi.stubGlobal('navigator', { onLine: true })
  })
  afterEach(() => {
    restore()
    vi.unstubAllGlobals()
  })

  it('keeps a failed message, then sends it again under the same clientId', async () => {
    const ids: string[] = []
    let fail = true
    restore = setTransport(async (m) => {
      ids.push(m.clientId)
      if (fail) throw new TypeError('Failed to fetch')
      return entry(20, 'user')
    })
    const first = await submit({ text: 'Book the 7:30 table' })
    expect(first.state).toBe('failed')
    expect(first.error).toBe('You’re offline.')
    fail = false
    const again = await retry(first.clientId)
    expect(again?.state).toBe('sent')
    expect(ids).toHaveLength(2)
    expect(ids[1]).toBe(ids[0])
    // Delivered once: the outbox holds one item, and the entry is in the feed once.
    expect(outbox.get().items).toHaveLength(1)
    expect(store.get().data.feed.filter((e) => e.id === 20)).toHaveLength(1)
  })

  it('reuses the clientId when the same failed message is sent again', async () => {
    const ids: string[] = []
    let fail = true
    restore = setTransport(async (m) => {
      ids.push(m.clientId)
      if (fail) throw new Error('Server error')
      return null
    })
    const a = await submit({ text: 'Make it 8' })
    expect(a).toMatchObject({ state: 'failed', error: 'Server error' })
    fail = false
    const b = await submit({ text: 'Make it 8' })
    expect(b.state).toBe('sent')
    expect(ids[1]).toBe(ids[0])
    // A different message gets its own id.
    await submit({ text: 'And a high chair' })
    expect(ids[2]).not.toBe(ids[0])
  })

  it('sends a new answer under a new id, even when the old one is still marked failed', async () => {
    const ids: string[] = []
    let fail = true
    restore = setTransport(async (m) => {
      ids.push(m.clientId)
      if (fail) throw new Error('Gateway timeout')
      return null
    })
    store.set((s) => ({ ...s, data: { ...s.data, feed: [entry(10, 'ask')] } }))
    expect((await submit({ text: 'Yes' }, 'ask')).state).toBe('failed')
    // The server had it after all, and Alexa has asked something new.
    store.set((s) => ({ ...s, data: { ...s.data, feed: [entry(10, 'ask'), { ...entry(11, 'user'), data: { clientId: ids[0] } }, entry(12, 'ask')] } }))
    fail = false
    await submit({ text: 'Yes' }, 'ask')
    expect(ids).toHaveLength(2)
    expect(ids[1]).not.toBe(ids[0])
  })

  it('says it’s queued when sent while Alexa works', async () => {
    store.set((s) => ({ ...s, data: { ...s.data, session: { ...blankSession, status: 'working' } } }))
    restore = setTransport(async () => entry(30, 'user'))
    expect((await submit({ text: 'Also check Target' })).state).toBe('queued')
  })

  // FH-9: replies are read aloud for a voice message's run only.
  it('reads replies aloud after a voice message, and stops for a typed one or a failed one', async () => {
    store.set((s) => ({ ...s, data: { ...s.data, feed: [entry(10, 'say')] } }))
    let fail = false
    restore = setTransport(async () => {
      if (fail) throw new TypeError('Failed to fetch')
      return null
    })
    await submit({ text: 'Book Via Carota for two', voice: true })
    expect(voiced).toMatchObject({ on: true, after: 10, ran: false })
    await submit({ text: 'Make it 8' })
    expect(voiced.on).toBe(false)
    fail = true
    await submit({ text: 'And a high chair', voice: true })
    expect(voiced.on).toBe(false)
  })

  it('counts a voice message sent mid-run as part of that run', async () => {
    store.set((s) => ({ ...s, data: { ...s.data, session: { ...blankSession, status: 'working' } } }))
    restore = setTransport(async () => null)
    await submit({ text: 'Also check Target', voice: true })
    expect(voiced).toMatchObject({ on: true, ran: true })
  })

  it('doesn’t retry what didn’t fail', async () => {
    restore = setTransport(async () => null)
    const a = await submit({ text: 'hi' })
    expect(await retry(a.clientId)).toBeUndefined()
  })
})
