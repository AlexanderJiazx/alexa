import { useLayoutEffect, type RefObject } from 'react'
import { createStore, useSelector } from '../../state/store'
import type { Ask } from './model'

// Where the needs-you cards are. Each open card in the exchange says
// whether it's on screen, so the dock above the composer shows only when
// the card itself has been scrolled away (or isn't in the exchange at all),
// and the dock's button can bring the card back.

// A card is on screen (true), off it (false), or 'unknown': mounted, and
// the observer hasn't reported on it yet. One that isn't here isn't in the
// exchange.
export type Where = boolean | 'unknown'

const shown = createStore<Record<number, Where>>({})
const cards = new Map<number, HTMLElement>()

function setShown(id: number, on: Where | null) {
  shown.set((s) => {
    if (on === null) {
      if (!(id in s)) return s
      const n = { ...s }
      delete n[id]
      return n
    }
    return s[id] === on ? s : { ...s, [id]: on }
  })
}

// useCardPresence watches a card for the entry id while enabled. The card
// counts as there, place unknown, from before the frame it first shows in,
// so the dock doesn't flash up while the observer's first report is on its
// way.
export function useCardPresence(id: number | undefined, ref: RefObject<HTMLElement | null>, enabled = true) {
  useLayoutEffect(() => {
    const el = ref.current
    if (!enabled || id === undefined || !el) return
    cards.set(id, el)
    // A sliver at the log's edge doesn't count as seeing the card.
    const io =
      typeof IntersectionObserver === 'undefined'
        ? null
        : new IntersectionObserver((es) => setShown(id, es[es.length - 1].isIntersecting), { root: el.closest('.log'), rootMargin: '-40px 0px -40px 0px' })
    setShown(id, io ? 'unknown' : true)
    io?.observe(el)
    return () => {
      io?.disconnect()
      if (cards.get(id) === el) cards.delete(id)
      setShown(id, null)
    }
  }, [id, ref, enabled])
}

// cardShown says whether the dock leaves the card for id to itself: it's on
// screen, or it's there and the observer hasn't said where yet.
export const cardShown = (s: Record<number, Where>, id: number | undefined) => id !== undefined && (s[id] === true || s[id] === 'unknown')

export const useCardShown = (id: number | undefined) => useSelector(shown, (s) => cardShown(s, id))

// revealCard scrolls the card for id into view and puts focus on its first
// control. It says false when there's no card to show (it isn't in the
// exchange, or the exchange is hidden behind a panel).
export function revealCard(id: number | undefined) {
  const el = id === undefined ? undefined : cards.get(id)
  if (!el || !el.isConnected || el.offsetParent === null) return false
  const still = typeof matchMedia !== 'undefined' && matchMedia('(prefers-reduced-motion: reduce)').matches
  el.scrollIntoView({ block: 'nearest', behavior: still ? 'auto' : 'smooth' })
  el.querySelector<HTMLElement>('button:not(:disabled), input, select')?.focus({ preventScroll: true })
  return true
}

// ---------- questions skipped for now ----------

// Skip answers "use your best judgement" so the run carries on, and keeps
// the question here so it can still be answered from Needs you. It lasts
// for this page's life; the exchange decides whether it still shows.
export type Skipped = { id: number; ask: Ask; at: number }

const skippedStore = createStore<Skipped[]>([])

export function markSkipped(ask: Ask) {
  if (ask.id === undefined) return
  const id = ask.id
  skippedStore.set((l) => [...l.filter((s) => s.id !== id), { id, ask, at: Date.now() }])
}

export const unskip = (id: number) => skippedStore.set((l) => (l.some((s) => s.id === id) ? l.filter((s) => s.id !== id) : l))

const all = (l: Skipped[]) => l
export const useSkipped = () => useSelector(skippedStore, all)
export const useIsSkipped = (id: number | undefined) => useSelector(skippedStore, (l) => id !== undefined && l.some((s) => s.id === id))
