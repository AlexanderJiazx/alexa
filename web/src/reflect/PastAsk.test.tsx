import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import type { Entry } from '../api/types'
import { answerOf } from './exchanges'
import { PastAsk } from './PastAsk'

// Reflect draws a past question with the main screen's AnsweredAsk, so the
// two can't drift apart: the same choices, the same tick, the same note.

let n = 0
const e = (kind: Entry['kind'], text: string, data?: Entry['data'], status?: Entry['status']): Entry => ({ id: ++n, kind, text, createdAt: n * 1000, updatedAt: n * 1000, data, status })

describe('PastAsk', () => {
  it('draws an answered question as the main screen does', () => {
    const ask = e('ask', 'Which table?', { options: ['Window', 'Bar'] }, 'answered')
    const out = renderToStaticMarkup(<PastAsk ask={ask} answer={answerOf([ask, e('user', 'Window')], ask)} />)
    expect(out).toContain('class="ask-past"')
    expect(out).toContain('<ul class="ask-was" aria-label="The choices offered">')
    expect(out).toMatch(/<li class="chosen" aria-current="true">.*Window<\/li>/)
    expect(out).toContain('You chose <b>Window</b>')
    expect(out).not.toContain('past-')
  })

  it('says a question went unanswered', () => {
    const ask = e('ask', 'Which table?', { options: ['Window', 'Bar'] }, 'done')
    const out = renderToStaticMarkup(<PastAsk ask={ask} answer={null} />)
    expect(out).toContain('<p class="ask-unanswered">Not answered</p>')
  })
})
