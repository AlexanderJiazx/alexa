// Stroke icons from the design (24px grid, currentColor).
import type { ReactNode } from "react"
type P = { size?: number; width?: number }

const S = ({ size = 20, width = 1.5, children }: P & { children: ReactNode }) => (
  <svg className="i" width={size} height={size} viewBox="0 0 24 24" style={{ strokeWidth: width }} aria-hidden="true">
    {children}
  </svg>
)

export const Plus = (p: P) => <S {...p}><path d="M12 5v14M5 12h14" /></S>
export const Bars = (p: P) => <S {...p}><path d="M4 10v4M8 7v10M12 4v16M16 7v10M20 10v4" /></S>
export const Up = (p: P) => <S width={2} {...p}><path d="M12 19V5M6 11l6-6 6 6" /></S>
export const Close = (p: P) => <S {...p}><path d="M6.5 6.5l11 11M17.5 6.5l-11 11" /></S>
export const Check = (p: P) => <S width={2} {...p}><path d="M5 12.5l4.5 4.5L19 7.5" /></S>
export const Pointer = (p: P) => <S {...p}><path d="M5.5 3.5l13 6.2-5.6 1.7-1.7 5.6z" /></S>
export const HandBack = (p: P) => <S {...p}><path d="M9 14L4 9l5-5" /><path d="M4 9h10.5a5.5 5.5 0 0 1 0 11H11" /></S>
export const Eye = (p: P) => <S {...p}><path d="M2.5 12s3.5-6.5 9.5-6.5 9.5 6.5 9.5 6.5-3.5 6.5-9.5 6.5S2.5 12 2.5 12z" /><circle cx="12" cy="12" r="2.75" /></S>
export const List = (p: P) => <S {...p}><path d="M9 6.5h11M9 12h11M9 17.5h11M4.5 6.5h.01M4.5 12h.01M4.5 17.5h.01" /></S>
export const Chevron = (p: P) => <S {...p}><path d="M6 9.5l6 6 6-6" /></S>
export const Lock = (p: P) => <S width={2.25} {...p}><rect x="5" y="11" width="14" height="9.5" rx="2.5" /><path d="M8.5 11V8a3.5 3.5 0 0 1 7 0v3" /></S>
export const Keyboard = (p: P) => <S {...p}><rect x="3" y="6" width="18" height="12" rx="2.5" /><path d="M7 10h.01M10.5 10h.01M14 10h.01M17 10h.01M8 14h8" /></S>
export const Calendar = (p: P) => <S {...p}><path d="M4.5 6.5h15v13h-15zM4.5 10.5h15M8.5 4v4M15.5 4v4" /></S>
export const You = (p: P) => <S width={2} {...p}><path d="M5 7.5h14M5 12h9M5 16.5h6" /></S>
export const Stop = (p: P) => <S {...p}><rect x="6.5" y="6.5" width="11" height="11" rx="2" /></S>
export const File = (p: P) => <S {...p}><path d="M14 3.5H7a2 2 0 0 0-2 2v13a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V8.5z" /><path d="M14 3.5v5h5" /></S>
export const Download = (p: P) => <S {...p}><path d="M12 4v11M7 10.5l5 5 5-5M5 19.5h14" /></S>
export const LinkIcon = (p: P) => <S {...p}><path d="M10 14a4 4 0 0 0 5.66 0l3-3a4 4 0 0 0-5.66-5.66l-1 1" /><path d="M14 10a4 4 0 0 0-5.66 0l-3 3a4 4 0 0 0 5.66 5.66l1-1" /></S>
export const Back = (p: P) => <S {...p}><path d="M14.5 6l-6 6 6 6" /></S>
export const Fwd = (p: P) => <S {...p}><path d="M9.5 6l6 6-6 6" /></S>
export const Reload = (p: P) => <S {...p}><path d="M19.5 12a7.5 7.5 0 1 1-2.2-5.3" /><path d="M19.5 4.5v4h-4" /></S>
export const Clock = (p: P) => <S {...p}><circle cx="12" cy="12" r="8.5" /><path d="M12 7.5V12l3 2" /></S>
export const Brain = (p: P) => <S {...p}><path d="M12 5.5a3 3 0 0 0-5.6-1.4A3 3 0 0 0 4 9a3 3 0 0 0 .8 5.5A3 3 0 0 0 9 19a3 3 0 0 0 3-1.5M12 5.5a3 3 0 0 1 5.6-1.4A3 3 0 0 1 20 9a3 3 0 0 1-.8 5.5A3 3 0 0 1 15 19a3 3 0 0 1-3-1.5M12 5.5v12" /></S>
export const Folder = (p: P) => <S {...p}><path d="M3.5 7.5a2 2 0 0 1 2-2h4l2 2h7a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2h-13a2 2 0 0 1-2-2z" /></S>
export const Gear = (p: P) => <S {...p}><circle cx="12" cy="12" r="3" /><path d="M19.4 15a1.7 1.7 0 0 0 .3 1.8l.1.1a2 2 0 1 1-2.8 2.8l-.1-.1a1.7 1.7 0 0 0-1.8-.3 1.7 1.7 0 0 0-1 1.5V21a2 2 0 1 1-4 0v-.1a1.7 1.7 0 0 0-1.1-1.5 1.7 1.7 0 0 0-1.8.3l-.1.1a2 2 0 1 1-2.8-2.8l.1-.1a1.7 1.7 0 0 0 .3-1.8 1.7 1.7 0 0 0-1.5-1H3a2 2 0 1 1 0-4h.1a1.7 1.7 0 0 0 1.5-1.1 1.7 1.7 0 0 0-.3-1.8l-.1-.1a2 2 0 1 1 2.8-2.8l.1.1a1.7 1.7 0 0 0 1.8.3H9a1.7 1.7 0 0 0 1-1.5V3a2 2 0 1 1 4 0v.1a1.7 1.7 0 0 0 1 1.5 1.7 1.7 0 0 0 1.8-.3l.1-.1a2 2 0 1 1 2.8 2.8l-.1.1a1.7 1.7 0 0 0-.3 1.8V9a1.7 1.7 0 0 0 1.5 1H21a2 2 0 1 1 0 4h-.1a1.7 1.7 0 0 0-1.5 1z" /></S>
export const People = (p: P) => <S {...p}><circle cx="9" cy="8" r="3.5" /><path d="M2.5 20a6.5 6.5 0 0 1 13 0M16 4.6a3.5 3.5 0 0 1 0 6.8M21.5 20a6.5 6.5 0 0 0-4-6" /></S>
export const Out = (p: P) => <S {...p}><path d="M14.5 4.5h3a2 2 0 0 1 2 2v11a2 2 0 0 1-2 2h-3M10 16.5l-4.5-4.5L10 7.5M5.5 12H15" /></S>
export const Globe = (p: P) => <S {...p}><circle cx="12" cy="12" r="8.5" /><path d="M3.5 12h17M12 3.5c2.5 2.6 3.5 5.5 3.5 8.5s-1 5.9-3.5 8.5c-2.5-2.6-3.5-5.5-3.5-8.5s1-5.9 3.5-8.5z" /></S>
export const Bell = (p: P) => <S {...p}><path d="M6 10a6 6 0 0 1 12 0c0 5 2 6.5 2 6.5H4S6 15 6 10M10 19.5a2 2 0 0 0 4 0" /></S>
export const Trash = (p: P) => <S {...p}><path d="M4.5 7h15M10 11v6M14 11v6M6 7l1 12.5h10L18 7M9 7V4.5h6V7" /></S>
export const Play = (p: P) => <S {...p}><path d="M8 5.5v13l10-6.5z" /></S>
export const Alert = (p: P) => <S {...p}><path d="M12 8.5v4.5M12 16.5h.01" /><circle cx="12" cy="12" r="8.5" /></S>

export function CursorArrow() {
  return (
    <svg width="20" height="20" viewBox="0 0 20 20" aria-hidden="true">
      <path d="M4 2v15l3.8-3.6 2.6 5.6 2.4-1.1-2.6-5.5h5.3z" fill="#111111" stroke="#FFFFFF" style={{ strokeWidth: 1.2, strokeLinejoin: 'round' }} />
    </svg>
  )
}

export function Wave({ size = 'big', anim }: { size?: 'big' | 'small'; anim: string }) {
  const heights = size === 'big' ? [20, 28, 36, 28, 20] : [8, 12, 16, 12, 8]
  return (
    <span className={`wave ${size}`} aria-hidden="true">
      {heights.map((h, i) => (
        <span key={i} style={{ height: h, animation: anim, animationDelay: `${-0.15 * i}s` }} />
      ))}
    </span>
  )
}
