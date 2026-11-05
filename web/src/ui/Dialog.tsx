import { useId, type ReactNode } from 'react'
import { cx } from './cx'
import { useModal } from './useModal'

type Props = {
  open: boolean
  onClose: () => void
  title: string
  children?: ReactNode
  // actions sit at the bottom right, the primary one last.
  actions?: ReactNode
  className?: string
}

// Dialog is a small modal on a native <dialog>: the page behind goes inert,
// focus stays inside, Escape or the backdrop closes it, and focus goes back
// where it was.
export function Dialog({ open, onClose, title, children, actions, className }: Props) {
  const m = useModal(open, onClose, 120)
  const id = useId()
  if (!m.shown) return null
  return (
    <dialog ref={m.ref} className={cx('dialog', m.leaving && 'leaving', className)} aria-labelledby={id} onKeyDown={m.onKeyDown} onClick={m.onClick}>
      <div className="dialog-head">
        <h2 id={id}>{title}</h2>
      </div>
      {children && <div className="dialog-body">{children}</div>}
      {actions && <div className="dialog-actions">{actions}</div>}
    </dialog>
  )
}
