import { createStore, type Store } from '../state/store'
import type { ToastTone } from '../ui/Toast'

// The toast queue (WP-12, IX-F22): brief notes at the bottom of the screen
// that can be good news as well as bad, carry one action ("Undo", "Open")
// and be dismissed. Timers pause while the pointer is over the stack, so a
// note is never pulled away from under someone about to press it.

export type ToastAction = { label: string; run: () => void }

// How a note went away: its time ran out, the user dismissed it, or they
// pressed its action.
export type ToastEnd = 'timeout' | 'dismiss' | 'action'

export type Note = {
  id: number
  title: string
  body?: string
  tone: ToastTone
  action?: ToastAction
  // leaving plays the exit before the note is removed.
  leaving?: boolean
}

export type NoteInput = {
  title: string
  body?: string
  tone?: ToastTone
  action?: ToastAction
  // ms is how long the note stays; errors stay a little longer by default,
  // and a note with an action at least actionMs.
  ms?: number
  // onEnd hears how the note went away, once.
  onEnd?: (how: ToastEnd) => void
}

// At most this many notes show; a new one pushes the oldest out.
export const maxNotes = 3
// The exit lasts about 0.6x the enter (--d-1).
export const exitMs = 120
const defaultMs = (tone: ToastTone) => (tone === 'bad' ? 8000 : 5000)
// A note with something to press (Undo) stays at least this long: on a
// touch screen it can't be paused, and someone using a screen reader or a
// switch needs the time to reach it.
export const actionMs = 10000
const lasts = (n: NoteInput, tone: ToastTone) => (n.action ? Math.max(actionMs, n.ms ?? 0) : (n.ms ?? defaultMs(tone)))

type Timer = { left: number; started: number; handle: ReturnType<typeof setTimeout> | null; onEnd?: (how: ToastEnd) => void }

export type Toasts = {
  store: Store<Note[]>
  notify: (n: NoteInput) => number
  dismiss: (id: number, how?: ToastEnd) => void
  // act runs a note's action and puts it away.
  act: (id: number) => void
  pause: () => void
  resume: () => void
}

// createToasts makes a queue. The app uses the one below; tests make their
// own.
export function createToasts(): Toasts {
  const store = createStore<Note[]>([])
  const timers = new Map<number, Timer>()
  let paused = false
  let next = 1

  const arm = (id: number) => {
    const t = timers.get(id)
    if (!t || paused) return
    t.started = Date.now()
    t.handle = setTimeout(() => dismiss(id, 'timeout'), t.left)
  }

  function dismiss(id: number, how: ToastEnd = 'dismiss') {
    const t = timers.get(id)
    if (!t) return
    if (t.handle) clearTimeout(t.handle)
    timers.delete(id)
    // A note that goes while hovered never hears the pointer leave, so an
    // empty stack starts the next note's clock afresh.
    if (timers.size === 0) paused = false
    store.set((list) => list.map((n) => (n.id === id ? { ...n, leaving: true } : n)))
    setTimeout(() => store.set((list) => list.filter((n) => n.id !== id)), exitMs)
    t.onEnd?.(how)
  }

  function notify(n: NoteInput) {
    const tone = n.tone ?? 'neutral'
    // The same plain note again (a retry failing the same way) restarts
    // the one on screen instead of stacking a copy.
    if (!n.action && !n.onEnd) {
      const same = store.get().find((x) => !x.leaving && !x.action && x.title === n.title && x.body === (n.body || undefined) && x.tone === tone)
      const t = same && timers.get(same.id)
      if (same && t && !t.onEnd) {
        if (t.handle) clearTimeout(t.handle)
        t.handle = null
        t.left = lasts(n, tone)
        arm(same.id)
        return same.id
      }
    }
    const id = next++
    const note: Note = { id, title: n.title, body: n.body || undefined, tone, action: n.action }
    timers.set(id, { left: lasts(n, tone), started: 0, handle: null, onEnd: n.onEnd })
    store.set((list) => [...list, note])
    // The oldest still on screen makes way.
    const live = store.get().filter((x) => !x.leaving)
    for (const old of live.slice(0, Math.max(0, live.length - maxNotes))) dismiss(old.id, 'timeout')
    arm(id)
    return id
  }

  return {
    store,
    notify,
    dismiss,
    act(id) {
      const n = store.get().find((x) => x.id === id)
      if (!n || n.leaving) return
      dismiss(id, 'action')
      n.action?.run()
    },
    pause() {
      if (paused) return
      paused = true
      for (const t of timers.values()) {
        if (!t.handle) continue
        clearTimeout(t.handle)
        t.handle = null
        t.left = Math.max(800, t.left - (Date.now() - t.started))
      }
    },
    resume() {
      if (!paused) return
      paused = false
      for (const id of timers.keys()) arm(id)
    },
  }
}

export const toasts = createToasts()

// notify shows a note: notify({title: 'Saved', tone: 'ok'}).
export const notify = toasts.notify
