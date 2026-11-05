import { useEffect, useRef, type RefObject } from 'react'
import { focusComposer } from '../../composer/focus'
import { store } from '../../state/store'
import { maxOptions, type AskKind } from './model'

// The ask's keyboard: 1 to 4 answer (or toggle, for multi-select), Enter
// picks the recommended option or finishes a multi-select (unless what has
// focus takes Enter itself), and a letter starts a typed answer in the
// composer. Escape is never bound: it must not
// stop the run. A confirm card has no shortcuts, so nothing is bought by a
// stray key; its buttons take Tab and Enter like any button.

export type KeyIn = {
  key: string
  // inField is true when the key goes to a text field, a select or
  // anything editable: typing there is never an answer.
  inField: boolean
  // ownEnter is true when what has focus takes its own Enter: anything
  // but the page and the card itself (enterIsFree).
  ownEnter: boolean
  mod: boolean // Ctrl, Meta or Alt held
  composing: boolean // an IME composition is in progress
}

export type KeyState = { kind: AskKind; count: number; recommended?: number; picked: number }

export type KeyOut = { do: 'pick'; index: number } | { do: 'toggle'; index: number } | { do: 'done' } | { do: 'type' } | null

export function askKey(k: KeyIn, s: KeyState): KeyOut {
  if (k.mod || k.composing || k.inField) return null
  if (s.kind === 'confirm' || s.kind === 'form') return null
  if (/^[1-9]$/.test(k.key)) {
    const i = Number(k.key) - 1
    if (i >= Math.min(s.count, maxOptions)) return null
    return s.kind === 'multi' ? { do: 'toggle', index: i } : { do: 'pick', index: i }
  }
  if (k.key === 'Enter') {
    if (k.ownEnter) return null
    if (s.kind === 'multi') return s.picked > 0 ? { do: 'done' } : null
    if (s.kind === 'choice' && s.recommended !== undefined) return { do: 'pick', index: s.recommended }
    return null
  }
  // / is the shortcut to the composer: it focuses it and doesn't type, as
  // it does when no question is open.
  if (k.key === '/') return null
  // A printable letter starts a typed answer.
  if (k.key.length === 1 && /\S/.test(k.key) && !/^[0-9]$/.test(k.key)) return { do: 'type' }
  return null
}

// enterIsFree says whether Enter is the ask's to take: only while nothing
// in particular has focus, which is the page or the card itself (its
// options take focus as a group when a question arrives). Anything else
// with focus takes its own Enter: a link opens, a button presses, a summary
// opens, a field or a scroller keeps it. Enter on a focused link must never
// answer the question with its recommended option.
export function enterIsFree(focused: unknown, free: readonly unknown[]) {
  return !focused || free.includes(focused)
}

const editable = (t: EventTarget | null) => {
  const el = t as HTMLElement | null
  if (!el || !el.tagName) return false
  return el.isContentEditable || el.tagName === 'TEXTAREA' || el.tagName === 'SELECT' || (el.tagName === 'INPUT' && !['button', 'checkbox', 'radio', 'submit', 'reset'].includes((el as HTMLInputElement).type))
}

// Keys belong to whatever is on top: Reflect, a sheet, the menu or a
// dialog take them from the ask.
function covered(t: HTMLElement | null) {
  const ui = store.get().ui
  return ui.reflect || !!ui.sheet || ui.menu || !!t?.closest?.('dialog, [role=dialog], [role=menu], [aria-modal=true]')
}

// finePointer says whether there's a mouse or trackpad, and so most likely
// a keyboard: touch screens get no key hints.
export const finePointer = () => typeof matchMedia === 'function' && matchMedia('(pointer: fine)').matches

// Only one card answers keys at a time. The live question (what Alexa is
// waiting on, which the dock and the hints point to) wins over a question
// skipped earlier and opened again with Answer now; that one answers keys
// only while focus is inside it. Among equals, the newest card wins.
export type KeyOwner = { late: boolean; holds: boolean }

export function keyOwner<T extends KeyOwner>(cards: T[]): T | undefined {
  for (let i = cards.length - 1; i >= 0; i--) if (cards[i].holds) return cards[i]
  for (let i = cards.length - 1; i >= 0; i--) if (!cards[i].late) return cards[i]
  return undefined
}

type Owner = { late: boolean; card: () => HTMLElement | null }
const owners: Owner[] = []

// useAskKeys listens for the ask's keys while enabled. on gets what
// askKey decided; a letter is sent on to the composer. box is inside the
// card (its options), and says where the card is; late marks a question
// that was skipped.
export function useAskKeys(enabled: boolean, state: KeyState, on: (k: Exclude<KeyOut, null | { do: 'type' }>) => void, opts: { late?: boolean; box?: RefObject<HTMLElement | null> } = {}) {
  const ref = useRef({ state, on })
  ref.current = { state, on }
  const { late = false, box } = opts
  useEffect(() => {
    if (!enabled) return
    const me: Owner = { late, card: () => box?.current?.closest<HTMLElement>('.ask') ?? box?.current ?? null }
    owners.push(me)
    const h = (e: KeyboardEvent) => {
      if (e.defaultPrevented) return
      const t = e.target as HTMLElement | null
      const at = t instanceof Node ? t : null
      const win = keyOwner(owners.map((o) => ({ o, late: o.late, holds: !!at && !!o.card()?.contains(at) })))
      if (win?.o !== me || covered(t)) return
      const free = enterIsFree(at, [document.body, document.documentElement, me.card(), box?.current])
      const out = askKey({ key: e.key, inField: editable(t), ownEnter: !free, mod: e.ctrlKey || e.metaKey || e.altKey, composing: e.isComposing }, ref.current.state)
      if (!out) return
      // Moving focus during keydown sends the letter to the composer.
      if (out.do === 'type') return focusComposer()
      e.preventDefault()
      ref.current.on(out)
    }
    document.addEventListener('keydown', h)
    return () => {
      document.removeEventListener('keydown', h)
      owners.splice(owners.indexOf(me), 1)
    }
  }, [enabled, late, box])
}
