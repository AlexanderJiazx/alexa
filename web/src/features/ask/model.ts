import type { AskForm, Block, BlockOf } from '../../api/blocks'
import type { Entry, Needs } from '../../api/types'
import { safeFields } from './form'

// Ask is what Alexa is asking, the same whether it comes from the ask entry
// in the exchange or from the session's needs (the dock and the caption
// under a panel), so every place draws it from one shape.
export type Ask = {
  id?: number // the ask entry, when known
  question: string
  options: string[]
  recommended?: number // a valid index into options, or nothing
  multi: boolean
  confirm: boolean
  form?: AskForm
  blocks?: Block[]
  // dropped counts form fields the client refused to draw (credentials).
  dropped: number
}

// The contract allows four options; the keys answer them 1 to 4.
export const maxOptions = 4

function make(id: number | undefined, question: string, d: { options?: string[]; recommended?: number; multi?: boolean; confirm?: boolean; form?: AskForm; blocks?: Block[] }): Ask {
  const options = (d.options ?? []).map((o) => o.trim()).filter(Boolean).slice(0, maxOptions)
  const r = d.recommended
  const recommended = typeof r === 'number' && Number.isInteger(r) && r >= 0 && r < options.length ? r : undefined
  const { fields, dropped } = safeFields(d.form?.fields)
  const form = fields.length ? { submit: d.form?.submit, fields } : undefined
  return { id, question, options, recommended, multi: !!d.multi && options.length > 1, confirm: !!d.confirm, form, blocks: d.blocks, dropped }
}

export const askFromEntry = (e: Entry): Ask => make(e.id, e.text, e.data ?? {})
export const askFromNeeds = (n: Needs): Ask => make(n.entryId, n.message, n)

// AskKind picks the card: a confirm card, a small form, multi-select chips,
// a single choice, or a question answered in the composer.
export type AskKind = 'confirm' | 'form' | 'multi' | 'choice' | 'free'

export function askKind(a: Ask): AskKind {
  if (a.confirm) return 'confirm'
  if (a.form) return 'form'
  if (a.multi) return 'multi'
  if (a.options.length) return 'choice'
  return 'free'
}

// Options longer than this read better as full-width rows than as pills.
export const longOption = 18

export const optionLayout = (options: string[]): 'pills' | 'rows' => (options.some((o) => o.length > longOption) ? 'rows' : 'pills')

// ---------- the confirm card ----------

export type Receipt = BlockOf<'receipt'>
export type Choice = { label: string; text: string }

export type Confirm = {
  receipt?: Receipt
  rest: Block[] // the other context blocks, drawn by BlockList
  // restAt is each of rest's index in the ask's blocks, which is what a
  // tap on a card names to the server.
  restAt: number[]
  eyebrow: string
  confirm: Choice
  change: Choice
  cancel: Choice
}

// An answer is a cancel or a change by how it starts, so a go-ahead that
// mentions "don't add insurance" or "a different size" is neither.
const cancelRe = /^(cancel|no|nope|don[’']?t|do not|stop|never ?mind|not now|abort)\b/i
const changeRe = /^(change|edit|adjust|modify|different|something else|let me (change|edit))\b/i

const own = (text: string): Choice => ({ label: text, text })

// confirmOf works out the confirm card from an ask: the receipt it shows and
// the three answers. Each answer is a visible message in the model's own
// words where it offered them, and its button says exactly what it sends.
// The go-ahead is the option Alexa recommends (or the first one that isn't
// a cancel or a change) and is never taken for Cancel or Change; Cancel is
// the last option that starts as one; the secondary button is a change, or
// else the option left over, so nothing offered goes missing.
export function confirmOf(a: Ask): Confirm {
  const blocks = a.blocks ?? []
  const receipt = blocks.find((b): b is Receipt => b.type === 'receipt')
  const restAt = blocks.flatMap((b, i) => (b === receipt ? [] : [i]))
  const rest = restAt.map((i) => blocks[i])
  const opts = a.options
  const yes = a.recommended ?? opts.findIndex((o) => !cancelRe.test(o) && !changeRe.test(o))
  const others = opts.map((_, i) => i).filter((i) => i !== yes)
  const cancel = others.filter((i) => cancelRe.test(opts[i])).pop()
  const left = others.filter((i) => i !== cancel)
  const change = left.find((i) => changeRe.test(opts[i])) ?? left[0]
  const eyebrow = receipt ? 'Confirm purchase' : 'Confirm before Alexa goes ahead'
  return {
    receipt,
    rest,
    restAt,
    eyebrow,
    confirm: yes >= 0 ? own(opts[yes]) : own('Confirm'),
    change: change !== undefined ? own(opts[change]) : { label: 'Change', text: 'Change something' },
    cancel: cancel !== undefined ? own(opts[cancel]) : { label: 'Cancel', text: 'Cancel. Don’t go ahead.' },
  }
}

// ---------- answers that aren't a plain option ----------

// The visible message Skip sends: the run carries on without the answer,
// and the question stays in Needs you to answer later.
export const skipText = 'Skip this one for now. Use your best judgement.'

// lateText is an answer to a question that was skipped: it says which
// question it answers, since the run has moved on.
export function lateText(question: string, answer: string) {
  const q = question.length > 64 ? `${question.slice(0, 63).trimEnd()}…` : question
  return `About “${q}”: ${answer}`
}

// multiText is a multi-select answer: the picks in the order offered,
// "Amazon, B&H".
export const multiText = (options: string[], picked: Set<number>) =>
  options.filter((_, i) => picked.has(i)).join(', ')
