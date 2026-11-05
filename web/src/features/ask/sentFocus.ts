import { useLayoutEffect, useRef } from 'react'
import { focusComposer } from '../../composer/focus'
import { finePointer } from './keys'

// useSentFocus keeps the keyboard's place when a card's controls give way
// to the line saying what was sent (a form, a multi-select): focus was on
// a control that's gone. On a desktop it goes to the composer, where the
// next thing is typed; on a touch screen to the sent line itself, since
// focusing the composer would raise the keyboard. Call before() as the
// answer goes, while the controls are still there.
//
// The sent line goes too, once the ask is answered and drawn as it was
// asked. Focus on it then moves to the question, which stays, rather than
// falling back to the top of the page and losing a screen reader's place.
export function useSentFocus<T extends HTMLElement>(off: boolean) {
  const box = useRef<T>(null)
  const sent = useRef<HTMLParagraphElement>(null)
  const moving = useRef(false)
  const before = () => {
    moving.current = !!box.current?.contains(document.activeElement)
  }
  useLayoutEffect(() => {
    if (!off || !moving.current) return
    moving.current = false
    if (finePointer() && focusComposer()) return
    sent.current?.focus({ preventScroll: true })
  }, [off])
  useLayoutEffect(() => {
    const el = sent.current
    if (!off || !el) return
    return () => {
      if (document.activeElement === el) keepPlace(el)
    }
  }, [off])
  return { box, sent, before }
}

// keepPlace runs as a focused element is about to go: once it's gone, and
// if focus went nowhere, it goes to the question it answered, in the card
// if that's still there, or as the conversation now shows it.
function keepPlace(el: HTMLElement) {
  const card = el.closest<HTMLElement>('.ask')
  const id = card?.dataset.ask
  requestAnimationFrame(() => {
    const now = document.activeElement
    if (now && now !== document.body) return
    const q = (card?.isConnected ? card.querySelector<HTMLElement>('.q') : null) ?? (id ? document.querySelector<HTMLElement>(`[data-ask-q="${id}"]`) : null)
    if (q) focusQuietly(q)
  })
}

// focusQuietly focuses text that isn't a control, for this once: it can't
// be tabbed to, and a click on it later doesn't take focus.
export function focusQuietly(el: HTMLElement) {
  if (!el.hasAttribute('tabindex')) {
    el.setAttribute('tabindex', '-1')
    el.addEventListener('blur', () => el.removeAttribute('tabindex'), { once: true })
  }
  el.focus({ preventScroll: true })
}
