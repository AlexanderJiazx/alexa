import { useId, useRef, type PointerEvent, type ReactNode } from 'react'
import { cx } from './cx'
import { Close } from './icons'
import { IconButton } from './IconButton'
import { useModal } from './useModal'

type Props = {
  open: boolean
  onClose: () => void
  title: string
  // actions sit in the head, before Close.
  actions?: ReactNode
  children: ReactNode
  className?: string
}

// A phone shows the sheet from below, with a grabber.
const phone = () => typeof matchMedia === 'function' && matchMedia('(max-width: 640px)').matches

// shouldClose says whether a drag that ended dy pixels down, moving at v
// pixels a millisecond, puts the sheet away: far enough, or flung.
export function shouldClose(dy: number, v: number, height: number) {
  return dy + v * 180 > Math.min(160, height * 0.3)
}

type Drag = { id: number; y0: number; y: number; t: number; v: number; moved: boolean }

// Sheet is a tall modal for a task or a setting: from the right on wide
// screens, a bottom sheet with a grabber on phones. It shares its look with
// the account sheets (.sheet). On a phone its head can be pulled down to
// put it away, as the grabber says; × , the backdrop and Escape still do.
export function Sheet({ open, onClose, title, actions, children, className }: Props) {
  const m = useModal(open, onClose, 200)
  const id = useId()
  const drag = useRef<Drag | null>(null)
  if (!m.shown) return null

  // The drag moves the dialog itself, outside React, so nothing re-renders
  // while a finger is on it.
  const onPointerDown = (e: PointerEvent<HTMLDialogElement>) => {
    const t = e.target as Element
    if (!phone() || e.button !== 0 || !t.closest('.sheet-head') || t.closest('button, a, input, select, textarea')) return
    drag.current = { id: e.pointerId, y0: e.clientY, y: 0, t: e.timeStamp, v: 0, moved: false }
  }
  const onPointerMove = (e: PointerEvent<HTMLDialogElement>) => {
    const d = drag.current
    const el = e.currentTarget
    if (!d || d.id !== e.pointerId) return
    const dy = e.clientY - d.y0
    if (!d.moved) {
      if (Math.abs(dy) < 6) return
      d.moved = true
      el.setPointerCapture?.(e.pointerId)
      el.dataset.dragging = ''
    }
    // Pulled up, it gives a little, like a rubber band.
    const y = dy < 0 ? dy / 4 : dy
    d.v = 0.8 * ((y - d.y) / Math.max(1, e.timeStamp - d.t)) + 0.2 * d.v
    d.y = y
    d.t = e.timeStamp
    el.style.transform = `translateY(${y}px)`
  }
  const onPointerEnd = (e: PointerEvent<HTMLDialogElement>) => {
    const d = drag.current
    const el = e.currentTarget
    if (!d || d.id !== e.pointerId) return
    drag.current = null
    if (!d.moved) return
    delete el.dataset.dragging
    if (e.type === 'pointerup' && shouldClose(d.y, d.v, el.offsetHeight)) {
      // It goes on down from where the finger left it.
      el.dataset.flung = ''
      onClose()
      return
    }
    el.style.transform = ''
  }

  return (
    <dialog
      ref={m.ref}
      className={cx('sheet', m.leaving && 'leaving', className)}
      aria-labelledby={id}
      onKeyDown={m.onKeyDown}
      onClick={m.onClick}
      onPointerDown={onPointerDown}
      onPointerMove={onPointerMove}
      onPointerUp={onPointerEnd}
      onPointerCancel={onPointerEnd}
    >
      <div className="sheet-head">
        <h2 id={id}>{title}</h2>
        <div className="sheet-actions">
          {actions}
          <IconButton label="Close" onClick={onClose}>
            <Close size={18} />
          </IconButton>
        </div>
      </div>
      <div className="sheet-body">{children}</div>
    </dialog>
  )
}
