import { describe, expect, it } from 'vitest'
import type { Entry, EntryData, EntryStatus } from '../../api/types'
import { answerOf, answersIn, chosenIn, picksIn } from './answer'

// How an ask was answered, read back from the conversation, so the main
// screen and Reflect show an answered ask the same way (D2).

let id = 0
const e = (kind: string, text: string, data?: EntryData, status?: EntryStatus): Entry => ({ id: ++id, kind, text, data, status, createdAt: id, updatedAt: id })

describe('answerOf', () => {
  it('finds the option picked, past the steps in between', () => {
    const ask = e('ask', 'Which lunch?', { options: ['Chicken Caesar Wrap', 'Mediterranean Bowl'] }, 'answered')
    const reply = e('user', 'Mediterranean Bowl')
    const got = answerOf([ask, e('step', 'Ordering'), reply], ask)
    expect(got?.option).toBe('Mediterranean Bowl')
    expect(got?.entry).toBe(reply)
  })
  it('tells an answer in your own words, and an ask left unanswered', () => {
    const ask = e('ask', 'Tea or coffee?', { options: ['Tea', 'Coffee'] })
    expect(answerOf([ask, e('user', 'Water please')], ask)?.option).toBe(null)
    expect(answerOf([ask, e('say', 'Never mind.')], ask)).toBe(null)
  })
  it('reads a multi-select back into its picks', () => {
    const ask = e('ask', 'Which extras?', { options: ['Breakfast', 'Parking', 'Late checkout'], multi: true })
    expect(answerOf([ask, e('user', 'Breakfast, Late checkout', { via: 'multi' })], ask)?.picks).toEqual(['Breakfast', 'Late checkout'])
    expect(picksIn(['A', 'B'], 'B, A')).toEqual([])
  })
})

describe('chosenIn', () => {
  it('is the picks, the option, or nothing', () => {
    const reply = e('user', 'x')
    expect(chosenIn({ entry: reply, option: null, picks: ['A', 'B'] })).toEqual(['A', 'B'])
    expect(chosenIn({ entry: reply, option: 'Tea', picks: [] })).toEqual(['Tea'])
    expect(chosenIn({ entry: reply, option: null, picks: [] })).toEqual([])
    expect(chosenIn(null)).toEqual([])
  })
})

describe('answersIn', () => {
  it('hides a reply that only repeats the choice the answered ask shows', () => {
    const ask = e('ask', 'Which lunch?', { options: ['Wrap', 'Bowl'] }, 'answered')
    const reply = e('user', 'Bowl')
    const got = answersIn([e('user', 'Order lunch'), ask, reply, e('say', 'Ordered.')])
    expect(got.of.get(ask.id)?.option).toBe('Bowl')
    expect([...got.repeats]).toEqual([reply.id])
  })
  it('keeps a reply in your own words, a form and a skip', () => {
    const ask = e('ask', 'Which lunch?', { options: ['Wrap', 'Bowl'] }, 'answered')
    const form = e('ask', 'Where to?', { form: { fields: [{ name: 'city', label: 'City', type: 'text' }] } }, 'answered')
    const got = answersIn([ask, e('user', 'Something lighter'), form, e('user', 'City: Porto', { via: 'form' })])
    expect(got.repeats.size).toBe(0)
    expect(got.of.get(form.id)?.via).toBe('form')
  })
  it('leaves the reply under a question still open', () => {
    const ask = e('ask', 'Which lunch?', { options: ['Wrap', 'Bowl'] }, 'open')
    const got = answersIn([ask, e('user', 'Bowl')])
    expect(got.repeats.size).toBe(0)
  })
  it('knows an ask that went unanswered', () => {
    const ask = e('ask', 'Which lunch?', { options: ['Wrap', 'Bowl'] }, 'answered')
    expect(answersIn([ask, e('note', 'Stopped')]).of.get(ask.id)).toBe(null)
  })
})
