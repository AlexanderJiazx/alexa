import { api, ApiError, newClientId } from '../api/client'
import type { Tap, Via } from '../api/blocks'
import type { Attachment, Entry, FormValues, SessionStatus } from '../api/types'
import { createStore, dispatch, store, useSelector } from '../state/store'
import { voiceFailed, voiceSent } from './spoken'

// The outbox is how anything the user sends reaches Alexa: the composer, an
// answer to an ask, a confirm. Every message goes with a clientId, so a
// retry after a dropped connection, or a double tap, is the same message to
// the server and never a second one (WP-18). It also keeps the receipts:
// sending, couldn't send, queued while Alexa works, then seen.

export type { Tap }

export type Outgoing = {
  text: string
  attachments?: Attachment[]
  voice?: boolean
  // via, tap and values say how the app made the message (Appendix A.4):
  // the server turns a form's values into the "[Form] …" line Alexa reads.
  via?: Via
  tap?: Tap
  values?: FormValues
  // askEntry is the skipped question a late answer answers (FE-1).
  askEntry?: number
  // interrupt stops the current step and sends this instead (WP-14).
  interrupt?: boolean
}

export type ItemState = 'sending' | 'failed' | 'queued' | 'seen' | 'sent'

export type Item = {
  clientId: string
  msg: Outgoing
  from: 'composer' | 'ask'
  state: ItemState
  at: number
  entryId?: number
  error?: string
  // slow is set once sending has taken long enough to say so.
  slow?: boolean
  // counted is set once the session's queued count has included it.
  counted?: boolean
  // idleSince is when the run stopped while this was still queued.
  idleSince?: number
  // after is the last entry in the feed when it was sent: whatever Alexa
  // says after that is a new turn, and a new answer is a new message.
  after?: number
}

export type Outbox = { items: Item[] }

export const outbox = createStore<Outbox>({ items: [] })

export function useOutbox<T>(select: (o: Outbox) => T, eq?: (a: T, b: T) => boolean): T {
  return useSelector(outbox, select, eq)
}

// ---------- pure steps (outbox.test.ts) ----------

const put = (items: Item[], it: Item) => [...items.filter((x) => x.clientId !== it.clientId), it].slice(-6)
const patch = (items: Item[], id: string, p: Partial<Item>) => items.map((x) => (x.clientId === id ? { ...x, ...p } : x))

// settled records a delivered message. Sent while Alexa works, it waits in
// her inbox until the current step ends: that's a receipt worth showing.
export function settled(items: Item[], id: string, entry: Entry | null, working: boolean): Item[] {
  return patch(items, id, { state: working ? 'queued' : 'sent', entryId: entry?.id, error: undefined, slow: false })
}

export const failedAt = (items: Item[], id: string, error: string): Item[] => patch(items, id, { state: 'failed', error, slow: false })

// Alexa has read a queued message once she says, asks or reports something
// after it (steps don't count: the turn that was running when it arrived
// keeps making them), or once the session's queued count, having counted
// it, goes back to zero.
const reads = new Set(['say', 'ask', 'report', 'takeover', 'login', 'error'])

export type Seen = { feed: Entry[]; queued?: number; status: SessionStatus; now: number }

export function observe(items: Item[], s: Seen): Item[] {
  let changed = false
  const out = items.map((it) => {
    if (it.state !== 'queued' || it.entryId === undefined) return it
    const id = it.entryId
    let next = it
    if (typeof s.queued === 'number' && s.queued > 0 && !it.counted) next = { ...next, counted: true }
    const read = s.feed.some((e) => e.id > id && reads.has(e.kind)) || (next.counted && s.queued === 0)
    if (read) next = { ...next, state: 'seen' }
    else if (s.status !== 'working') {
      // The run ended with this still waiting: the server starts a new run
      // for it. If none starts, stop claiming it's queued.
      const since = next.idleSince ?? s.now
      next = s.now - since > 4000 ? { ...next, state: 'sent' } : { ...next, idleSince: since }
    } else if (next.idleSince) next = { ...next, idleSince: undefined }
    if (next !== it) changed = true
    return next
  })
  return changed ? out : items
}

// sameMessage says whether a send repeats a message that failed, so it
// goes again under the same clientId.
export const sameMessage = (a: Outgoing, b: Outgoing) =>
  a.text === b.text && !!a.interrupt === !!b.interrupt && (a.attachments ?? []).map((x) => x.path).join('\n') === (b.attachments ?? []).map((x) => x.path).join('\n')

// landed finds a failed message that reached the server after all (its
// answer was lost on the way back): the feed has an entry sent with its
// clientId. It's settled, not failed, and its id is never sent again.
export function landed(items: Item[], feed: Entry[]): Item[] {
  if (!items.some((x) => x.state === 'failed')) return items
  let changed = false
  const out = items.map((it) => {
    if (it.state !== 'failed') return it
    const e = sentWith(feed, it.clientId)
    if (!e) return it
    changed = true
    return { ...it, state: 'sent' as const, entryId: e.id, error: undefined }
  })
  return changed ? out : items
}

const sentWith = (feed: Entry[], clientId: string) => {
  for (let i = feed.length - 1; i >= 0; i--) if (feed[i].kind === 'user' && feed[i].data?.clientId === clientId) return feed[i]
  return undefined
}

// How long typing the same text again counts as retrying a failed send.
export const retryFor = 2 * 60e3

// resend finds the failed message a new send repeats, whose clientId it
// can go under: the same words from the same place, failed a moment ago,
// with nothing from Alexa since. Anything else is a new message (a "Yes"
// to a new question is a new answer), and the server must not take it for
// the old one.
export function resend(items: Item[], msg: Outgoing, from: Item['from'], feed: Entry[], now: number): Item | undefined {
  return items.find(
    (x) =>
      x.state === 'failed' &&
      x.from === from &&
      sameMessage(x.msg, msg) &&
      now - x.at < retryFor &&
      !sentWith(feed, x.clientId) &&
      !feed.some((e) => e.id > (x.after ?? 0) && reads.has(e.kind)),
  )
}

// ---------- the wire ----------

export type Transport = (m: Outgoing & { clientId: string }) => Promise<Entry | null>

let transport: Transport = (m) => {
  if (m.interrupt) return api.interrupt(m.text, m.clientId, m.attachments ?? [], !!m.voice)
  return api.send(m.text, m.attachments ?? [], !!m.voice, m.clientId, { via: m.via, tap: m.tap, values: m.values, askEntry: m.askEntry })
}

// setTransport swaps the wire, for tests.
export function setTransport(t: Transport) {
  const prev = transport
  transport = t
  return () => {
    transport = prev
  }
}

// ---------- sending ----------

const slowAfter = 600
const seenFor = 2500

function why(e: unknown) {
  if (e instanceof ApiError && e.status === 404 && (e.message === 'Not Found' || /not found/i.test(e.message))) return 'This server can’t do that yet.'
  if (e instanceof TypeError || !navigator.onLine) return 'You’re offline.'
  return (e as Error)?.message || 'Something went wrong.'
}

async function deliver(id: string) {
  const it = outbox.get().items.find((x) => x.clientId === id)
  if (!it) return
  const slow = setTimeout(() => outbox.set((o) => ({ items: patch(o.items, id, { slow: true }) })), slowAfter)
  // What Alexa says in reply to a voice message is read aloud; a typed one
  // turns that off.
  const f = store.get().data.feed
  const working = store.get().data.session.status === 'working'
  voiceSent(!!it.msg.voice, f.length ? f[f.length - 1].id : 0, working)
  try {
    const entry = await transport({ ...it.msg, clientId: id })
    // A server that answers with the stored entry shows it at once; the
    // socket's echo is the same entry, so it lands in the same place.
    if (entry) dispatch({ type: 'event', ev: { type: 'entry', data: entry } })
    outbox.set((o) => ({ items: settled(o.items, id, entry, working || !!it.msg.interrupt) }))
  } catch (e) {
    voiceFailed()
    outbox.set((o) => ({ items: failedAt(o.items, id, why(e)) }))
  } finally {
    clearTimeout(slow)
  }
}

// submit sends a message and resolves with how it went: the composer puts a
// failed message's text back.
export async function submit(msg: Outgoing, from: Item['from'] = 'composer'): Promise<Item> {
  const feed = store.get().data.feed
  const again = resend(outbox.get().items, msg, from, feed, Date.now())
  const id = again?.clientId ?? newClientId()
  const it: Item = { clientId: id, msg, from, state: 'sending', at: Date.now(), after: again?.after ?? (feed.length ? feed[feed.length - 1].id : 0) }
  outbox.set((o) => ({ items: put(o.items, it) }))
  await deliver(id)
  return outbox.get().items.find((x) => x.clientId === id) ?? it
}

export async function retry(id: string): Promise<Item | undefined> {
  if (!outbox.get().items.some((x) => x.clientId === id && x.state === 'failed')) return
  outbox.set((o) => ({ items: patch(o.items, id, { state: 'sending', error: undefined }) }))
  await deliver(id)
  return outbox.get().items.find((x) => x.clientId === id)
}

export const dismiss = (id: string) => outbox.set((o) => ({ items: o.items.filter((x) => x.clientId !== id) }))

// watch moves queued receipts to seen as the session moves, and lets a seen
// receipt go after a moment. App code calls it once; it returns a stop.
export function watch() {
  const timers = new Map<string, ReturnType<typeof setTimeout>>()
  let last = store.get().data
  const tick = () => {
    const d = store.get().data
    const before = outbox.get().items
    if (!before.some((x) => x.state === 'queued' || x.state === 'seen' || x.state === 'failed')) return
    const after = observe(landed(before, d.feed), { feed: d.feed, queued: d.session.queued, status: d.session.status, now: Date.now() })
    if (after !== before) outbox.set({ items: after })
    for (const it of after) {
      if (it.state !== 'seen' || timers.has(it.clientId)) continue
      timers.set(
        it.clientId,
        setTimeout(() => {
          timers.delete(it.clientId)
          dismiss(it.clientId)
        }, seenFor),
      )
    }
  }
  const unStore = store.subscribe(() => {
    const d = store.get().data
    if (d === last) return
    last = d
    tick()
  })
  // The idle fallback needs time to pass without events.
  const every = setInterval(tick, 1000)
  return () => {
    unStore()
    clearInterval(every)
    timers.forEach(clearTimeout)
  }
}
