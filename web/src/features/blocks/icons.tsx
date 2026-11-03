// The blocks' own glyphs, drawn like ui/icons.tsx: a 24px grid, 1.5px round
// strokes in currentColor. The rest (Check, Close, Copy, Expand, ...) come
// from ui/icons.
import type { ReactNode } from 'react'
type P = { size?: number; width?: number }

const S = ({ size = 20, width = 1.5, children }: P & { children: ReactNode }) => (
  <svg className="i" width={size} height={size} viewBox="0 0 24 24" style={{ strokeWidth: width }} aria-hidden="true">
    {children}
  </svg>
)

export const Plane = (p: P) => <S {...p}><path d="M10.2 13.8L4 12.5l-1-1.6 1.2-.7 6.5 1.1 4-4.3C16 5.6 18.3 4.6 19 5.3s-.3 3-1.7 4.3l-4.3 4 1.1 6.5-.7 1.2-1.6-1-1.3-6.2-3 2.8.2 2.2-.9.9-1.2-2.4-2.4-1.2.9-.9 2.2.2z" /></S>
export const Bed = (p: P) => <S {...p}><path d="M3.5 18.5v-12M3.5 14h17v4.5M20.5 14v-2.5a3 3 0 0 0-3-3h-6V14" /><circle cx="7.5" cy="10.5" r="1.75" /></S>
export const Fork = (p: P) => <S {...p}><path d="M7 3.5V9a2 2 0 0 0 2 2 2 2 0 0 0 2-2V3.5M9 3.5v17M16.5 20.5V3.5c-2 .8-3 3.3-3 6.5 0 2 1 3 3 3" /></S>
export const Ticket = (p: P) => <S {...p}><path d="M3.5 8.5v-2a1 1 0 0 1 1-1h15a1 1 0 0 1 1 1v2a3.5 3.5 0 0 0 0 7v2a1 1 0 0 1-1 1h-15a1 1 0 0 1-1-1v-2a3.5 3.5 0 0 0 0-7z" /><path d="M14.5 5.5v2M14.5 11v2M14.5 16.5v2" /></S>
export const Car = (p: P) => <S {...p}><path d="M5 16.5H4a.5.5 0 0 1-.5-.5v-3.2a2 2 0 0 1 .6-1.4L6 9.5l1.6-3.3a1.5 1.5 0 0 1 1.3-.7h6.2a1.5 1.5 0 0 1 1.3.7L18 9.5l1.9 1.9a2 2 0 0 1 .6 1.4V16a.5.5 0 0 1-.5.5h-1M9 16.5h6M6 9.5h12" /><circle cx="7" cy="16.5" r="2" /><circle cx="17" cy="16.5" r="2" /></S>
export const Task = (p: P) => <S {...p}><rect x="4.5" y="4.5" width="15" height="15" rx="3.5" /><path d="M8.5 12.2l2.4 2.3 4.6-4.8" /></S>
export const Pin = (p: P) => <S {...p}><path d="M12 20.5s-6.5-5.4-6.5-10.5a6.5 6.5 0 0 1 13 0c0 5.1-6.5 10.5-6.5 10.5z" /><circle cx="12" cy="10" r="2.25" /></S>
export const Phone = (p: P) => <S {...p}><path d="M8.6 4.5H6.2a1.7 1.7 0 0 0-1.7 1.9 15 15 0 0 0 13.1 13.1 1.7 1.7 0 0 0 1.9-1.7v-2.4a1 1 0 0 0-.8-1l-2.9-.6a1 1 0 0 0-.9.3l-1.4 1.4a11 11 0 0 1-4.6-4.6l1.4-1.4a1 1 0 0 0 .3-.9l-.6-2.9a1 1 0 0 0-1-.8z" /></S>
export const Directions = (p: P) => <S {...p}><path d="M11.3 3.2a1 1 0 0 1 1.4 0l8.1 8.1a1 1 0 0 1 0 1.4l-8.1 8.1a1 1 0 0 1-1.4 0l-8.1-8.1a1 1 0 0 1 0-1.4z" /><path d="M9.5 14.5v-2a1.5 1.5 0 0 1 1.5-1.5h4M13 8.5l2.5 2.5-2.5 2.5" /></S>
export const TrendUp = (p: P) => <S width={2} {...p}><path d="M12 18V6M7 11l5-5 5 5" /></S>
export const TrendDown = (p: P) => <S width={2} {...p}><path d="M12 6v12M7 13l5 5 5-5" /></S>
export const TrendFlat = (p: P) => <S width={2} {...p}><path d="M6 12h12M13 7l5 5-5 5" /></S>
export const Bulb = (p: P) => <S {...p}><path d="M9.5 17.5h5M10 20.5h4M12 3.5a6 6 0 0 0-3.5 10.9c.6.4 1 1.1 1 1.8v1.3h5v-1.3c0-.7.4-1.4 1-1.8A6 6 0 0 0 12 3.5z" /></S>

// The checklist's status marks: a filled tick, a cross, an empty ring and a
// dash for skipped.
export const Done = (p: P) => <S width={2} {...p}><circle className="fill" cx="12" cy="12" r="9" /><path className="on" d="M8 12.3l2.7 2.7L16.2 9.5" /></S>
export const Failed = (p: P) => <S width={2} {...p}><circle className="fill" cx="12" cy="12" r="9" /><path className="on" d="M9.2 9.2l5.6 5.6M14.8 9.2l-5.6 5.6" /></S>
export const Todo = (p: P) => <S {...p}><circle cx="12" cy="12" r="8.25" /></S>
export const Skipped = (p: P) => <S {...p}><circle cx="12" cy="12" r="8.25" strokeDasharray="2.6 2.6" /><path d="M8.5 12h7" /></S>

// Star is the rating glyph, filled with the ink.
export const Star = ({ size = 12 }: { size?: number }) => (
  <svg className="i star" width={size} height={size} viewBox="0 0 24 24" aria-hidden="true">
    <path d="M12 2.8l2.8 5.8 6.3.9-4.6 4.4 1.1 6.3L12 17.2l-5.6 3 1.1-6.3-4.6-4.4 6.3-.9z" />
  </svg>
)
