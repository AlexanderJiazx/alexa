import { wsURL } from '../../api/client'
import { closeRevoked, reconnectDelay, stableAfter, type SocketState } from './status'

// The live socket (/api/live): JPEG frames as binary messages, a few text
// messages, and, while the user is in control, their input going the other
// way.

// What the server says in text. hello comes first on every connection.
// notice says why an address didn't open. asleep and loading are planned
// (B10): today's server never sends them.
export type LiveHello = {
  type: 'hello'
  width: number
  height: number
  url?: string
  title?: string
  cursor?: { x: number; y: number }
  loading?: boolean
}
export type LiveText = LiveHello | { type: 'asleep' } | { type: 'notice'; text: string } | { type: 'loading'; loading: boolean }

// HumanInput is one input event for the page (internal/browser HumanInput).
// buttons is the DOM mask of the buttons held (1 left, 2 right, 4 middle);
// a server before be4's change ignores it, and then a drag can't hold its
// button down.
export type HumanInput = {
  kind: 'mousedown' | 'mouseup' | 'mousemove' | 'wheel' | 'keydown' | 'keyup' | 'text' | 'navigate' | 'back' | 'forward' | 'reload'
  x?: number
  y?: number
  button?: 'left' | 'middle' | 'right'
  buttons?: number
  clicks?: number
  dx?: number
  dy?: number
  key?: string
  code?: string
  keyCode?: number
  text?: string
  modifiers?: number
  url?: string
}

export type SourceEvents = {
  frame(frame: Blob): void
  text(m: LiveText): void
  socket(s: SocketState): void
}

// LiveSource is where frames come from: the socket, or a fixture in the
// gallery.
export type LiveSource = {
  start(on: SourceEvents): () => void
  send(ev: HumanInput): boolean
  // wake asks a sleeping browser to start again (B10).
  wake(): void
}

// socketSource is the real thing. It reconnects with backoff (status.ts),
// at once when the server says the browser restarted (once, until the
// socket proves itself with a frame or by staying up), when the network
// comes back, or when the tab is shown again. Each socket is a generation:
// handlers of one that was replaced do nothing.
export function socketSource(path = '/api/live'): LiveSource {
  let ws: WebSocket | null = null
  let on: SourceEvents | null = null
  let timer: ReturnType<typeof setTimeout> | undefined
  let stable: ReturnType<typeof setTimeout> | undefined
  // attempt counts reconnects since the last healthy socket: one that sent
  // a frame or stayed open for stableAfter. Opening alone isn't enough: the
  // server opens, then closes at once if the browser dies as it starts.
  let attempt = 0
  let opened = false
  let sleeping = false

  const connect = (wake = false) => {
    clearTimeout(timer)
    const s = new WebSocket(wsURL(wake ? `${path}?wake=1` : path))
    s.binaryType = 'blob'
    ws = s
    s.onopen = () => {
      if (ws !== s) return
      opened = true
      clearTimeout(stable)
      stable = setTimeout(() => {
        if (ws === s) attempt = 0
      }, stableAfter)
      on?.socket('open')
    }
    s.onmessage = (ev) => {
      if (ws !== s) return
      if (typeof ev.data !== 'string') {
        attempt = 0
        on?.frame(ev.data as Blob)
        return
      }
      let m: LiveText
      try {
        m = JSON.parse(ev.data)
      } catch {
        return
      }
      if (m.type === 'asleep') sleeping = true
      if (m.type === 'hello') sleeping = false
      on?.text(m)
    }
    s.onclose = (ev) => {
      if (ws !== s) return
      ws = null
      clearTimeout(stable)
      if (!on) return
      on.socket(opened ? 'closed' : 'connecting')
      // Asleep stays asleep until Wake; signed out is the app's business.
      if (sleeping || ev.code === closeRevoked) return
      attempt++
      timer = setTimeout(connect, reconnectDelay(attempt, ev.code))
    }
  }

  // now skips the wait when there's reason to think it will work.
  const now = () => {
    if (on && !ws && !sleeping) {
      attempt = 0
      connect()
    }
  }
  const onVisible = () => {
    if (!document.hidden) now()
  }

  return {
    start(events) {
      on = events
      opened = false
      sleeping = false
      connect()
      window.addEventListener('online', now)
      document.addEventListener('visibilitychange', onVisible)
      return () => {
        on = null
        clearTimeout(timer)
        clearTimeout(stable)
        window.removeEventListener('online', now)
        document.removeEventListener('visibilitychange', onVisible)
        const s = ws
        ws = null
        s?.close()
      }
    },
    send(ev) {
      if (!ws || ws.readyState !== WebSocket.OPEN) return false
      ws.send(JSON.stringify(ev))
      return true
    },
    wake() {
      if (!on) return
      sleeping = false
      const s = ws
      ws = null
      s?.close()
      on.socket('connecting')
      connect(true)
    },
  }
}
