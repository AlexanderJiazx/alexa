import { createContext, useCallback, useContext, useEffect, useId, useRef, useState, type ReactNode } from 'react'
import { createPortal } from 'react-dom'
import { Button } from '../ui/Button'
import { cx } from '../ui/cx'
import { Skeleton } from '../ui/Skeleton'
import { notify } from './toasts'

// The pieces every sheet is built from: a titled section, an inset group of
// rows (the Settings pattern: label on the left, control on the right,
// rows divided by hairlines), and the loading and failure states.

export function Section({ title, aside, className, children }: { title?: string; aside?: ReactNode; className?: string; children: ReactNode }) {
  return (
    <section className={cx('sec', className)}>
      {(title || aside) && (
        <div className="sec-head">
          {title && <h3 className="sec-title">{title}</h3>}
          {aside}
        </div>
      )}
      {children}
    </section>
  )
}

// Group is an inset group. pad gives it room for a form instead of rows.
// id lets Settings' search find it (useSettingId).
export function Group({ pad, tone, id, className, children }: { pad?: boolean; tone?: 'danger'; id?: string; className?: string; children: ReactNode }) {
  return (
    <div id={id} className={cx('group', pad && 'pad', tone, className)}>
      {children}
    </div>
  )
}

// Row is one line of a group: what it is on the left (a label, and a line
// under it), the control on the right.
export function Row({ label, sub, lead, id, children, className }: { label: ReactNode; sub?: ReactNode; lead?: ReactNode; id?: string; children?: ReactNode; className?: string }) {
  return (
    <div id={id} className={cx('row', className)}>
      {lead && <span className="row-lead">{lead}</span>}
      <div className="row-text">
        <div className="row-label">{label}</div>
        {sub && <div className="row-sub">{sub}</div>}
      </div>
      {children && <div className="row-end">{children}</div>}
    </div>
  )
}

// RowSkeletons hold a group's place while it loads.
export function RowSkeletons({ count = 3 }: { count?: number }) {
  return (
    <div className="group" aria-busy="true" aria-label="Loading">
      {Array.from({ length: count }, (_, i) => (
        <div key={i} className="row">
          <div className="row-text skel-text">
            <Skeleton width="58%" height={14} />
            <Skeleton width="34%" height={12} />
          </div>
        </div>
      ))}
    </div>
  )
}

// LoadError is a list that couldn't load, with a way to try again.
export function LoadError({ message, onRetry }: { message: string; onRetry: () => void }) {
  return (
    <div className="load-error" role="alert">
      <p>Couldn’t load this. {message}</p>
      <Button size="sm" onClick={onRetry}>
        Try again
      </Button>
    </div>
  )
}

// useList loads a list for a sheet (FE-09): the data, a failure to show,
// and reload, which also runs whenever a server-side change counter in deps
// moves. Only the latest load lands: a slow older one that answers after a
// newer one is dropped, so it can't put back what the newer one replaced
// (FH-13).
export function useList<T>(load: () => Promise<T[]>, deps: unknown[] = []) {
  const [list, setList] = useState<T[] | null>(null)
  const [error, setError] = useState('')
  const seq = useRef(0)
  const reload = useCallback(() => {
    const n = ++seq.current
    load().then(
      (l) => {
        if (n !== seq.current) return
        setList(l)
        setError('')
      },
      (e: Error) => {
        if (n === seq.current) setError(e.message)
      },
    )
  }, deps)
  useEffect(reload, [reload])
  return { list, error, reload, setList }
}

// failed reports a change that didn't go through.
export const failed = (title: string) => (e: Error) => notify({ title, body: e.message, tone: 'bad' })

// useFirstFocus returns an id for the field that should take the focus as
// it appears (given, or made up). It waits a frame, so a sheet opening with
// it has placed its own focus first.
export function useFirstFocus(on = true, given?: string) {
  const made = useId()
  const id = given ?? made
  useEffect(() => {
    if (!on) return
    const f = requestAnimationFrame(() => document.getElementById(id)?.focus())
    return () => cancelAnimationFrame(f)
  }, [on, id])
  return id
}

// useFocusBack is for the button an inline form takes the place of (Add in
// the head, Reset password in a row). Closing the form removes the field
// that had the focus, which drops the keyboard at the top of the page:
// back(ids) puts it on the first of those buttons that's there once React
// has drawn it, waiting for the list (when the first one added replaces the
// empty state, say). It never takes the focus from somewhere else.
export function useFocusBack(list: unknown) {
  const [want, back] = useState<string[] | null>(null)
  useEffect(() => {
    if (!want) return
    const now = document.activeElement
    if (now && now !== document.body) return back(null)
    const el = want.map((id) => document.getElementById(id)).find(Boolean)
    if (!el) return
    el.focus()
    back(null)
  }, [want, list])
  return back
}

// ---------- in Settings ----------

// SettingScope is what Settings puts before the ids of the rows and fields
// its search can find, so two of it on one page (the gallery) don't share
// an id. useSettingId(key) is the id for one of them; the key is the
// setting's in settingsIndex.ts.
export const SettingScope = createContext('setting-')
export const useSettingId = (key: string) => useContext(SettingScope) + key

// HeadSlot is the place in Settings' head, before Close, where a section
// puts its own buttons (Add, New); HeadActions draws them there.
export const HeadSlot = createContext<HTMLElement | null>(null)
export function HeadActions({ children }: { children: ReactNode }) {
  const slot = useContext(HeadSlot)
  return slot ? createPortal(children, slot) : null
}
