import type { StepData } from '../../api/progress'
import type { Entry, Needs } from '../../api/types'
import type { Cursor, Live as LiveStore } from '../../state/live'
import { createStore } from '../../state/store'
import type { Box } from './ActionOverlay'
import code from './fixtures/code.jpg'
import results from './fixtures/results.jpg'
import shop from './fixtures/shop.jpg'
import type { HumanInput, LiveSource } from './source'

// Fixtures for the browser panel's gallery: recorded frames (1280×800, as
// the server's screencast sends them), steps in flight, needs, and a fake
// live socket that plays the frames.

export const frames = { shop, code, results }

export const page = { w: 1280, h: 800 }

// Where things are on the recorded pages, in page pixels.
export const spots = {
  addToCart: { x: 744, y: 418, box: { x: 664, y: 392, w: 160, h: 52 } },
  search: { x: 960, y: 32, box: { x: 862, y: 13, w: 300, h: 38 } },
  firstDigit: { x: 486, y: 396, box: { x: 460, y: 366, w: 52, h: 60 } },
  card: { x: 187, y: 278, box: { x: 48, y: 163, w: 278, h: 230 } },
} satisfies Record<string, { x: number; y: number; box: Box }>

type FakeOptions = {
  // frames to play, in turn; the first is sent at once.
  frames: string[]
  // fps plays them on a loop (the render check).
  fps?: number
  // never: the socket never opens; drop: it opens, sends a frame, and
  // drops (Reconnecting); asleep: the server says the browser is asleep.
  socket?: 'open' | 'never' | 'drop' | 'asleep'
  hello?: { url?: string; cursor?: { x: number; y: number }; loading?: boolean }
}

// fakeSource is a live socket made of fixtures.
export function fakeSource(o: FakeOptions): LiveSource & { sent: HumanInput[] } {
  const sent: HumanInput[] = []
  return {
    sent,
    start(on) {
      let alive = true
      const timers: ReturnType<typeof setTimeout>[] = []
      if (o.socket === 'never') return () => {}
      on.socket('open')
      on.text({ type: 'hello', width: page.w, height: page.h, ...o.hello })
      Promise.all(o.frames.map((u) => fetch(u).then((r) => r.blob()))).then((blobs) => {
        if (!alive) return
        on.frame(blobs[0])
        if (o.socket === 'drop') timers.push(setTimeout(() => alive && on.socket('closed'), 400))
        if (o.socket === 'asleep') timers.push(setTimeout(() => alive && on.text({ type: 'asleep' }), 400))
        if (o.fps) {
          let i = 0
          timers.push(setInterval(() => on.frame(blobs[++i % blobs.length]), 1000 / o.fps))
        }
      })
      return () => {
        alive = false
        timers.forEach((t) => clearInterval(t))
      }
    },
    send(ev) {
      sent.push(ev)
      return true
    },
    wake() {},
  }
}

// cursorAt is a live store with Alexa's pointer resting at a spot; click
// replays a click there.
export function cursorAt(spot: { x: number; y: number; box?: Box } | null, url = '') {
  const cursor = spot ? ({ x: spot.x, y: spot.y, click: false, seq: 1, box: spot.box } as Cursor) : null
  return createStore<LiveStore>({ url, title: '', cursor })
}

let ids = 100
function step(tool: string, kind: StepData['kind'], extra: Partial<StepData>, text: string): Entry {
  const id = ++ids
  return { id, kind: 'step', text, status: 'running', data: { step: { tool, kind, status: 'running', ...extra } }, createdAt: 0, updatedAt: 0 }
}

export const steps = {
  click: step('browser_click', 'click', { host: 'emberandoak.com', target: 'Add to cart' }, 'Adding the grinder to the cart'),
  type: step('browser_type', 'type', { host: 'emberandoak.com', target: 'Search the shop', text: 'burr grinder' }, 'Searching the shop'),
  secret: step('browser_login', 'type', { host: 'acmesavings.example', target: 'One-time code', text: '482913' }, 'Signing in'),
  open: step('browser_open', 'browse', { host: 'emberandoak.com', url: 'https://emberandoak.com/search?q=burr+grinder' }, 'Opening the results'),
}

export const urls = {
  shop: 'https://www.emberandoak.com/grinders/hearth-64-conical-burr',
  results: 'https://www.emberandoak.com/search?q=burr+grinder',
  code: 'https://secure.acmesavings.example/verify?step=2',
}

export const needs: Record<'takeover' | 'login', Needs> = {
  takeover: { kind: 'takeover', message: 'Enter the 6-digit code Acme Savings texted you, then hand back.' },
  login: { kind: 'login', message: 'I need to sign in to check your balance.', site: 'acmesavings.example' },
}
