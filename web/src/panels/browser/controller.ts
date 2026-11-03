import type { Live as LiveStore } from '../../state/live'
import { createStore, type Store } from '../../state/store'
import { createPainter, type Painter } from './frames'
import type { Point } from './glide'
import type { Size } from './input'
import type { HumanInput, LiveSource, LiveText } from './source'
import { isStale, type SocketState } from './status'

// The live view's controller: one per open browser panel. It owns the
// socket and the canvas painter outside React, and keeps a small store of
// what the view shows (connected, asleep, stale, loading, the page size),
// which changes a few times a minute, never per frame.

export type LiveView = {
  socket: SocketState
  asleep: boolean
  hasFrame: boolean
  stale: boolean
  loading: boolean
  size: Size
  // start is where Alexa's pointer was when the view opened.
  start: Point | null
  // typing: the keyboard field has the focus (phones show their keyboard).
  typing: boolean
}

export type LiveOptions = {
  // cursor is the store Alexa's pointer moves come through: each one is
  // something that should change the page.
  cursor?: Store<LiveStore>
  onHello?(url: string, title?: string): void
  onNotice?(text: string): void
  // painter makes what paints frames onto the canvas; tests pass one that
  // needs no DOM.
  painter?: typeof createPainter
}

// Keys is the keyboard field, which LiveScreen registers so the head's
// keyboard button and address bar can hand the keyboard back to the page.
export type Keys = { focus(): void; blur(): void }

export type Live = ReturnType<typeof createLive>

// crashesIn reads the crash count from the live store.
const crashesIn = (l?: LiveStore) => l?.crashed ?? 0

const initial: LiveView = { socket: 'connecting', asleep: false, hasFrame: false, stale: false, loading: false, size: { w: 1280, h: 800 }, start: null, typing: false }

// navGrace is how long a navigation shows as loading when no frame follows
// it (the page may not have changed).
const navGrace = 1500

export function createLive(source: LiveSource, opts: LiveOptions = {}) {
  const view = createStore<LiveView>(initial)
  const patch = (p: Partial<LiveView>) =>
    view.set((v) => ((Object.keys(p) as (keyof LiveView)[]).every((k) => Object.is(v[k], p[k])) ? v : { ...v, ...p }))

  let canvas: HTMLCanvasElement | null = null
  let painter: Painter | null = null
  let keys: Keys | null = null
  let expectSince: number | null = null
  let painted = 0
  let received = 0
  // The socket as the source reports it, and whether the browser behind it
  // went away (a crash) since it last opened: then the picture is the last
  // one there will be, so the view says Reconnecting even before the
  // socket notices.
  let socket: SocketState = 'connecting'
  let gone = false
  const showSocket = () => patch({ socket: gone && socket === 'open' ? 'closed' : socket, ...(socket === 'open' && !gone ? {} : { stale: false }) })
  const found = () => {
    if (!gone) return
    gone = false
    showSocket()
  }
  const lost = () => {
    gone = true
    showSocket()
  }
  // What makes the loading bar show: a navigation with no frame yet, a
  // browser_open or back in flight, or the server saying so.
  let nav: ReturnType<typeof setTimeout> | undefined
  let navPending = false
  // navPainted: a frame came after the navigation, and the bar is about to
  // go. Later frames don't put that off, or a page that keeps changing (a
  // video, a ticker) would keep it showing for good.
  let navPainted = false
  let stepLoading = false
  let serverLoading = false
  const loading = () => patch({ loading: navPending || stepLoading || serverLoading })

  const settle = () => {
    expectSince = null
    patch({ stale: false })
  }

  // expect says something just happened that should change the page: if no
  // frame comes within staleAfter, the view is stale.
  const expect = (at = performance.now()) => {
    if (expectSince === null && view.get().socket === 'open') expectSince = at
  }

  const onText = (m: LiveText) => {
    if (m.type === 'hello') {
      found()
      settle()
      serverLoading = !!m.loading
      loading()
      patch({ asleep: false, size: { w: m.width || initial.size.w, h: m.height || initial.size.h }, start: m.cursor ? { x: m.cursor.x, y: m.cursor.y } : null })
      if (m.url) opts.onHello?.(m.url, m.title)
    } else if (m.type === 'asleep') {
      settle()
      patch({ asleep: true })
    } else if (m.type === 'notice') {
      opts.onNotice?.(m.text)
    } else if (m.type === 'loading') {
      serverLoading = m.loading
      loading()
    }
  }

  const onFrame = (f: Blob) => {
    received++
    found()
    settle()
    if (navPending && !navPainted) {
      navPainted = true
      clearTimeout(nav)
      nav = setTimeout(() => {
        navPending = false
        loading()
      }, 250)
    }
    painter?.push(f)
  }

  return {
    view,

    // start opens the socket and the painter; the returned function closes
    // them. It runs from an effect, so it can run twice in development.
    start() {
      painter = (opts.painter ?? createPainter)(
        () => canvas,
        () => {
          painted++
          if (!view.get().hasFrame) patch({ hasFrame: true })
        },
      )
      const stop = source.start({
        frame: onFrame,
        text: onText,
        socket: (s) => {
          socket = s
          if (s === 'open') gone = false
          showSocket()
        },
      })
      const tick = setInterval(() => {
        if (!document.hidden && view.get().socket === 'open') patch({ stale: isStale(expectSince, performance.now()) })
      }, 1000)
      // Each move of Alexa's pointer is an action about to land. A crash
      // counted in the same store (Live.crashed, from the hub's browser
      // event "crashed") means the browser is starting again.
      let seq = opts.cursor?.get().cursor?.seq ?? 0
      let crashes = crashesIn(opts.cursor?.get())
      const unwatch = opts.cursor?.subscribe(() => {
        const l = opts.cursor?.get()
        const c = l?.cursor
        if (c && c.seq !== seq) {
          seq = c.seq
          expect()
        }
        const n = crashesIn(l)
        if (n !== crashes) {
          crashes = n
          lost()
        }
      })
      return () => {
        stop()
        clearInterval(tick)
        clearTimeout(nav)
        unwatch?.()
        painter?.dispose()
        painter = null
      }
    },

    // attach gives the painter its canvas (and paints the last frame on it).
    attach(c: HTMLCanvasElement) {
      canvas = c
      painter?.redraw()
      return () => {
        if (canvas === c) canvas = null
      }
    },

    send(ev: HumanInput) {
      return source.send(ev)
    },

    // at is for tests and the gallery.
    expect,
    // settle forgets what was expected: Alexa stopped, or the user handed back.
    settle,

    // navigated: the page went somewhere; the bar shows until it paints.
    navigated() {
      navPending = true
      navPainted = false
      loading()
      clearTimeout(nav)
      nav = setTimeout(() => {
        navPending = false
        loading()
      }, navGrace)
    },
    setStepLoading(on: boolean) {
      stepLoading = on
      loading()
    },

    // lost says the browser behind the view went away (it crashed): the
    // view shows Reconnecting until the socket opens again or a frame
    // comes (B02). The server closes the socket too, with closeRestarted.
    lost,

    wake() {
      socket = 'connecting'
      gone = false
      patch({ asleep: false, socket })
      source.wake()
    },

    // The keyboard field, for the head's keyboard button and address bar.
    setKeys(k: Keys | null) {
      keys = k
    },
    setTyping(typing: boolean) {
      patch({ typing })
    },
    focusKeys() {
      keys?.focus()
    },
    blurKeys() {
      keys?.blur()
    },

    // stats is how many frames arrived and were painted, for the gallery's
    // render check.
    stats: () => ({ received, painted }),
  }
}
