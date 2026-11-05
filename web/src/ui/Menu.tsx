import { useEffect, useRef, type KeyboardEvent, type ReactNode } from 'react'
import { cx } from './cx'
import { moveIndex } from './focus'

type MenuProps = {
  label: string
  // id goes on the menu itself, for its button's aria-controls.
  id?: string
  onClose: () => void
  children: ReactNode
  className?: string
  // leaving plays the exit (pair it with useExit in the owner).
  leaving?: boolean
}

const items = (el: HTMLElement) => Array.from(el.querySelectorAll<HTMLElement>('[role=menuitem]:not([disabled]),[role=menuitemradio]:not([disabled])'))

// Menu is a popover list of actions. It takes focus on its first item;
// arrows, Home and End move through the items; Escape or Tab closes it and
// focus returns to whatever opened it. Clicking outside closes it too.
export function Menu({ id, label, onClose, children, className, leaving }: MenuProps) {
  const ref = useRef<HTMLDivElement>(null)
  useEffect(() => {
    const opener = document.activeElement as HTMLElement | null
    const el = ref.current
    if (el) items(el)[0]?.focus({ preventScroll: true })
    return () => opener?.focus?.({ preventScroll: true })
  }, [])
  const onKeyDown = (e: KeyboardEvent) => {
    const el = ref.current
    if (!el) return
    if (e.key === 'Escape' || e.key === 'Tab') {
      e.preventDefault()
      e.stopPropagation()
      onClose()
      return
    }
    const list = items(el)
    const i = moveIndex(e.key, list.indexOf(document.activeElement as HTMLElement), list.length, 'vertical')
    if (i === null) return
    e.preventDefault()
    list[i]?.focus()
  }
  return (
    <>
      <div className="scrim" onClick={onClose} />
      <div ref={ref} id={id} className={cx('menu', leaving && 'leaving', className)} role="menu" aria-label={label} onKeyDown={onKeyDown}>
        {children}
      </div>
    </>
  )
}

type ItemProps = {
  icon?: ReactNode
  onSelect: () => void
  children: ReactNode
  // hint is a shortcut shown on the right (desktop only); count a number.
  hint?: string
  count?: number
  tone?: 'default' | 'quiet' | 'danger'
  disabled?: boolean
}

export function MenuItem({ icon, onSelect, children, hint, count, tone = 'default', disabled }: ItemProps) {
  return (
    <button type="button" role="menuitem" tabIndex={-1} className={cx('menu-item', tone !== 'default' && tone)} onClick={onSelect} disabled={disabled}>
      {icon}
      <span className="menu-label">{children}</span>
      {count ? <span className="count">{count}</span> : null}
      {hint && <span className="menu-hint">{hint}</span>}
    </button>
  )
}

export function MenuSeparator() {
  return <div className="menu-sep" role="separator" />
}
