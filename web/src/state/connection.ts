import { api, ApiError, wsURL } from '../api/client'
import { afterParam, closeLagged, closeRevoked, eventsPath, type BrowserEvent, type ClientMessage, type Hello, type ServerEvent } from '../api/realtime'
import type { AppNotification, SessionData } from '../api/types'
import { needsEarlier } from './exchange'
import { applyBrowser } from './live'
import type { Action, Sync } from './reducer'
import { dispatch, setApp, store, type ConnectionStatus } from './store'

// The connection keeps one event socket to the server open and the store in
// step with it:
//
//   - One socket per generation (FE-02). Every reconnect starts a new
//     generation, and a stale socket's late events and close are ignored, so
//     a phone waking up never ends up with two.
//   - Sync on every hello, the first included (FE-01). The server says hello
//     once it has subscribed this client, so nothing published after the
//     snapshot is read can be missed. Events that arrive while the snapshot
//     is on its way are buffered by the reducer and applied after it, newer
//     ones only.
//   - With WP-18 the client reconnects with ?after=epoch:seq and the server
//     replays what was missed instead (or says resync). A gap in seq means
//     events were dropped: resync.
//   - It tells the server whether the user is looking (attention, and the
//     visibility message today's server understands), which decides when
//     Alexa's messages arrive as push notifications. The server counts an
//     attention report for two minutes only, so it's repeated while the
//     socket is open.
//   - Close code 4401 (revoked) goes back to sign-in; 4000 (fell behind)
//     reconnects at once.
//   - A snapshot that fails is tried again, with backoff, while the socket
//     is open, and the next hello takes one even where the server could
//     replay: until a snapshot lands, the feed may have a hole in it.

export type SocketLike = {
  readyState: number
  send: (data: string) => void
  close: (code?: number, reason?: string) => void
  onopen: ((ev: unknown) => void) | null
  onmessage: ((ev: { data: unknown }) => void) | null
  onclose: ((ev: { code: number }) => void) | null
}

const OPEN = 1

export type Env = {
  visible: () => boolean
  focused: () => boolean
  online: () => boolean
  setTimeout: (fn: () => void, ms: number) => unknown
  clearTimeout: (t: unknown) => void
  // listen adds an event listener to the document or the window and returns
  // what removes it.
  listen: (target: 'document' | 'window', type: string, fn: () => void) => () => void
}

export type Snapshot = { snap: SessionData; notes?: AppNotification[] }

export type ConnectionDeps = {
  open: (path: string) => SocketLike
  snapshot: () => Promise<Snapshot>
  dispatch: (a: Action) => void
  sync: () => Sync
  browser: (d: BrowserEvent) => void
  status: (s: ConnectionStatus) => void
  unauthorized: () => void
  // after runs once a snapshot is in, for follow-up loading.
  after?: () => Promise<void>
  // event is told about every event, once it's been handed to the store.
  event?: (ev: ServerEvent) => void
  syncError?: (err: unknown, first: boolean) => void
  // signedIn checks whether the session is still good after the socket
  // fails to open, which is how a 401 on the upgrade looks from here.
  signedIn?: () => Promise<boolean>
  env: Env
  // How long to wait for a hello before syncing anyway.
  helloWait?: number
}

export type Connection = {
  start: () => void
  stop: () => void
  resync: () => Promise<void>
  // generation counts the sockets opened so far (for tests).
  generation: () => number
}

// backoff is how long to wait before reconnect attempt n (1-based).
export function backoff(attempt: number) {
  return Math.min(15000, 500 * 2 ** attempt)
}

// attentionEvery is how often attention is repeated while the socket is
// open: well inside the two minutes the server counts a report for.
export const attentionEvery = 45000

// isGap says whether seq skips past what was last seen.
export function isGap(last: number | undefined, seq: number) {
  return last !== undefined && seq > last + 1
}

export function createConnection(d: ConnectionDeps): Connection {
  const env = d.env
  let gen = 0
  let ws: SocketLike | null = null
  let stopped = true
  let attempt = 0
  let failedOpens = 0
  let timer: unknown
  let helloTimer: unknown
  let beatTimer: unknown
  let everLive = false
  let syncing = false
  let again = false
  let synced = false
  // syncFails counts the snapshots that failed in a row; while it's above
  // zero the feed can't be trusted to be whole.
  let syncFails = 0
  let syncTimer: unknown
  // helloGen is the generation whose hello has arrived; syncGen is the one
  // the snapshot on its way was asked for in, after that hello.
  let helloGen = 0
  let syncGen = 0
  // What this client has seen of the server's event stream.
  let epoch: string | undefined
  let seq: number | undefined
  let unlisten: (() => void)[] = []

  const setStatus = (s: ConnectionStatus) => d.status(env.online() ? s : 'offline')

  const send = (m: ClientMessage) => {
    if (ws && ws.readyState === OPEN) ws.send(JSON.stringify(m))
  }
  const attention = () => {
    const visible = env.visible()
    send({ type: 'attention', visible, focused: visible && env.focused() })
    return visible
  }
  const report = () => send({ type: 'visibility', visible: attention() })
  // beat repeats attention for as long as this generation's socket is open.
  const beat = (g: number) => {
    env.clearTimeout(beatTimer)
    beatTimer = env.setTimeout(() => {
      if (g !== gen || stopped) return
      attention()
      beat(g)
    }, attentionEvery)
  }

  const sync = async () => {
    if (stopped) return
    if (syncing) {
      again = true
      return
    }
    syncing = true
    env.clearTimeout(syncTimer)
    syncGen = helloGen === gen ? gen : 0
    again = false
    const first = !synced
    let ok = false
    d.dispatch({ type: 'syncStart' })
    try {
      const r = await d.snapshot()
      d.dispatch({ type: 'snapshot', snap: r.snap, notes: r.notes })
      if (r.snap.epoch) epoch = r.snap.epoch
      if (typeof r.snap.seq === 'number') seq = Math.max(seq ?? r.snap.seq, r.snap.seq)
      synced = true
      ok = true
      syncFails = 0
      await d.after?.()
    } catch (err) {
      if (!ok) {
        syncFails++
        d.dispatch({ type: 'syncFailed' })
        // Said once per run of failures, not on every retry.
        if (syncFails === 1) d.syncError?.(err, first)
      }
    } finally {
      syncing = false
    }
    if (stopped) return
    if (!ok) retrySync()
    else if (again || d.sync().again) sync()
  }

  // retrySync tries the snapshot again after a while. A socket that has
  // closed by then will say hello on reconnecting, which syncs.
  const retrySync = () => {
    env.clearTimeout(syncTimer)
    syncTimer = env.setTimeout(() => {
      if (!stopped && !syncing && ws && ws.readyState === OPEN) sync()
    }, backoff(syncFails))
  }

  const hello = (g: number, ev: ServerEvent | null, sentAfter: boolean) => {
    if (g !== gen) return
    env.clearTimeout(helloTimer)
    everLive = true
    setStatus('live')
    const h: Hello = { ...(ev ?? {}), ...((ev?.data as Hello | undefined) ?? {}) }
    // Reconnected where the server can replay what was missed: no snapshot,
    // unless the last one failed.
    if (sentAfter && h.epoch && h.epoch === epoch && !syncFails) return
    helloGen = g
    if (h.epoch) epoch = h.epoch
    if (typeof h.seq === 'number') seq = h.seq
    sync()
  }

  const handle = (g: number, ev: ServerEvent, sentAfter: boolean) => {
    if (ev.type === 'hello') return hello(g, ev, sentAfter)
    if (typeof ev.seq === 'number') {
      const restarted = ev.epoch && epoch && ev.epoch !== epoch
      const gap = !restarted && isGap(seq, ev.seq)
      if (ev.epoch) epoch = ev.epoch
      seq = restarted ? ev.seq : Math.max(seq ?? ev.seq, ev.seq)
      if (restarted || gap) sync()
    }
    if (ev.type === 'browser') {
      if (ev.data) d.browser(ev.data)
    } else if (ev.type === 'resync') {
      // After a restart the server says hello and then resync; the snapshot
      // the hello asked for already has everything.
      if (!(syncing && syncGen === g)) sync()
    } else {
      d.dispatch({ type: 'event', ev })
      if (d.sync().again && !d.sync().pending) sync()
    }
    d.event?.(ev)
  }

  const connect = () => {
    if (stopped) return
    env.clearTimeout(timer)
    env.clearTimeout(helloTimer)
    env.clearTimeout(beatTimer)
    const g = ++gen
    const after = afterParam(epoch, seq)
    const sock = d.open(eventsPath(after))
    ws = sock
    let opened = false
    setStatus(everLive ? 'reconnecting' : 'connecting')
    sock.onopen = () => {
      if (g !== gen) return
      opened = true
      attempt = 0
      failedOpens = 0
      report()
      beat(g)
      // A server that doesn't say hello still gets synced.
      helloTimer = env.setTimeout(() => hello(g, null, false), d.helloWait ?? 1500)
    }
    sock.onmessage = (m) => {
      if (g !== gen || typeof m.data !== 'string') return
      let ev: ServerEvent
      try {
        ev = JSON.parse(m.data)
      } catch {
        return
      }
      if (ev && typeof ev.type === 'string') handle(g, ev, !!after)
    }
    sock.onclose = (e) => {
      if (g !== gen) return
      ws = null
      env.clearTimeout(helloTimer)
      env.clearTimeout(beatTimer)
      if (stopped) return
      if (e.code === closeRevoked) {
        stop()
        d.unauthorized()
        return
      }
      if (!opened) failedOpens++
      setStatus('reconnecting')
      // Dropped for falling behind: the server is fine, so catch up now.
      if (e.code === closeLagged && opened) {
        attempt = 0
        timer = env.setTimeout(connect, 0)
        return
      }
      attempt++
      // Two failed opens in a row may be the server turning the upgrade
      // away because the session is gone.
      if (failedOpens >= 2 && d.signedIn) {
        failedOpens = 0
        d.signedIn().then((ok) => {
          if (!ok && !stopped) {
            stop()
            d.unauthorized()
          }
        })
      }
      timer = env.setTimeout(connect, backoff(attempt))
    }
  }

  // reconnectNow drops a socket that isn't open and starts a new one at
  // once; the old one's close arrives for a stale generation and is ignored.
  const reconnectNow = () => {
    const old = ws
    ws = null
    connect()
    try {
      old?.close()
    } catch {
      /* already closed */
    }
  }

  const onVisibility = () => {
    report()
    if (env.visible() && !stopped && (!ws || ws.readyState > OPEN)) reconnectNow()
  }
  const onOnline = () => {
    if (!stopped && (!ws || ws.readyState !== OPEN)) reconnectNow()
    else setStatus('live')
  }
  const onOffline = () => d.status('offline')

  function stop() {
    if (stopped) return
    stopped = true
    gen++
    env.clearTimeout(timer)
    env.clearTimeout(helloTimer)
    env.clearTimeout(beatTimer)
    env.clearTimeout(syncTimer)
    unlisten.forEach((f) => f())
    unlisten = []
    const old = ws
    ws = null
    old?.close()
  }

  return {
    start() {
      if (!stopped) return
      stopped = false
      attempt = 0
      unlisten = [
        env.listen('document', 'visibilitychange', onVisibility),
        env.listen('window', 'focus', report),
        env.listen('window', 'blur', report),
        env.listen('window', 'online', onOnline),
        env.listen('window', 'offline', onOffline),
      ]
      connect()
    },
    stop,
    resync: () => sync(),
    generation: () => gen,
  }
}

// ---------- the app's connection ----------

const browserEnv: Env = {
  visible: () => document.visibilityState === 'visible',
  focused: () => document.hasFocus(),
  online: () => navigator.onLine !== false,
  setTimeout: (fn, ms) => setTimeout(fn, ms),
  clearTimeout: (t) => clearTimeout(t as ReturnType<typeof setTimeout>),
  listen: (target, type, fn) => {
    const t = target === 'document' ? document : window
    t.addEventListener(type, fn)
    return () => t.removeEventListener(type, fn)
  },
}

const listeners = new Set<(ev: ServerEvent) => void>()

// onServerEvent subscribes to every event from the server, after the store
// has it (it may still be buffered while a snapshot loads). It returns what
// unsubscribes.
export function onServerEvent(fn: (ev: ServerEvent) => void) {
  listeners.add(fn)
  return () => {
    listeners.delete(fn)
  }
}

// fillExchange loads earlier pages when the snapshot doesn't reach back to
// where the current exchange began, as on a long run (FE-12).
async function fillExchange() {
  for (let i = 0; i < 3; i++) {
    const { feed, hasMore } = store.get().data
    if (!needsEarlier(feed, hasMore)) return
    dispatch({ type: 'earlier', page: await api.feed(feed[0].id) })
  }
}

let current: Connection | null = null

// connectApp starts the app's connection; what it returns stops it.
export function connectApp(h: { unauthorized: () => void; syncError: (err: unknown, first: boolean) => void }) {
  current?.stop()
  const c = createConnection({
    open: (path) => new WebSocket(wsURL(path)) as unknown as SocketLike,
    snapshot: async () => {
      const [snap, notes] = await Promise.all([api.session(), api.notifications().catch(() => undefined)])
      return { snap, notes }
    },
    dispatch,
    sync: () => store.get().data.sync,
    browser: applyBrowser,
    status: (s) => setApp({ connection: s }),
    unauthorized: h.unauthorized,
    after: fillExchange,
    event: (ev) => listeners.forEach((f) => f(ev)),
    syncError: h.syncError,
    signedIn: () => api.me().then(
      () => true,
      (e) => !(e instanceof ApiError && e.status === 401),
    ),
    env: browserEnv,
  })
  current = c
  c.start()
  return () => {
    c.stop()
    if (current === c) current = null
  }
}

// resync fetches a fresh snapshot, as after clearing the conversation.
export function resync(): Promise<void> {
  return current?.resync() ?? Promise.resolve()
}
