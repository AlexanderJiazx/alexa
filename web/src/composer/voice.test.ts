import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { VoiceAborted, VoiceSession } from './voice'

// FE-03: cancelling voice while the microphone prompt, the audio graph or
// the recognizer's socket is still on its way must leave nothing
// listening. A fake getUserMedia, AudioContext and WebSocket let each await
// be held open while the session is cancelled.

function deferred<T>() {
  let resolve!: (v: T) => void
  let reject!: (e: unknown) => void
  const promise = new Promise<T>((ok, no) => {
    resolve = ok
    reject = no
  })
  return { promise, resolve, reject }
}

type Fake = {
  track: { stop: ReturnType<typeof vi.fn> }
  mic: ReturnType<typeof deferred<unknown>>
  module: ReturnType<typeof deferred<void>> | null
  contexts: FakeContext[]
  nodes: FakeNode[]
  sockets: FakeSocket[]
}
let fake: Fake

class FakeContext {
  state = 'running'
  destination = {}
  close = vi.fn(async () => {
    this.state = 'closed'
  })
  audioWorklet = { addModule: () => fake.module?.promise ?? Promise.resolve() }
  constructor() {
    fake.contexts.push(this)
  }
  createMediaStreamSource() {
    return { connect: vi.fn() }
  }
  createGain() {
    return { gain: { value: 1 }, connect: () => this.destination }
  }
}

class FakeNode {
  port = { onmessage: null }
  disconnect = vi.fn()
  constructor() {
    fake.nodes.push(this)
  }
  connect(x: unknown) {
    return x
  }
}

class FakeSocket {
  static OPEN = 1
  readyState = 0
  binaryType = ''
  onopen: (() => void) | null = null
  onclose: (() => void) | null = null
  onerror: (() => void) | null = null
  onmessage: ((e: { data: string }) => void) | null = null
  sent: unknown[] = []
  close = vi.fn(() => {
    this.readyState = 3
    queueMicrotask(() => this.onclose?.())
  })
  constructor(public url: string) {
    fake.sockets.push(this)
  }
  send(d: unknown) {
    this.sent.push(d)
  }
}

const handlers = () => ({ onText: vi.fn(), onTurn: vi.fn(), onError: vi.fn(), onLevel: vi.fn() })
const tick = () => new Promise((r) => setTimeout(r, 0))

beforeEach(() => {
  const track = { stop: vi.fn() }
  fake = { track, mic: deferred<unknown>(), module: null, contexts: [], nodes: [], sockets: [] }
  vi.stubGlobal('navigator', { mediaDevices: { getUserMedia: () => fake.mic.promise }, language: 'en-US', onLine: true })
  vi.stubGlobal('AudioContext', FakeContext)
  vi.stubGlobal('AudioWorkletNode', FakeNode)
  vi.stubGlobal('WebSocket', FakeSocket)
  vi.stubGlobal('location', { protocol: 'http:', host: 'localhost:5185' })
})
afterEach(() => {
  vi.unstubAllGlobals()
})

const stream = () => ({ getTracks: () => [fake.track] })

describe('VoiceSession cancelled while it starts', () => {
  it('stops the microphone it gets after the prompt, and builds nothing', async () => {
    const h = handlers()
    const s = new VoiceSession(h)
    const started = s.start()
    s.cancel()
    fake.mic.resolve(stream())
    await expect(started).rejects.toBeInstanceOf(VoiceAborted)
    expect(fake.track.stop).toHaveBeenCalled()
    expect(fake.contexts).toHaveLength(0)
    expect(fake.sockets).toHaveLength(0)
    expect(h.onError).not.toHaveBeenCalled()
  })

  it('says nothing when the prompt is dismissed after cancelling', async () => {
    const h = handlers()
    const s = new VoiceSession(h)
    const started = s.start()
    s.cancel()
    fake.mic.reject(new Error('NotAllowedError'))
    await expect(started).rejects.toBeInstanceOf(VoiceAborted)
    expect(h.onError).not.toHaveBeenCalled()
  })

  it('closes the audio graph when cancelled while it loads', async () => {
    fake.module = deferred<void>()
    const s = new VoiceSession(handlers())
    const started = s.start()
    fake.mic.resolve(stream())
    await tick()
    expect(fake.contexts).toHaveLength(1)
    s.cancel()
    fake.module.resolve()
    await expect(started).rejects.toBeInstanceOf(VoiceAborted)
    expect(fake.track.stop).toHaveBeenCalled()
    expect(fake.contexts[0].close).toHaveBeenCalled()
    expect(fake.nodes).toHaveLength(0)
    expect(fake.sockets).toHaveLength(0)
  })

  it('closes the socket and the graph when cancelled while the recognizer connects', async () => {
    const s = new VoiceSession(handlers())
    const started = s.start()
    fake.mic.resolve(stream())
    await tick()
    expect(fake.sockets).toHaveLength(1)
    s.cancel()
    await expect(started).rejects.toBeInstanceOf(VoiceAborted)
    expect(fake.sockets[0].close).toHaveBeenCalled()
    expect(fake.nodes[0].disconnect).toHaveBeenCalled()
    expect(fake.contexts[0].close).toHaveBeenCalled()
    expect(fake.track.stop).toHaveBeenCalled()
  })

  it('releases everything when cancelled once listening', async () => {
    const s = new VoiceSession(handlers())
    const started = s.start()
    fake.mic.resolve(stream())
    await tick()
    const ws = fake.sockets[0]
    ws.readyState = 1
    ws.onmessage?.({ data: JSON.stringify({ type: 'ready' }) })
    await expect(started).resolves.toBeUndefined()
    s.cancel()
    s.cancel() // twice is fine
    expect(ws.close).toHaveBeenCalled()
    expect(fake.track.stop).toHaveBeenCalled()
    expect(fake.contexts[0].close).toHaveBeenCalledTimes(1)
  })
})

describe('VoiceSession without permission', () => {
  it('tells the user the microphone was denied', async () => {
    const h = handlers()
    const s = new VoiceSession(h)
    const started = s.start()
    fake.mic.reject(new Error('NotAllowedError'))
    await expect(started).rejects.toThrow('mic denied')
    expect(h.onError).toHaveBeenCalledWith('Microphone access was denied')
  })
})
