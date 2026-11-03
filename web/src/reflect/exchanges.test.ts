import { describe, expect, it } from 'vitest'
import type { Entry } from '../api/types'
import { answerOf, matches, picksIn, quietJob, toExchanges } from './exchanges'

let n = 0
const e = (p: Partial<Entry>): Entry => ({ id: ++n, kind: 'say', text: '', createdAt: n * 1000, updatedAt: n * 1000, ...p })

describe('toExchanges', () => {
  it('splits where Alexa was set to work, and titles each by its request', () => {
    const feed = [e({ kind: 'user', text: 'Find flights to Lisbon\nfor next week', data: { start: true } }), e({ kind: 'step', text: 'Searching' }), e({ text: 'Here are three.' }), e({ kind: 'user', text: 'Thanks' }), e({ kind: 'user', text: 'Weather tomorrow?', data: { start: true } }), e({ text: 'Sunny.' })]
    const ex = toExchanges(feed)
    expect(ex.map((x) => x.title)).toEqual(['Find flights to Lisbon', 'Weather tomorrow?'])
    expect(ex[0].entries).toHaveLength(4)
    expect(ex[0].outcome).toBe('done')
  })

  it('keeps a follow-up under its topic, and the topic ends where the follow-up does', () => {
    const feed = [
      e({ kind: 'user', text: 'Find a hotel in Lisbon', data: { start: true } }),
      e({ text: 'Casa Alfama, €140.' }),
      e({ kind: 'user', text: 'Under €120?', data: { start: true, followUp: true } }),
      e({ kind: 'ask', text: 'Book Pensão Lisboa at €110?', status: 'open' }),
      e({ kind: 'user', text: 'Weather tomorrow?', data: { start: true } }),
      e({ text: 'Sunny.' }),
    ]
    const ex = toExchanges(feed)
    expect(ex.map((x) => x.title)).toEqual(['Find a hotel in Lisbon', 'Weather tomorrow?'])
    expect(ex[0].entries).toHaveLength(4)
    expect(ex[0].outcome).toBe('waiting')
  })

  it('gives a follow-up its own header after a job that had nothing to say', () => {
    const feed = [
      e({ kind: 'user', text: 'Find a hotel in Lisbon', data: { start: true } }),
      e({ text: 'Casa Alfama, €140.' }),
      e({ kind: 'note', text: 'Scheduled: Price watch', data: { scheduled: true, start: true } }),
      e({ kind: 'step', text: 'Checked prices' }),
      e({ kind: 'user', text: 'Under €120?', data: { start: true, followUp: true } }),
      e({ text: 'Pensão Lisboa, €110.' }),
    ]
    expect(toExchanges(feed).map((x) => x.title)).toEqual(['Find a hotel in Lisbon', 'Price watch', 'Under €120?'])
  })

  it('names a scheduled job and says how things ended', () => {
    const feed = [
      e({ kind: 'note', text: 'Scheduled: Morning briefing', data: { scheduled: true, start: true } }),
      e({ kind: 'report', text: 'Briefing', data: { id: 'r1' } }),
      e({ kind: 'user', text: 'Book it', data: { start: true } }),
      e({ kind: 'note', text: 'Stopped' }),
      e({ kind: 'user', text: 'Try again', data: { start: true } }),
      e({ kind: 'error', text: 'The model service returned an error' }),
      e({ kind: 'user', text: 'Which one?', data: { start: true } }),
      e({ kind: 'ask', text: 'Tea or coffee?', status: 'open', data: { options: ['Tea', 'Coffee'] } }),
    ]
    const ex = toExchanges(feed)
    expect(ex[0]).toMatchObject({ title: 'Morning briefing', scheduled: true, reports: 1, outcome: 'done' })
    expect(ex.map((x) => x.outcome)).toEqual(['done', 'stopped', 'failed', 'waiting'])
  })

  it('knows a title that is only the user’s own request', () => {
    const feed = [
      e({ kind: 'note', text: 'Scheduled: Morning briefing', data: { scheduled: true, start: true } }),
      e({ text: 'Here’s your briefing.' }),
      e({ kind: 'user', text: 'Find flights to Lisbon', data: { start: true } }),
      e({ kind: 'note', text: 'Resumed after a restart', data: { start: true } }),
    ]
    expect(toExchanges(feed).map((x) => x.ownWords)).toEqual([false, true, false])
  })

  it('counts a scheduled job’s plan as quiet', () => {
    const note = e({ kind: 'note', text: 'Scheduled: Price watch', data: { scheduled: true, start: true } })
    const plan = e({ kind: 'plan', text: 'Check the price' })
    expect(quietJob([note, plan, e({ kind: 'step' })])).toBe(true)
    expect(quietJob([note, plan, e({ text: 'Still $279.' })])).toBe(false)
    expect(quietJob([e({ kind: 'user', data: { start: true } }), plan])).toBe(false)
    // A follow-up after a quiet job with a plan carries on the topic before it.
    const feed = [e({ kind: 'user', text: 'Watch the price', data: { start: true } }), e({ text: 'On it.' }), note, plan, e({ kind: 'user', text: 'And the other one?', data: { start: true, followUp: true } })]
    expect(toExchanges(feed).map((x) => x.title)).toEqual(['Watch the price', 'Price watch', 'And the other one?'])
  })

  it('falls back to long pauses when nothing is marked', () => {
    const t = 10 * 3600e3
    const feed = [e({ kind: 'user', text: 'One', createdAt: t }), e({ text: 'A', createdAt: t + 1000 }), e({ kind: 'user', text: 'Two', createdAt: t + 2000 }), e({ kind: 'user', text: 'Three', createdAt: t + 3 * 3600e3 })]
    expect(toExchanges(feed).map((x) => x.title)).toEqual(['One', 'Three'])
  })
})

describe('answerOf', () => {
  it('finds the option that was picked', () => {
    const ask = e({ kind: 'ask', text: 'Tea or coffee?', status: 'answered', data: { options: ['Tea', 'Coffee'] } })
    const reply = e({ kind: 'user', text: 'coffee' })
    const got = answerOf([ask, e({ kind: 'step' }), reply], ask)
    expect(got?.option).toBe('Coffee')
    expect(got?.entry).toBe(reply)
  })

  it('knows a typed answer from a picked one, and an ask left unanswered', () => {
    const ask = e({ kind: 'ask', text: 'Tea or coffee?', data: { options: ['Tea', 'Coffee'] } })
    expect(answerOf([ask, e({ kind: 'user', text: 'Water please' })], ask)?.option).toBe(null)
    expect(answerOf([ask, e({ text: 'Never mind.' })], ask)).toBe(null)
  })

  it('reads a multi-select answer back into its picks', () => {
    const ask = e({ kind: 'ask', text: 'Which extras?', data: { options: ['Breakfast', 'Parking', 'Late checkout'], multi: true } })
    const got = answerOf([ask, e({ kind: 'user', text: 'Breakfast, Late checkout', data: { via: 'multi' } })], ask)
    expect(got?.picks).toEqual(['Breakfast', 'Late checkout'])
    expect(got?.option).toBe(null)
    expect(answerOf([ask, e({ kind: 'user', text: 'Breakfast and a cot' })], ask)?.picks).toEqual([])
  })

  it('names the confirm button that was pressed, and keeps how a reply was sent', () => {
    const ask = e({ kind: 'ask', text: 'Order it?', data: { confirm: true } })
    expect(answerOf([ask, e({ kind: 'user', text: 'Confirm' })], ask)?.option).toBe('Confirm')
    expect(answerOf([ask, e({ kind: 'user', text: 'Cancel. Don’t go ahead.' })], ask)?.option).toBe('Cancel')
    const form = e({ kind: 'ask', text: 'Dates?', data: { form: { fields: [{ name: 'from', label: 'From', type: 'date' }] } } })
    const got = answerOf([form, e({ kind: 'user', text: 'From Thu, Nov 12', data: { via: 'form' } })], form)
    expect(got?.via).toBe('form')
    expect(got?.option).toBe(null)
  })
})

describe('picksIn', () => {
  it('keeps the offered order and a comma inside an option', () => {
    expect(picksIn(['Paris, France', 'Rome'], 'Paris, France, Rome')).toEqual(['Paris, France', 'Rome'])
    expect(picksIn(['A', 'B'], 'B')).toEqual(['B'])
    expect(picksIn(['A', 'B'], 'B, A')).toEqual([])
  })
})

describe('matches', () => {
  it('searches the title and what was said, not the steps', () => {
    const [ex] = toExchanges([e({ kind: 'user', text: 'Hotels in Porto', data: { start: true } }), e({ kind: 'step', text: 'booking.com' }), e({ text: 'The Grand is best.' })])
    expect(matches(ex, 'porto')).toBe(true)
    expect(matches(ex, 'GRAND')).toBe(true)
    expect(matches(ex, 'booking')).toBe(false)
    expect(matches(ex, ' ')).toBe(true)
  })
})
