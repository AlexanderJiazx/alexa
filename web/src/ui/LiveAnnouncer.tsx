import { useSyncExternalStore } from 'react'

// What screen readers should hear that isn't otherwise on screen as it
// happens (FE-08): "Report ready", "Alexa needs you". Mount <LiveAnnouncer/>
// once; call announce() from anywhere.

// Each region keeps its own count, so a polite message never repeats the
// last assertive one.
type State = { polite: string; assertive: string; pn: number; an: number }
let state: State = { polite: '', assertive: '', pn: 0, an: 0 }
const subs = new Set<() => void>()

export function announce(text: string, opts: { assertive?: boolean } = {}) {
  state = opts.assertive ? { ...state, assertive: text, an: state.an + 1 } : { ...state, polite: text, pn: state.pn + 1 }
  subs.forEach((f) => f())
}

function subscribe(fn: () => void) {
  subs.add(fn)
  return () => {
    subs.delete(fn)
  }
}

export function LiveAnnouncer() {
  const s = useSyncExternalStore(
    subscribe,
    () => state,
    () => state,
  )
  // A fresh node for each message, so the same words twice are read twice.
  return (
    <>
      <div className="sr" role="status" aria-live="polite" aria-atomic="true">
        <span key={s.pn}>{s.polite}</span>
      </div>
      <div className="sr" role="alert" aria-live="assertive" aria-atomic="true">
        <span key={s.an}>{s.assertive}</span>
      </div>
    </>
  )
}
