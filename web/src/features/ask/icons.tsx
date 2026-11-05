// Glyphs the ask cards need that ui/icons.tsx doesn't have, drawn the same
// way: a 24px grid, 1.5px round strokes, currentColor.
import type { ReactNode } from 'react'

type P = { size?: number; width?: number }

const S = ({ size = 20, width = 1.5, children }: P & { children: ReactNode }) => (
  <svg className="i" width={size} height={size} viewBox="0 0 24 24" style={{ strokeWidth: width }} aria-hidden="true">
    {children}
  </svg>
)

export const Minus = (p: P) => <S {...p}><path d="M5 12h14" /></S>
// Later: an arrow curving round, for a question skipped for now.
export const Later = (p: P) => <S {...p}><path d="M5 12a7 7 0 1 0 2.05-4.95" /><path d="M5 4.5V8h3.5" /><path d="M12 8.5V12l2.5 1.5" /></S>
// Enter: the return key, for the key hints.
export const Enter = (p: P) => <S {...p}><path d="M19 5.5v6a2.5 2.5 0 0 1-2.5 2.5H6" /><path d="M9.5 10.5L6 14l3.5 3.5" /></S>
