import { useEffect, useRef, useState, type MouseEvent, type KeyboardEvent } from 'react'
import { trapTab } from './focus'

const reduceMotion = typeof matchMedia === 'function' ? matchMedia('(prefers-reduced-motion: reduce)') : null

// useModal drives a native <dialog> as a modal: showModal() when open turns
// true (the rest of the page goes inert), Escape and a click on the backdrop
// ask onClose, Tab stays inside, and on the way out it plays the exit for
// exitMs before closing and handing focus back to what had it before.
export function useModal(open: boolean, onClose: () => void, exitMs: number) {
  const ref = useRef<HTMLDialogElement>(null)
  const returnTo = useRef<HTMLElement | null>(null)
  const close = useRef(onClose)
  close.current = onClose
  const [shown, setShown] = useState(open)
  const [leaving, setLeaving] = useState(false)
  if (open && !shown) setShown(true)

  useEffect(() => {
    const d = ref.current
    if (!d) return
    if (open) {
      setLeaving(false)
      if (!d.open) {
        returnTo.current = document.activeElement as HTMLElement | null
        d.showModal()
      }
      return
    }
    const how = closing(open, d.open, shown)
    if (how === 'now') {
      setLeaving(false)
      setShown(false)
      returnTo.current?.focus?.()
      return
    }
    if (how !== 'play') return
    setLeaving(true)
    const t = setTimeout(
      () => {
        d.close()
        setLeaving(false)
        setShown(false)
        returnTo.current?.focus?.()
      },
      reduceMotion?.matches ? 0 : exitMs,
    )
    return () => clearTimeout(t)
  }, [open, shown, exitMs])

  // Escape fires cancel: animate out through onClose rather than letting the
  // browser close at once. If the browser closes anyway, keep the owner's
  // state in step, asking once per Escape (closeOnce).
  useEffect(() => {
    const d = ref.current
    if (!d) return
    const once = closeOnce(
      () => close.current(),
      () => open,
    )
    const cancel = (e: Event) => once.cancel(e)
    const closed = () => once.closed()
    d.addEventListener('cancel', cancel)
    d.addEventListener('close', closed)
    return () => {
      d.removeEventListener('cancel', cancel)
      d.removeEventListener('close', closed)
    }
  }, [open, shown])

  // Unmounted while open: close it and give focus back.
  useEffect(
    () => () => {
      if (ref.current?.open) ref.current.close()
      returnTo.current?.focus?.()
    },
    [],
  )

  return {
    ref,
    shown,
    leaving,
    onKeyDown: (e: KeyboardEvent) => {
      if (ref.current) trapTab(e, ref.current)
    },
    // A click outside the box lands on the dialog element itself (its
    // backdrop).
    onClick: (e: MouseEvent<HTMLDialogElement>) => {
      if (e.target !== e.currentTarget) return
      const r = e.currentTarget.getBoundingClientRect()
      if (e.clientX < r.left || e.clientX > r.right || e.clientY < r.top || e.clientY > r.bottom) close.current()
    },
  }
}

// closing says how a modal goes once open turns false: it plays its exit
// while the dialog is still up, and is put away at once when the browser
// has already closed it (an Escape it wouldn't let be cancelled, with no
// gesture since the last one). Waiting on an exit there would leave it
// shown for good: nothing played, focus not handed back, and the next
// open taken for one still going.
export function closing(open: boolean, dialogOpen: boolean, shown: boolean): 'play' | 'now' | null {
  if (open) return null
  if (dialogOpen) return 'play'
  return shown ? 'now' : null
}

// closeOnce turns a dialog's cancel and close events into one ask per
// Escape. Escape fires cancel, which asks the owner to close (so the exit
// plays). Without a gesture since the last one, the browser won't let
// cancel be stopped and closes the dialog straight after: the close that
// follows is the same Escape and mustn't ask again (Reflect's close steps
// back in history, and twice would leave the app, M8). A close with no
// cancel before it still asks, while the owner thinks the dialog is open.
export function closeOnce(ask: () => void, isOpen: () => boolean) {
  let asked = false
  return {
    cancel(e: Pick<Event, 'preventDefault' | 'defaultPrevented'>) {
      e.preventDefault()
      asked = !e.defaultPrevented
      ask()
    },
    closed() {
      if (asked) {
        asked = false
        return
      }
      if (isOpen()) ask()
    },
  }
}
