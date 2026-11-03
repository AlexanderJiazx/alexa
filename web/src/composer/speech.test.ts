import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { speak, speaking, stopSpeaking } from './speech'

// FH-9: a second reply that came while the first was read aloud cut it off
// mid-sentence. Replies queue now, one after another. A fake synth stands
// in for the browser's: it says what it was given and when it was told to
// stop.

class FakeUtterance {
  text: string
  rate = 1
  voice: unknown = null
  onend: (() => void) | null = null
  onerror: (() => void) | null = null
  constructor(text: string) {
    this.text = text
  }
}

type Synth = { speaking: boolean; pending: boolean; said: FakeUtterance[]; cancel: ReturnType<typeof vi.fn>; speak: (u: FakeUtterance) => void; getVoices: () => [] }
let synth: Synth

beforeEach(() => {
  vi.useFakeTimers()
  synth = {
    speaking: false,
    pending: false,
    said: [],
    cancel: vi.fn(() => {
      synth.speaking = false
    }),
    speak(u) {
      synth.said.push(u)
      synth.speaking = true
    },
    getVoices: () => [],
  }
  vi.stubGlobal('window', { speechSynthesis: synth })
  vi.stubGlobal('SpeechSynthesisUtterance', FakeUtterance)
})
afterEach(() => {
  stopSpeaking()
  vi.unstubAllGlobals()
  vi.useRealTimers()
})

const said = () => synth.said.map((u) => u.text)
const finish = () => synth.said[synth.said.length - 1].onend?.()

describe('speak', () => {
  it('queues a reply behind the one being read instead of cutting it off', () => {
    speak('Your table is booked for 7:30.')
    const cancels = synth.cancel.mock.calls.length
    speak('I also sent Sam the details.')
    expect(said()).toEqual(['Your table is booked for 7:30.'])
    expect(synth.cancel.mock.calls.length).toBe(cancels)
    expect(speaking.get()).toEqual({ on: true, text: 'Your table is booked for 7:30.' })
    finish()
    expect(said()).toEqual(['Your table is booked for 7:30.', 'I also sent Sam the details.'])
    expect(speaking.get()).toEqual({ on: true, text: 'I also sent Sam the details.' })
    finish()
    expect(speaking.get().on).toBe(false)
  })

  it('stops what is being read and drops what waits', () => {
    const ended: string[] = []
    speak('one', () => ended.push('one'))
    speak('two', () => ended.push('two'))
    speak('three', () => ended.push('three'))
    stopSpeaking()
    expect(said()).toEqual(['one'])
    expect(synth.cancel).toHaveBeenCalled()
    expect(ended.sort()).toEqual(['one', 'three', 'two'])
    expect(speaking.get().on).toBe(false)
  })

  it('stops one line on its own: a waiting one leaves the queue, the one read gives way to the next', () => {
    speak('one')
    const two = speak('two')
    speak('three')
    two()
    finish()
    expect(said()).toEqual(['one', 'three'])
    const four = speak('four')
    four() // still waiting behind three
    finish()
    expect(said()).toEqual(['one', 'three'])
    expect(speaking.get().on).toBe(false)
  })

  it('notices the synth went quiet when it never says it ended', () => {
    speak('one')
    speak('two')
    synth.speaking = false
    vi.advanceTimersByTime(800)
    expect(said()).toEqual(['one', 'two'])
  })
})
