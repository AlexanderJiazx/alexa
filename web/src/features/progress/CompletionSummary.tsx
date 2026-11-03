import { useMemo, useState } from 'react'
import type { Plan } from '../../api/progress'
import { OutcomeGlyph } from './glyphs'
import { groupSteps, summarize, type Step } from './group'
import { PlanDone } from './PlanCard'
import type { BlockContext } from './runState'
import { Chev, Dotted, FailedSteps, FaviconStack, StepGroup } from './StepGroup'

// CompletionSummary is what a run's steps fold into once it's over: "Done ·
// 3m 12s · 14 steps on 3 sites" with the sites' icons, or "Stopped after 6
// steps: couldn’t sign in to united.com". A tap opens every step, grouped.
// When the run failed or stopped, its failed steps stay in view while it's
// closed; a step Alexa got past on the way to done is only in the list.
// A run that had a plan opens to the plan's checklist instead, so the plan
// needs no line of its own.
export function CompletionSummary({ steps, ctx, plan }: { steps: Step[]; ctx: BlockContext; plan?: Plan }) {
  const [open, setOpen] = useState(false)
  const groups = useMemo(() => groupSteps(steps), [steps])
  const sum = useMemo(() => summarize(steps, ctx), [steps, ctx])
  const failed = groups.flatMap((g) => g.failed)
  const bad = sum.outcome === 'failed' || sum.outcome === 'stopped'
  return (
    <div className={`steps-log pg-done pg-o-${sum.outcome}`}>
      <button type="button" className="pg-sum" aria-expanded={open} onClick={() => setOpen(!open)}>
        <span className="pg-ic">
          <OutcomeGlyph outcome={sum.outcome} />
        </span>
        <span className="pg-text num">
          <Dotted text={sum.text} />
          {!bad && <FaviconStack hosts={sum.sites} size={14} />}
        </span>
        <Chev open={open} />
      </button>
      {open && plan ? (
        <PlanDone plan={plan} steps={steps} />
      ) : open ? (
        <div className="steps pg-groups">
          {groups.map((g) => (
            <StepGroup key={g.key} g={g} />
          ))}
        </div>
      ) : (
        bad && failed.length > 0 && <FailedSteps steps={failed} />
      )}
    </div>
  )
}
