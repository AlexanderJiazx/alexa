// The heights of the panel sheet on a small screen (PanelSheet): peek (its
// head and a sliver), half (the conversation above it, the composer below)
// and full.
export type Detent = 'peek' | 'half' | 'full'
export const detents: Detent[] = ['peek', 'half', 'full']
export const rank = (d: Detent) => detents.indexOf(d)

// A short screen (a phone on its side, or with its keyboard up) has no room
// for half: half would show less of the panel than the peek does, and
// squeeze the conversation, and a form being filled in, to a sliver. There
// the sheet is at its peek or full, and half comes back with the room.
export const shortScreen = '(max-height: 519px)'

// detentsFor is the heights the sheet stops at.
export const detentsFor = (short: boolean): Detent[] => (short ? ['peek', 'full'] : detents)

// stepDetent is the height one step up (+1) or down (-1) from d, or d at
// the end.
export function stepDetent(d: Detent, by: 1 | -1, short: boolean): Detent {
  const r = rank(d)
  const list = detentsFor(short)
  const next = by > 0 ? list.find((x) => rank(x) > r) : [...list].reverse().find((x) => rank(x) < r)
  return next ?? d
}
