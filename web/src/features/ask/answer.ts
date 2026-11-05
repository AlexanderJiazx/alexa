import type { Entry } from '../../api/types'
import { askFromEntry, askKind, confirmOf } from './model'

// How one of Alexa's questions was answered, read back from the
// conversation, so an answered ask looks the same on the main screen and in
// Reflect: the choices it offered with the one picked ticked, and "You
// chose …" (AnsweredAsk). Pure, so it's tested without a DOM.

// Answer is how an ask was answered: the reply, the choice it made (one
// option or confirm button, or a multi-select's picks), and how the reply
// was sent, so a tapped answer is never called the user's own words.
export type Answer = { entry: Entry; option: string | null; picks: string[]; via?: string }

const norm = (s: string) => s.trim().toLowerCase()

// picksIn reads a multi-select answer back into its picks. multiText joins
// them in the order they were offered, so they come off the front in that
// order; anything left over means it wasn't a list of picks after all.
export function picksIn(options: string[], text: string): string[] {
  let rest = text.trim()
  const out: string[] = []
  for (const o of options) {
    if (rest === o) {
      out.push(o)
      rest = ''
      break
    }
    if (rest.startsWith(`${o}, `)) {
      out.push(o)
      rest = rest.slice(o.length + 2)
    }
  }
  return rest === '' ? out : []
}

// answerOf finds how an ask was answered: the next thing the user said in
// the same exchange, and which of the choices offered it was. A confirm
// card's buttons count as choices, under the label they showed.
export function answerOf(entries: Entry[], ask: Entry): Answer | null {
  const i = entries.indexOf(ask)
  if (i < 0) return null
  for (let j = i + 1; j < entries.length; j++) {
    const e = entries[j]
    if (e.kind === 'user') {
      const a = askFromEntry(ask)
      const via = e.data?.via
      const picks = askKind(a) === 'multi' ? picksIn(a.options, e.text) : []
      if (picks.length) return { entry: e, option: null, picks, via }
      let option = a.options.find((o) => norm(o) === norm(e.text)) ?? null
      if (!option && a.confirm) {
        const c = confirmOf(a)
        option = [c.confirm, c.change, c.cancel].find((b) => norm(b.text) === norm(e.text))?.label ?? null
      }
      return { entry: e, option, picks, via }
    }
    if (e.kind === 'ask' || e.kind === 'say') return null
  }
  return null
}

// chosenIn is what an answer picked from the choices offered: a
// multi-select's picks, or the one option.
export const chosenIn = (a: Answer | null | undefined): string[] => (a ? (a.picks.length ? a.picks : a.option ? [a.option] : []) : [])

// Answers is how each ask in a run of entries was answered, and the
// replies that only repeat a choice the ask now shows ticked, which the
// conversation leaves out (as Reflect does).
export type Answers = { of: Map<number, Answer | null>; repeats: Set<number> }

// answersIn works out Answers for a run of entries. Only an ask no longer
// waiting hides a reply: an open question is still a card on the main
// screen, and the reply under it stays as sent.
export function answersIn(entries: Entry[]): Answers {
  const of = new Map<number, Answer | null>()
  const repeats = new Set<number>()
  for (const e of entries) {
    if (e.kind !== 'ask') continue
    const a = answerOf(entries, e)
    of.set(e.id, a)
    if (a && e.status !== 'open' && chosenIn(a).length) repeats.add(a.entry.id)
  }
  return { of, repeats }
}
