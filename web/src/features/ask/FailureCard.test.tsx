import { renderToStaticMarkup } from 'react-dom/server'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { Entry } from '../../api/types'
import { FailureCard, repaired } from './FailureCard'

// The shared clock stands in for itself, to count who reads it.
const clock = vi.hoisted(() => ({ reads: 0 }))
vi.mock('../../ui/Timer', () => ({
  useNow: () => {
    clock.reads++
    return Date.now()
  },
}))

describe('Fix conversation', () => {
  it('says what it set aside, and when there was nothing to', () => {
    expect(repaired(['IMG_2041.heic'])).toBe('Set aside what Alexa couldn’t read.')
    // With nothing it couldn't read (a refusal over something else, say),
    // the server returns setAside: [] and starts no run: no fix to claim.
    expect(repaired([])).toBe('There was nothing Alexa couldn’t read to set aside.')
  })
})

// FH-20: every error card read the clock, so each re-rendered every
// second, in Reflect too. Only a countdown before Try again reads it now.
describe('Try again', () => {
  const failed = (retryAt?: number): Entry => ({
    id: 9,
    kind: 'error',
    text: 'Too many requests.',
    createdAt: 0,
    updatedAt: 0,
    data: { failure: { reason: 'rate_limited', retryable: true, retryAt } },
  })
  const html = (e: Entry, live = true) => renderToStaticMarkup(<FailureCard entry={e} live={live} />)
  beforeEach(() => {
    clock.reads = 0
  })

  it('counts down only while the server asks to wait', () => {
    const out = html(failed(Date.now() + 65e3))
    expect(out).toMatch(/Try again in 1:0[45]/)
    expect(out).toContain('disabled=""')
    expect(clock.reads).toBe(1)
  })

  it('leaves the clock alone with no wait, a wait that’s over, or in Reflect', () => {
    expect(html(failed())).toContain('Try again')
    expect(html(failed(Date.now() - 1000))).not.toContain('Try again in')
    html(failed(Date.now() + 65e3), false)
    expect(clock.reads).toBe(0)
  })
})
