// boardTab is where Tab goes while the composer sits in the home board, so
// focus follows what's seen (header, composer, the board's sections)
// rather than the page's order (header, sections, composer). It's given
// the focused element's index among the composer's controls and among the
// sections' (-1 when it's in neither), and answers:
//   - 'more': on from the composer to the first section control
//   - 'bar': back from the first section control to the composer
//   - 'head': back from the composer to the header, past the sections
//   - 'past-bar': on from the last section control past the composer
//   - null: the browser's own Tab is right.
export type BoardTab = 'more' | 'bar' | 'head' | 'past-bar' | null

export function boardTab(at: { bar: number; barCount: number; more: number; moreCount: number }, back: boolean): BoardTab {
  if (at.bar >= 0) {
    if (!back && at.bar === at.barCount - 1) return at.moreCount ? 'more' : null
    if (back && at.bar === 0) return 'head'
    return null
  }
  if (at.more >= 0) {
    if (back && at.more === 0) return at.barCount ? 'bar' : null
    if (!back && at.more === at.moreCount - 1) return 'past-bar'
  }
  return null
}
