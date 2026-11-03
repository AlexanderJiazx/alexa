import { createContext, useContext, type ReactNode } from 'react'
import { Close } from '../ui/icons'
import { IconButton } from '../ui/IconButton'
import { Sheet as ModalSheet } from '../ui/Sheet'
import './sheets.css'

// The gallery draws sheets in place, side by side, rather than as modals
// over the page; the app never sets this.
export const InlineSheets = createContext(false)

type Props = { open: boolean; title: string; onClose: () => void; children: ReactNode }

// Sheet is the Notifications inbox's frame (Settings has its own window):
// the ui/ Sheet, a native modal <dialog> that traps focus, closes on Escape
// and hands focus back to whatever opened it (FE-07).
export function Sheet({ open, title, onClose, children }: Props) {
  const inline = useContext(InlineSheets)
  if (inline)
    return (
      <section className="sheet sheet-inline" aria-label={title}>
        <div className="sheet-head">
          <h2>{title}</h2>
          <div className="sheet-actions">
            <IconButton label="Close" onClick={onClose}>
              <Close size={18} />
            </IconButton>
          </div>
        </div>
        <div className="sheet-body">{children}</div>
      </section>
    )
  return (
    <ModalSheet open={open} onClose={onClose} title={title}>
      {children}
    </ModalSheet>
  )
}
