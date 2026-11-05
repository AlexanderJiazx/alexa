import { describe, expect, it } from 'vitest'
import { detentsFor, stepDetent } from './detents'

// M2: on a short screen the sheet has no half, which would show less of the
// panel than the peek and leave the conversation a sliver.
describe('detents', () => {
  it('stops at peek, half and full, or at peek and full when short', () => {
    expect(detentsFor(false)).toEqual(['peek', 'half', 'full'])
    expect(detentsFor(true)).toEqual(['peek', 'full'])
  })

  it('steps through half when there is room', () => {
    expect(stepDetent('peek', 1, false)).toBe('half')
    expect(stepDetent('half', 1, false)).toBe('full')
    expect(stepDetent('full', -1, false)).toBe('half')
    expect(stepDetent('full', 1, false)).toBe('full')
    expect(stepDetent('peek', -1, false)).toBe('peek')
  })

  it('goes straight between peek and full on a short screen', () => {
    expect(stepDetent('peek', 1, true)).toBe('full')
    expect(stepDetent('full', -1, true)).toBe('peek')
    // A half left from before the screen got short steps out of it.
    expect(stepDetent('half', 1, true)).toBe('full')
    expect(stepDetent('half', -1, true)).toBe('peek')
  })
})
