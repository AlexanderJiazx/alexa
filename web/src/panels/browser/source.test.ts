import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { socketSource } from './source'
import { closeRestarted, reconnectDelay, retryDelay, stableAfter } from './status'

// A WebSocket the test drives: it records each connection, and the test
// opens, feeds and closes it.
class FakeSocket {
  static all: FakeSocket[] = []
  binaryType = ''
  onopen: (() => void) | null = null
  onmessage: ((ev: { data: unknown }) => void) | null = null
  onclose: ((ev: { code: number }) => void) | null = null
  constructor(public url: string) {
    FakeSocket.all.push(this)
  }
  send() {}
  close() {}
  open() {
    this.onopen?.()
  }
  drop(code: number) {
    this.onclose?.({ code })
  }
}

const events = () => ({ frame: vi.fn(), text: vi.fn(), socket: vi.fn() })

beforeEach(() => {
  vi.useFakeTimers()
  FakeSocket.all = []
  vi.stubGlobal('WebSocket', FakeSocket)
  vi.stubGlobal('location', { protocol: 'http:', host: 'localhost' })
  vi.stubGlobal('window', { addEventListener() {}, removeEventListener() {} })
  vi.stubGlobal('document', { hidden: false, addEventListener() {}, removeEventListener() {} })
  vi.spyOn(Math, 'random').mockReturnValue(0.5)
})
afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

describe('reconnectDelay', () => {
  it('comes straight back once when the browser restarted, then backs off', () => {
    expect(reconnectDelay(1, closeRestarted)).toBe(0)
    expect(reconnectDelay(2, closeRestarted, () => 0.5)).toBe(retryDelay(2, () => 0.5))
    expect(reconnectDelay(1, 1006, () => 0.5)).toBe(500)
  })
})

describe('socketSource', () => {
  it('backs off when the browser dies each time it starts', () => {
    const stop = socketSource().start(events())
    // Each connection opens, then the server closes it at once with 1012.
    const cycle = () => {
      const s = FakeSocket.all[FakeSocket.all.length - 1]
      s.open()
      s.drop(closeRestarted)
    }
    cycle()
    vi.advanceTimersByTime(0)
    expect(FakeSocket.all).toHaveLength(2) // the first restart: at once
    cycle()
    vi.advanceTimersByTime(999)
    expect(FakeSocket.all).toHaveLength(2) // then it waits a second
    vi.advanceTimersByTime(1)
    expect(FakeSocket.all).toHaveLength(3)
    cycle()
    vi.advanceTimersByTime(1999)
    expect(FakeSocket.all).toHaveLength(3) // and two
    vi.advanceTimersByTime(1)
    expect(FakeSocket.all).toHaveLength(4)
    stop()
  })

  it('comes straight back again after a socket that sent frames', () => {
    const stop = socketSource().start(events())
    const first = FakeSocket.all[0]
    first.open()
    first.drop(closeRestarted)
    vi.advanceTimersByTime(0)
    const second = FakeSocket.all[1]
    second.open()
    second.onmessage?.({ data: new Blob(['jpeg']) })
    second.drop(closeRestarted)
    vi.advanceTimersByTime(0)
    expect(FakeSocket.all).toHaveLength(3)
    stop()
  })

  it('counts a socket that stays up as healthy, frames or not', () => {
    const stop = socketSource().start(events())
    FakeSocket.all[0].open()
    FakeSocket.all[0].drop(closeRestarted)
    vi.advanceTimersByTime(0)
    FakeSocket.all[1].open()
    vi.advanceTimersByTime(stableAfter)
    FakeSocket.all[1].drop(closeRestarted)
    vi.advanceTimersByTime(0)
    expect(FakeSocket.all).toHaveLength(3)
    stop()
  })
})
