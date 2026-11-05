import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import { planDoneEntry, run14 } from './fixtures'
import { StepsView } from './StepsBlock'

// A run and the plan at its head are one line once the run is over: the
// summary says how it went, and opens to the plan's checklist.
describe('StepsView with a plan', () => {
  const html = (live: boolean, outcome: 'done' | 'open') => renderToStaticMarkup(<StepsView steps={run14} live={live} ctx={{ outcome }} plan={planDoneEntry} />)

  it('folds a finished run and its plan into the summary', () => {
    const out = html(false, 'done')
    expect(out).toContain('pg-done')
    expect(out).not.toContain('pg-plan')
    expect(out.match(/class="pg-sum"/g)).toHaveLength(1)
  })

  it('keeps the plan as its own line while the run is going', () => {
    const out = html(true, 'open')
    expect(out).toContain('pg-plan')
    expect(out).not.toContain('pg-done')
  })

  it('shows a plan with no steps on its own', () => {
    const out = renderToStaticMarkup(<StepsView steps={[]} live={false} ctx={{ outcome: 'done' }} plan={planDoneEntry} />)
    expect(out).toContain('pg-plan')
  })
})
