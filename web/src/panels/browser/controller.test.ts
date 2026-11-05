import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { Live as LiveStore } from '../../state/live'
import { createStore } from '../../state/store'
import { createLive } from './controller'
import type { Painter } from './frames'
import type { LiveSource, SourceEvents } from './source'
import { liveStatus } from './status'

// The controller runs outside React and the DOM, so it's tested with a
// source the test drives and a painter that paints at once.

function setup(cursor = createStore<LiveStore & { crashed?: number }>({ url: '', title: '', cursor: null })) {
  let on: SourceEvents | null = null
  const source: LiveSource = {
    start(events) {
      on = events
      return () => (on = null)
    },
    send: () => true,
    wake: () => {},
  }
  const painter = (_: unknown, onPaint: () => void): Painter => ({ push: () => onPaint(), redraw: () => {}, dispose: () => {} })
  const live = createLive(source, { cursor, painter })
  const stop = live.start()
  const status = () => liveStatus(live.view.get())
  const frame = () => on!.frame(new Blob())
  const open = () => {
    on!.socket('open')
    on!.text({ type: 'hello', width: 1280, height: 800 })
    frame()
  }
  return { live, stop, status, frame, open, cursor, on: () => on! }
}

beforeEach(() => {
  vi.useFakeTimers()
  vi.stubGlobal('document', { hidden: false })
})
afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

describe('the live controller', () => {
  it('connects, then shows live once a frame is painted', () => {
    const t = setup()
    expect(t.status()).toBe('connecting')
    t.on().socket('open')
    expect(t.status()).toBe('connecting')
    t.frame()
    expect(t.status()).toBe('live')
    t.stop()
  })

  it('says reconnecting when the socket drops, and live when it’s back', () => {
    const t = setup()
    t.open()
    t.on().socket('closed')
    expect(t.status()).toBe('reconnecting')
    t.on().socket('open')
    expect(t.status()).toBe('live')
    t.stop()
  })

  it('goes stale when something should have changed the page and no frame came', () => {
    const t = setup()
    t.open()
    t.live.expect()
    vi.advanceTimersByTime(4000)
    expect(t.status()).toBe('live')
    vi.advanceTimersByTime(2000)
    expect(t.status()).toBe('stale')
    t.frame()
    expect(t.status()).toBe('live')
    t.stop()
  })

  it('stays live on a still page: no frame is expected', () => {
    const t = setup()
    t.open()
    vi.advanceTimersByTime(30000)
    expect(t.status()).toBe('live')
    t.stop()
  })

  it('counts each move of Alexa’s pointer as something that should change the page', () => {
    const t = setup()
    t.open()
    t.cursor.set((l) => ({ ...l, cursor: { x: 10, y: 10, click: true, seq: 1 } }))
    vi.advanceTimersByTime(6000)
    expect(t.status()).toBe('stale')
    t.stop()
  })

  it('shows reconnecting as soon as the browser crashes, until it’s back', () => {
    const t = setup()
    t.open()
    t.live.lost()
    expect(t.status()).toBe('reconnecting')
    // The old socket closes, and a new one opens on the new browser.
    t.on().socket('closed')
    expect(t.status()).toBe('reconnecting')
    t.on().socket('open')
    expect(t.status()).toBe('live')
    t.stop()
  })

  it('hears a crash counted in the live store', () => {
    const t = setup()
    t.open()
    t.cursor.set((l) => ({ ...l, crashed: 1 }))
    expect(t.status()).toBe('reconnecting')
    t.frame()
    expect(t.status()).toBe('live')
    t.stop()
  })

  it('shows loading after a navigation until the page paints', () => {
    const t = setup()
    t.open()
    t.live.navigated()
    expect(t.live.view.get().loading).toBe(true)
    t.frame()
    vi.advanceTimersByTime(300)
    expect(t.live.view.get().loading).toBe(false)
    t.stop()
  })

  it('stops loading after a while when no frame follows a navigation', () => {
    const t = setup()
    t.open()
    t.live.navigated()
    vi.advanceTimersByTime(1600)
    expect(t.live.view.get().loading).toBe(false)
    t.stop()
  })
})

describe('the loading bar on a busy page', () => {
  it('goes once the page paints, even while frames keep coming', () => {
    const t = setup()
    t.open()
    t.live.navigated()
    for (let i = 0; i < 20; i++) {
      t.frame()
      vi.advanceTimersByTime(80)
    }
    expect(t.live.view.get().loading).toBe(false)
    t.stop()
  })
})
