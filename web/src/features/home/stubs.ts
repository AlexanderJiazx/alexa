import type { Entry, SessionState } from '../../api/types'
import { frames } from '../../panels/browser/fixtures'
import { blankSession } from '../../state/reducer'
import { dispatch, store } from '../../state/store'
import * as f from './fixtures'

// The stage gallery's stand-ins for the server (development only): /api
// answers from the fixtures, a message gets a short scripted run back, and
// the live browser plays a recorded frame. They're installed by the gallery
// page when it renders, inside its own frame, never on import.

// fresh says the scenario is someone new, with nothing saved yet.
type Opts = { fresh: () => boolean }

let installed = false

export function installStubs(o: Opts) {
  if (installed) return
  installed = true
  const real = window.fetch.bind(window)
  window.fetch = async (input, init) => {
    const url = new URL(typeof input === 'string' ? input : input instanceof URL ? input.href : input.url, location.href)
    if (!url.pathname.startsWith('/api/')) return real(input, init)
    const method = (init?.method ?? 'GET').toUpperCase()
    const body = typeof init?.body === 'string' ? JSON.parse(init.body) : null
    return json(answer(method, url.pathname, body, o))
  }
  window.WebSocket = FakeSocket as unknown as typeof WebSocket
}

const json = (v: unknown) => new Response(JSON.stringify(v ?? {}), { status: 200, headers: { 'Content-Type': 'application/json' } })

function answer(method: string, path: string, body: { text?: string; clientId?: string } | null, o: Opts): unknown {
  if (method === 'GET') {
    if (path === '/api/schedules') return o.fresh() ? [] : f.schedules
    if (path === '/api/reports') return o.fresh() ? [] : f.reports
    if (path.startsWith('/api/reports/')) return f.reports.find((r) => r.id === decodeURIComponent(path.slice(13)))
    if (path === '/api/logins') return o.fresh() ? [] : f.logins
    if (path === '/api/notifications' || path === '/api/memories' || path === '/api/files') return []
    return {}
  }
  if (method === 'POST' && (path === '/api/messages' || path === '/api/interrupt')) return { ok: true, entry: reply(body?.text ?? '', body?.clientId) }
  return { ok: true }
}

let timers: ReturnType<typeof setTimeout>[] = []

// stopScript forgets a scripted run, when the gallery switches scenario.
export function stopScript() {
  timers.forEach(clearTimeout)
  timers = []
}

// later runs fn after ms, unless the scenario changes first.
export const later = (ms: number, fn: () => void) => {
  timers.push(setTimeout(fn, ms))
}
const put = (e: Entry) => dispatch({ type: 'event', ev: { type: 'entry', data: e } })
const state = (s: Partial<SessionState>) => dispatch({ type: 'session', session: { ...store.get().data.session, ...s } })

// reply stores the user's message and plays a short run in answer: two
// steps, then a say with follow-ups. A message sent while Alexa works
// joins the run.
function reply(text: string, clientId?: string): Entry {
  const status = store.get().data.session.status
  const idle = status === 'idle'
  const user = at(f.entry('user', text, { data: { start: idle || undefined, clientId } }))
  // An answer to a question carries the run on; anything else while Alexa
  // works joins it.
  if (status === 'working') return user
  if (!idle) later(50, () => state({ status: 'working', needs: null }))
  const search = f.entry('step', 'Searching the web', { status: 'running' })
  const read = f.entry('step', 'Reading rtings.com', { status: 'running' })
  later(150, () => state({ status: 'working', activity: 'Searching the web', runStartedAt: Date.now() }))
  later(700, () => put(at(search)))
  later(1900, () => {
    put(done(search, `Searched the web for “${text.slice(0, 40)}”`))
    put(at(read))
    state({ activity: 'Reading rtings.com' })
  })
  later(3300, () => {
    put(done(read, 'Read rtings.com'))
    put(at(f.entry('say', 'Done. The short answer: the Sony WH-1000XM6 is still the one to beat, and it’s $279 at Best Buy this week.', { data: { final: true, suggestions: ['Show me the alternatives', 'Watch the price'] } })))
    state({ ...blankSession, surface: store.get().data.session.surface })
  })
  return user
}

// at stamps an entry with the time it happens; done finishes a step.
const started = new Map<number, number>()
function at(e: Entry): Entry {
  const t = Date.now()
  started.set(e.id, t)
  return { ...e, createdAt: t, updatedAt: t }
}
const done = (e: Entry, text: string): Entry => ({ ...e, status: 'done', doneText: text, createdAt: started.get(e.id) ?? Date.now(), updatedAt: Date.now() })

// FakeSocket stands in for the live browser's socket: it opens, says hello
// and sends one recorded frame.
class FakeSocket {
  static CONNECTING = 0
  static OPEN = 1
  static CLOSING = 2
  static CLOSED = 3
  readyState = 0
  binaryType = 'blob'
  onopen: (() => void) | null = null
  onmessage: ((e: { data: unknown }) => void) | null = null
  onclose: ((e: { code: number }) => void) | null = null
  onerror: (() => void) | null = null

  constructor(url: string) {
    if (!url.includes('/api/live')) return
    setTimeout(async () => {
      if (this.readyState !== 0) return
      this.readyState = 1
      this.onopen?.()
      this.onmessage?.({ data: JSON.stringify({ type: 'hello', width: 1280, height: 800, url: 'https://www.bestbuy.com/site/sony-wh1000xm6', title: 'Sony WH-1000XM6 – Best Buy' }) })
      const blob = await fetch(frames.shop).then((r) => r.blob())
      if (this.readyState === 1) this.onmessage?.({ data: blob })
    }, 120)
  }

  send() {}
  close() {
    this.readyState = 3
  }
  addEventListener() {}
  removeEventListener() {}
}
