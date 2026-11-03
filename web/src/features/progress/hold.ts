// Calm timing for live labels (WP-06): a label that changes is held on
// screen for a few seconds first, so quick steps don't flicker; the working
// line waits a moment before it shows, so a quick reply never flashes it;
// and screen readers hear at most one phase change every ten seconds.
// The cores are plain objects over an injectable clock, so they test
// without a DOM; the hooks wrap them.

import { useEffect, useRef, useState } from 'react'

export type Clock = {
  now: () => number
  set: (fn: () => void, ms: number) => unknown
  clear: (t: unknown) => void
}

export const realClock: Clock = {
  now: () => Date.now(),
  set: (fn, ms) => setTimeout(fn, ms),
  clear: (t) => clearTimeout(t as ReturnType<typeof setTimeout>),
}

export type Hold<T> = { get: () => T; push: (v: T, now?: boolean) => void; dispose: () => void }

// createHold shows each value for at least ms before the next replaces it.
// Values that arrive meanwhile wait, and only the latest is shown; one that
// goes back to what's on screen cancels the wait. now skips the hold (for
// Stop, which must answer at once).
export function createHold<T>(initial: T, ms: number, onChange: (v: T) => void, clock: Clock = realClock): Hold<T> {
  let shown = initial
  let since = clock.now()
  let timer: unknown = null
  const show = (v: T) => {
    timer = null
    shown = v
    since = clock.now()
    onChange(v)
  }
  return {
    get: () => shown,
    push(v, now) {
      if (timer !== null) {
        clock.clear(timer)
        timer = null
      }
      if (Object.is(v, shown)) return
      const wait = now ? 0 : since + ms - clock.now()
      if (wait <= 0) show(v)
      else timer = clock.set(() => show(v), wait)
    },
    dispose() {
      if (timer !== null) clock.clear(timer)
      timer = null
    },
  }
}

// useHold is createHold for a component: it returns the value to show.
export function useHold<T>(value: T, ms = 3000, now = false): T {
  const [shown, setShown] = useState(value)
  const hold = useRef<Hold<T> | null>(null)
  if (!hold.current) hold.current = createHold(value, ms, setShown)
  useEffect(() => {
    hold.current?.push(value, now)
  }, [value, now])
  useEffect(() => () => hold.current?.dispose(), [])
  return shown
}

// useShownAfter is false until ms after since (a unix ms time), then true.
export function useShownAfter(ms: number, since: number) {
  const [shown, setShown] = useState(() => ms <= 0 || Date.now() - since >= ms)
  useEffect(() => {
    if (shown) return
    const t = setTimeout(() => setShown(true), Math.max(0, since + ms - Date.now()))
    return () => clearTimeout(t)
  }, [shown, since, ms])
  return shown
}

export type Announcer = { push: (text: string) => void; cancel: () => void }

// createAnnouncer passes on at most one message every gap ms. A message that
// comes sooner waits for the gap, and only the latest waiting one is said;
// the same words twice in a row are said once.
export function createAnnouncer(say: (text: string) => void, gap = 10e3, clock: Clock = realClock): Announcer {
  let last = -Infinity
  let said = ''
  let waiting: string | null = null
  let timer: unknown = null
  const fire = () => {
    timer = null
    const t = waiting
    waiting = null
    if (t === null || t === said) return
    said = t
    last = clock.now()
    say(t)
  }
  return {
    push(text) {
      if (!text) return
      waiting = text
      if (timer !== null) return
      const wait = last + gap - clock.now()
      if (wait <= 0) fire()
      else timer = clock.set(fire, wait)
    },
    cancel() {
      if (timer !== null) clock.clear(timer)
      timer = null
      waiting = null
    },
  }
}
