import { describe, expect, it, vi } from 'vitest'
import { tapDetent } from './PanelSheet'

// The panels themselves need a browser to load; none is drawn here.
vi.mock('./PanelHost', () => ({ PanelHost: () => null }))

// M9: the grabber is 24px, too thin for a finger, so a tap on the head
// does what a tap on it does, at every height.
describe('a tap on the grabber or the head', () => {
  it('switches between half and full, and raises the peek to half', () => {
    expect(tapDetent('peek', false)).toBe('half')
    expect(tapDetent('half', false)).toBe('full')
    expect(tapDetent('full', false)).toBe('half')
  })

  it('switches between the peek and full on a short screen', () => {
    expect(tapDetent('peek', true)).toBe('full')
    expect(tapDetent('full', true)).toBe('peek')
    expect(tapDetent('half', true)).toBe('full')
  })
})
