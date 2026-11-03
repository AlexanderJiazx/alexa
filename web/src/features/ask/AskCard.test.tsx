import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it, vi } from 'vitest'
import type { Entry } from '../../api/types'
import type { Outgoing } from '../../composer/outbox'
import { AskCard } from './AskCard'

// A late answer to a skipped form question names the question's entry, so
// the server reads it against that question's form, not the one open now
// (FE-1). The form stands in for itself: what matters is what its send
// makes of the values.
const forms = vi.hoisted(() => [] as { send: (text: string, values: Record<string, unknown>) => void }[])
const sent = vi.hoisted(() => [] as Outgoing[])
vi.mock('./AskForm', () => ({
  AskForm: (p: (typeof forms)[number]) => {
    forms.push(p)
    return null
  },
}))
vi.mock('./reply', () => ({
  useReply: () => ({
    busy: false,
    sent: false,
    which: '',
    error: '',
    send: async (m: Outgoing) => sent.push(m) > 0,
    retry() {},
  }),
}))

const when: Entry = {
  id: 7,
  kind: 'ask',
  text: 'When?',
  status: 'answered',
  data: { form: { fields: [{ name: 'date', label: 'Date', type: 'date' }] } },
  createdAt: 1,
  updatedAt: 1,
}

function answer(late: boolean) {
  forms.length = 0
  sent.length = 0
  renderToStaticMarkup(<AskCard entry={late ? when : { ...when, status: 'open' }} late={late} />)
  expect(forms).toHaveLength(1)
  forms[0].send('Sat 14 Nov', { date: '2026-11-14' })
  expect(sent).toHaveLength(1)
  return sent[0]
}

describe('AskCard', () => {
  it('sends a late form answer with the question it answers', () => {
    expect(answer(true)).toEqual({ via: 'form', values: { date: '2026-11-14' }, text: 'About “When?”: Sat 14 Nov', askEntry: 7 })
  })

  it('sends an answer to the open question as it is', () => {
    const m = answer(false)
    expect(m.text).toBe('Sat 14 Nov')
    expect(m.askEntry).toBeUndefined()
  })
})
