// The report's own glyphs, drawn like ui/icons.tsx: a 24px grid, 1.5px
// strokes, round caps and joins, in currentColor.
import type { ReactNode } from 'react'
type P = { size?: number }

const S = ({ size = 20, children }: P & { children: ReactNode }) => (
  <svg className="i" width={size} height={size} viewBox="0 0 24 24" aria-hidden="true">
    {children}
  </svg>
)

// Page is an HTML report: a window with its title bar.
export const Page = (p: P) => <S {...p}><rect x="3.5" y="4.5" width="17" height="15" rx="2.5" /><path d="M3.5 9h17M6.75 6.75h.01M9.25 6.75h.01" /></S>
// Away is a link that leaves the app: an arrow up and to the right.
export const Away = (p: P) => <S {...p}><path d="M8 16l8-8M9.5 8H16v6.5" /></S>
