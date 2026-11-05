import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import { CompletionSummary } from './CompletionSummary'
import { failedSteps } from './fixtures'
import { toStep } from './group'

// A finished run folds into one line. Its failed steps stay in view only
// when the run itself didn't get there (less to read after a success).
describe('CompletionSummary', () => {
  const steps = failedSteps.map(toStep)
  const failed = steps.find((s) => s.status === 'failed')!
  const html = (outcome: 'done' | 'failed' | 'stopped') => renderToStaticMarkup(<CompletionSummary steps={steps} ctx={{ outcome }} />)

  it('keeps a failed step in view when the run failed or stopped', () => {
    expect(html('failed')).toContain(failed.label)
    expect(html('stopped')).toContain(failed.label)
  })

  it('leaves a step Alexa got past out of a run that ended done', () => {
    expect(html('done')).not.toContain(failed.label)
  })
})
