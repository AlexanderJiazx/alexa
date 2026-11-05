// Glyphs the composer needs that ui/icons.tsx doesn't have, drawn the same
// way: a 24px grid, 1.5px round strokes, currentColor.
import type { ReactNode } from 'react'

type P = { size?: number; width?: number }

const S = ({ size = 20, width = 1.5, children }: P & { children: ReactNode }) => (
  <svg className="i" width={size} height={size} viewBox="0 0 24 24" style={{ strokeWidth: width }} aria-hidden="true">
    {children}
  </svg>
)

// Speaker: Alexa reading a reply aloud.
export const Speaker = (p: P) => <S {...p}><path d="M4.5 9.5h3l4.5-4v13l-4.5-4h-3z" /><path d="M15.5 9a4 4 0 0 1 0 6M18 6.5a7.5 7.5 0 0 1 0 11" /></S>
// Offline: a cloud with a line through it.
export const Offline = (p: P) => <S {...p}><path d="M8.5 7.2A5.5 5.5 0 0 1 17.4 10a3.75 3.75 0 0 1 2.4 6.4M15 18.5H7a4 4 0 0 1-1.6-7.67" /><path d="M4 4l16 16" /></S>
// Queued: a small clock, for a message waiting its turn.
export const Waiting = (p: P) => <S {...p}><circle cx="12" cy="12" r="7.5" /><path d="M12 8v4l2.5 1.5" /></S>
