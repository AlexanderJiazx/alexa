import { describe, expect, it } from 'vitest'
import { boardTab } from './tabOrder'

// Tab on the home board follows what's seen: header, composer, sections.
describe('boardTab', () => {
  const at = (bar: number, more: number) => ({ bar, barCount: 3, more, moreCount: 4 })

  it('goes on from the composer to the board’s sections', () => {
    expect(boardTab(at(2, -1), false)).toBe('more')
    expect(boardTab(at(1, -1), false)).toBeNull()
  })
  it('goes back from the sections to the composer, and from the composer to the header', () => {
    expect(boardTab(at(-1, 0), true)).toBe('bar')
    expect(boardTab(at(0, -1), true)).toBe('head')
    expect(boardTab(at(-1, 2), true)).toBeNull()
  })
  it('leaves the page from the last section without coming back to the composer', () => {
    expect(boardTab(at(-1, 3), false)).toBe('past-bar')
  })
  it('leaves the rest to the browser', () => {
    expect(boardTab(at(-1, -1), false)).toBeNull()
    expect(boardTab({ bar: 0, barCount: 1, more: -1, moreCount: 0 }, false)).toBeNull()
  })
})
