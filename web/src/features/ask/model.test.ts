import { describe, expect, it } from 'vitest'
import type { Entry } from '../../api/types'
import { askFromEntry, askFromNeeds, askKind, confirmOf, lateText, multiText, optionLayout } from './model'

// The ask model (WP-10): what a card draws from an ask entry or the
// session's needs.

const ask = (data: Entry['data'], text = 'Which one?'): Entry => ({ id: 7, kind: 'ask', text, status: 'open', createdAt: 0, updatedAt: 0, data })

describe('askFromEntry', () => {
  it('keeps a valid recommended option, which needn’t be the first', () => {
    expect(askFromEntry(ask({ options: ['A', 'B', 'C'], recommended: 1 })).recommended).toBe(1)
  })
  it('drops a recommendation that isn’t a valid index', () => {
    for (const r of [3, -1, 1.5, Number.NaN]) expect(askFromEntry(ask({ options: ['A', 'B', 'C'], recommended: r })).recommended).toBeUndefined()
  })
  it('trims options and keeps at most four', () => {
    expect(askFromEntry(ask({ options: [' A ', '', 'B', 'C', 'D', 'E'] })).options).toEqual(['A', 'B', 'C', 'D'])
  })
  it('only lets several options be picked together', () => {
    expect(askFromEntry(ask({ options: ['A'], multi: true })).multi).toBe(false)
    expect(askFromEntry(ask({ options: ['A', 'B'], multi: true })).multi).toBe(true)
  })
  it('never keeps a form made only of secrets', () => {
    const a = askFromEntry(ask({ form: { fields: [{ name: 'password', label: 'Password', type: 'text' }] } }))
    expect(a.form).toBeUndefined()
    expect(a.dropped).toBe(1)
    expect(askKind(a)).toBe('free')
  })
  it('reads the session’s needs the same way', () => {
    const a = askFromNeeds({ kind: 'question', message: 'Which colour?', options: ['Black', 'Silver'], recommended: 1, entryId: 3 })
    expect(a).toMatchObject({ id: 3, question: 'Which colour?', options: ['Black', 'Silver'], recommended: 1 })
  })
})

describe('askKind', () => {
  it('picks the card, confirm first', () => {
    expect(askKind(askFromEntry(ask({ confirm: true, options: ['Yes'], form: { fields: [{ name: 'a', label: 'A', type: 'text' }] } })))).toBe('confirm')
    expect(askKind(askFromEntry(ask({ options: ['A', 'B'], form: { fields: [{ name: 'a', label: 'A', type: 'text' }] } })))).toBe('form')
    expect(askKind(askFromEntry(ask({ options: ['A', 'B'], multi: true })))).toBe('multi')
    expect(askKind(askFromEntry(ask({ options: ['A', 'B'] })))).toBe('choice')
    expect(askKind(askFromEntry(ask(undefined)))).toBe('free')
  })
})

describe('optionLayout', () => {
  it('draws long options as rows', () => {
    expect(optionLayout(['Black', 'Silver'])).toBe('pills')
    expect(optionLayout(['Black', 'Friday, free standard shipping'])).toBe('rows')
  })
})

describe('confirmOf', () => {
  const receipt = { type: 'receipt' as const, status: 'draft' as const, merchant: 'bestbuy.com', lines: [{ label: 'Tax', value: '$24.76' }], total: { amount: 303.76, currency: 'USD' } }
  const callout = { type: 'callout' as const, tone: 'info' as const, text: 'Ships Thursday' }

  it('finds the receipt and leaves the other blocks to BlockList', () => {
    const c = confirmOf(askFromEntry(ask({ confirm: true, options: ['Place order', 'Change something', 'Cancel'], recommended: 0, blocks: [callout, receipt] })))
    expect(c.receipt?.total?.amount).toBe(303.76)
    expect(c.rest).toEqual([callout])
    expect(c.restAt).toEqual([0])
    expect(c.eyebrow).toBe('Confirm purchase')
    expect(c.confirm).toEqual({ label: 'Place order', text: 'Place order' })
    expect(c.change).toEqual({ label: 'Change something', text: 'Change something' })
    expect(c.cancel).toEqual({ label: 'Cancel', text: 'Cancel' })
  })
  it('keeps each other block’s index in the ask, which is what a tap names', () => {
    const cards = { type: 'cards' as const, items: [{ title: 'Thu 9–11' }, { title: 'Fri 9–11' }] }
    const c = confirmOf(askFromEntry(ask({ confirm: true, blocks: [receipt, callout, cards] })))
    expect(c.rest).toEqual([callout, cards])
    expect(c.restAt).toEqual([1, 2])
  })
  it('never lets Cancel or Change send the go-ahead', () => {
    const cases: { options: string[]; recommended?: number; yes: string; change: string; cancel: string }[] = [
      { options: ['Place order, don’t add insurance', 'Change something', 'Cancel'], yes: 'Place order, don’t add insurance', change: 'Change something', cancel: 'Cancel' },
      { options: ['Yes, book it — no insurance', 'Cancel'], recommended: 0, yes: 'Yes, book it — no insurance', change: 'Change something', cancel: 'Cancel' },
      { options: ['Buy the different size', 'Cancel'], yes: 'Buy the different size', change: 'Change something', cancel: 'Cancel' },
      { options: ['No, keep my plan', 'Switch plans'], recommended: 1, yes: 'Switch plans', change: 'Change something', cancel: 'No, keep my plan' },
      { options: ['Cancel', 'Book it'], recommended: 1, yes: 'Book it', change: 'Change something', cancel: 'Cancel' },
    ]
    for (const t of cases) {
      const c = confirmOf(askFromEntry(ask({ confirm: true, options: t.options, recommended: t.recommended })))
      expect([c.confirm.text, c.change.text, c.cancel.text]).toEqual([t.yes, t.change, t.cancel])
      expect(c.change.text).not.toBe(c.confirm.text)
      expect(c.cancel.text).not.toBe(c.confirm.text)
    }
  })
  it('labels each button with the words it sends', () => {
    const c = confirmOf(askFromEntry(ask({ confirm: true, options: ['Book the 7:30', 'Book the 8:00', 'Don’t book'] })))
    for (const b of [c.confirm, c.change, c.cancel]) expect(b.label).toBe(b.text)
    // An option that's neither a change nor a cancel still gets a button.
    expect([c.confirm.text, c.change.text, c.cancel.text]).toEqual(['Book the 7:30', 'Book the 8:00', 'Don’t book'])
  })
  it('sends the model’s own words for each answer', () => {
    const c = confirmOf(askFromEntry(ask({ confirm: true, options: ['Edit the email', 'Don’t send', 'Send it'] })))
    expect(c.confirm.text).toBe('Send it')
    expect(c.change.text).toBe('Edit the email')
    expect(c.cancel.text).toBe('Don’t send')
    expect(c.eyebrow).toBe('Confirm before Alexa goes ahead')
  })
  it('has plain answers when the model offered none', () => {
    const c = confirmOf(askFromEntry(ask({ confirm: true })))
    expect([c.confirm.text, c.change.text, c.cancel.text]).toEqual(['Confirm', 'Change something', 'Cancel. Don’t go ahead.'])
  })
})

describe('answers', () => {
  it('lists multi-select picks in the order offered', () => {
    expect(multiText(['Amazon', 'Best Buy', 'Target', 'B&H'], new Set([3, 0]))).toBe('Amazon, B&H')
  })
  it('says which skipped question a late answer is for', () => {
    expect(lateText('Which colour?', 'Silver')).toBe('About “Which colour?”: Silver')
    const long = lateText('x'.repeat(100), 'Yes')
    expect(long.startsWith(`About “${'x'.repeat(63)}…”`)).toBe(true)
  })
})
