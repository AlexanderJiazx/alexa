import { describe, expect, it } from 'vitest'
import { cardShown } from './presence'

// FH-16: the dock showed for a frame as a question's card arrived, before
// the observer said the card was on screen. A card whose place isn't known
// yet keeps the dock away; one not in the exchange, or scrolled off, doesn't.
describe('cardShown', () => {
  it('leaves a card the observer hasn’t reported on yet to itself', () => {
    expect(cardShown({ 41: 'unknown' }, 41)).toBe(true)
  })
  it('follows the observer once it has reported', () => {
    expect(cardShown({ 41: true }, 41)).toBe(true)
    expect(cardShown({ 41: false }, 41)).toBe(false)
  })
  it('shows the dock for a card that isn’t in the exchange', () => {
    expect(cardShown({}, 41)).toBe(false)
    expect(cardShown({ 41: true }, undefined)).toBe(false)
  })
})
