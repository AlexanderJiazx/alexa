import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { ServerEvent } from '../api/realtime'
import type { Entry, SessionState } from '../api/types'
import { blankSession } from '../state/reducer'

// FH-9: after one voice message, every later reply was read aloud until
// the user next typed, a scheduled job's hours later included. Only the
// run that answers a voice message is read now.

const said = vi.hoisted(() => [] as string[])
vi.mock('./speech', () => ({ speak: (t: string) => void said.push(t) }))

const { speakReplies, voiceFailed, voiceSent, voiced } = await import('./spoken')

const entry = (id: number, kind: string, text = `${kind} ${id}`, data?: Entry['data']): ServerEvent => ({ type: 'entry', data: { id, kind, text, createdAt: 0, updatedAt: 0, data } })
const state = (status: SessionState['status'], queued?: number): ServerEvent => ({ type: 'state', data: { ...blankSession, status, queued } })
const run = (...evs: ServerEvent[]) => evs.forEach(speakReplies)

beforeEach(() => {
  said.length = 0
  voiceSent(false, 0, false)
})

describe('speakReplies', () => {
  it('reads what Alexa says and asks in reply to a voice message, once', () => {
    voiceSent(true, 10, false)
    run(entry(11, 'user'), state('working'), entry(12, 'step'), entry(13, 'say', 'Booked.'), entry(13, 'say', 'Booked.'), entry(14, 'ask', 'Text Sam?'))
    expect(said).toEqual(['Booked.', 'Text Sam?'])
  })

  it('stops once the run that answered it ends', () => {
    voiceSent(true, 10, false)
    run(state('working'), entry(12, 'say', 'Booked.'), state('idle'), entry(15, 'say', 'Later news.'))
    expect(said).toEqual(['Booked.'])
    expect(voiced.on).toBe(false)
  })

  it('doesn’t take an idle state from before the run for its end', () => {
    voiceSent(true, 10, false)
    run(state('idle'), state('working'), entry(12, 'say', 'Booked.'))
    expect(said).toEqual(['Booked.'])
  })

  it('keeps reading while a message sent mid-run waits for the next run', () => {
    voiceSent(true, 10, true)
    run(state('idle', 1), state('working'), entry(12, 'say', 'Booked.'), state('idle', 0), entry(14, 'say', 'Later news.'))
    expect(said).toEqual(['Booked.'])
  })

  it('reads nothing after a typed message', () => {
    voiceSent(true, 10, false)
    voiceSent(false, 12, true)
    run(entry(13, 'say', 'Booked.'))
    expect(said).toEqual([])
  })

  it('reads nothing after a voice message that couldn’t go', () => {
    voiceSent(true, 10, false)
    voiceFailed()
    run(state('working'), entry(13, 'say', 'Booked.'))
    expect(said).toEqual([])
  })

  it('never reads a scheduled job', () => {
    voiceSent(true, 10, false)
    run(state('working'), entry(11, 'note', 'Scheduled: Morning brief', { scheduled: true }), entry(12, 'say', 'Here’s your brief.'))
    expect(said).toEqual([])
  })
})
