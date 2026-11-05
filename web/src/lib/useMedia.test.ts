import { afterEach, describe, expect, it, vi } from 'vitest'
import { media } from './useMedia'

// FH-19: useMedia made its subscribe afresh on every render, so React
// unsubscribed and subscribed again each time. It's made once per query.

afterEach(() => vi.unstubAllGlobals())

describe('media', () => {
  it('gives the same subscribe for the same query', () => {
    expect(media('(max-width: 640px)').subscribe).toBe(media('(max-width: 640px)').subscribe)
    expect(media('(max-width: 640px)')).not.toBe(media('(max-width: 899px)'))
  })

  it('asks for the query list only once it’s needed, and only once', () => {
    const listeners = new Set<() => void>()
    const list = { matches: true, addEventListener: (_: string, fn: () => void) => listeners.add(fn), removeEventListener: (_: string, fn: () => void) => listeners.delete(fn) }
    const matchMedia = vi.fn(() => list)
    vi.stubGlobal('matchMedia', matchMedia)
    const m = media('(pointer: fine) and (min-width: 1px)')
    expect(matchMedia).not.toHaveBeenCalled()
    const stop = m.subscribe(() => {})
    expect(m.matches()).toBe(true)
    expect(listeners.size).toBe(1)
    stop()
    expect(listeners.size).toBe(0)
    expect(matchMedia).toHaveBeenCalledTimes(1)
  })
})
