import { useMemo, useState } from 'react'
import type { Entry } from '../../api/types'
import { shallow, useApp } from '../../state/store'
import type { EntryMode } from '../conversation/registry'
import { CompletionSummary } from './CompletionSummary'
import { earlierLabel, foldGroups, groupSteps, toSteps } from './group'
import './progress.css'
import { PlanRecord } from './PlanCard'
import { blockContext, hiddenByPlan, selectHiddenFrom, selectLivePlan, type BlockContext } from './runState'
import { FailedSteps, StepGroup } from './StepGroup'

// StepsBlock shows one run of steps in the conversation. While Alexa works
// it's the steps so far, grouped (the step under way is the working line
// below); once the run is over, they fold into its summary. Steps that ran
// under a plan step are the RunCard's while it's showing. In history
// (Reflect) every step shows; without a mode, an open Reflect counts as
// history. plan is the plan entry that heads the run, if it made one: the
// RunCard's too while the run is on the main screen.
export function StepsBlock({ steps, live, mode, plan }: { steps: Entry[]; live: boolean; mode?: EntryMode; plan?: Entry }) {
  const hiddenFrom = useApp((s) => (mode === 'history' || (!mode && s.ui.reflect) ? 0 : selectHiddenFrom(s)))
  const onCard = useApp((s) => mode === 'now' && !!plan && selectLivePlan(s)?.entryId === plan.id)
  const first = steps[0]?.id ?? 0
  const last = steps[steps.length - 1]?.id ?? 0
  const ctx = useApp((s) => blockContext(s.data.feed, first, last, s.data.session.status, s.data.run), shallow)
  const shown = useMemo(() => (hiddenFrom ? steps.filter((e) => !hiddenByPlan(e, hiddenFrom)) : steps), [steps, hiddenFrom])
  const head = onCard ? undefined : plan
  if (!shown.length && !head) return null
  return <StepsView steps={shown} live={live} ctx={ctx} plan={head} />
}

// StepsView is StepsBlock without the store: the gallery draws it from
// fixtures. A finished run folds into its summary, plan and all, so it's
// one line; otherwise the plan, if any, is a line of its own above the
// steps so far.
export function StepsView({ steps, live, ctx, plan }: { steps: Entry[]; live: boolean; ctx: BlockContext; plan?: Entry }) {
  const all = useMemo(() => toSteps(steps), [steps])
  // An interrupted step means the run was stopped, whatever came after.
  const context = useMemo<BlockContext>(() => (ctx.outcome === 'done' && all.some((s) => s.status === 'interrupted') ? { ...ctx, outcome: 'stopped' } : ctx), [ctx, all])
  const p = plan?.data?.plan?.steps?.length ? plan.data.plan : undefined
  // A single line of work between two things Alexa said needs no summary.
  const folds = !live && all.length > 0 && !(context.outcome === 'open' && groupSteps(all).length === 1)
  if (folds) return <CompletionSummary steps={all} ctx={context} plan={p} />
  return (
    <>
      {p && plan && <PlanRecord plan={p} title={p.title || plan.text || 'Plan'} steps={all} />}
      <LiveSteps steps={steps} />
    </>
  )
}

// LiveSteps lists the groups so far: the last four, the older ones folded
// into "N earlier steps" (their failures still in view).
function LiveSteps({ steps }: { steps: Entry[] }) {
  const [all, setAll] = useState(false)
  const groups = useMemo(() => groupSteps(toSteps(steps).filter((s) => s.status !== 'running')), [steps])
  if (!groups.length) return null
  const fold = all ? { shown: groups, hidden: 0, failed: [] } : foldGroups(groups)
  return (
    <div className="steps-log pg-live">
      {fold.hidden > 0 && (
        <button type="button" className="pg-earlier" onClick={() => setAll(true)}>
          <span className="pg-ic" aria-hidden="true">
            <span className="pg-dots" />
          </span>
          {earlierLabel(fold.hidden)}
        </button>
      )}
      {fold.failed.length > 0 && <FailedSteps steps={fold.failed} />}
      {fold.shown.map((g) => (
        <StepGroup key={g.key} g={g} />
      ))}
    </div>
  )
}
