import { describe, expect, it } from 'vitest'
import type { Entry } from '../../api/types'
import { toRows } from './Log'

const e = (id: number, kind: string): Entry => ({ id, kind, text: kind, createdAt: 1000 + id, updatedAt: 1000 + id })

describe('rows of the exchange', () => {
  it('groups consecutive steps into one block', () => {
    const rows = toRows([e(1, 'user'), e(2, 'step'), e(3, 'step'), e(4, 'say')], false)
    expect(rows.map((r) => r.steps?.map((s) => s.id) ?? r.entry?.kind)).toEqual(['user', [2, 3], 'say'])
  })

  it('puts a plan made after a step at the head of the run, so the run stays one block', () => {
    const rows = toRows([e(1, 'user'), e(2, 'step'), e(3, 'plan'), e(4, 'step'), e(5, 'step'), e(6, 'say')], false)
    expect(rows.map((r) => r.steps?.map((s) => s.id) ?? r.entry?.kind)).toEqual(['user', [2, 4, 5], 'say'])
    expect(rows[1].plan?.id).toBe(3)
  })

  it('puts a plan at the head of the steps after it', () => {
    const rows = toRows([e(1, 'user'), e(2, 'plan'), e(3, 'step'), e(4, 'step'), e(5, 'say')], false)
    expect(rows.map((r) => r.steps?.map((s) => s.id) ?? r.entry?.kind)).toEqual(['user', [3, 4], 'say'])
    expect(rows[1].plan?.id).toBe(2)
  })

  it('gives a second plan in a run a block of its own', () => {
    const rows = toRows([e(1, 'user'), e(2, 'plan'), e(3, 'step'), e(4, 'plan'), e(5, 'step')], false)
    expect(rows.map((r) => [r.plan?.id, r.steps?.map((s) => s.id)])).toEqual([[undefined, undefined], [2, [3]], [4, [5]]])
  })

  it('keeps steps apart when Alexa said something between them', () => {
    const rows = toRows([e(1, 'user'), e(2, 'step'), e(3, 'say'), e(4, 'step')], false)
    expect(rows.map((r) => r.steps?.map((s) => s.id) ?? r.entry?.kind)).toEqual(['user', [2], 'say', [4]])
  })
})
