import { useMemo, useState } from 'react'
import type { Plan, PlanStep } from '../../api/progress'
import { cx } from '../../ui/cx'
import { PlanGlyph, Pie } from './glyphs'
import { earlierLabel, foldGroups, groupSteps, sitesOf, spanText, type Step } from './group'
import { Chev, Dotted, FailedSteps, FaviconStack, Sep, StepGroup } from './StepGroup'

// byPlanStep sorts a run's tool steps under the plan step each ran under.
export function byPlanStep(plan: Plan, steps: Step[]): Step[][] {
  const rows: Step[][] = plan.steps.map(() => [])
  for (const s of steps) if (s.plan !== undefined && rows[s.plan]) rows[s.plan].push(s)
  return rows
}

export const doneCount = (plan: Plan) => plan.steps.filter((s) => s.status === 'done').length

// activeIndex is the plan step under way, or -1 when every step is over.
export const activeIndex = (plan: Plan) => plan.steps.findIndex((s) => s.status === 'active')

type RowsProps = {
  plan: Plan
  rows: Step[][]
  // live is the run in progress: the active step shows its work so far. It
  // has no clock of its own: the working line keeps the run's one ticking
  // time, and a step gets its time once it's over.
  live?: boolean
  // only shows just these rows (the RunCard, folded, shows the active one).
  only?: number[]
}

// PlanRows is the plan as a checklist: done, under way, to do, failed or
// skipped, each finished one with the sites its work went to and how long it
// took. The active step shows its work so far; a finished one opens to it.
export function PlanRows({ plan, rows, live, only }: RowsProps) {
  const active = activeIndex(plan)
  return (
    <ol className="pc-rows">
      {plan.steps.map((st, i) =>
        only && !only.includes(i) ? null : <PlanRow key={i} step={st} steps={rows[i] ?? []} live={!!live && i === active} />,
      )}
    </ol>
  )
}

function PlanRow({ step, steps, live }: { step: PlanStep; steps: Step[]; live: boolean }) {
  const [open, setOpen] = useState(false)
  const groups = useMemo(() => groupSteps(steps.filter((s) => s.status !== 'running')), [steps])
  const sites = useMemo(() => sitesOf(steps), [steps])
  const time = live ? '' : steps.length ? spanText(steps[0].start, Math.max(...steps.map((s) => s.end))) : ''
  // The active step shows its last few lines of work; any other opens to
  // all of them.
  const can = !live && groups.length > 0
  const fold = live ? foldGroups(groups, 3) : null
  const main = (
    <>
      <span className="pg-ic">
        <PlanGlyph status={step.status} />
      </span>
      <span className="pc-text">
        {step.text}
        {step.status === 'skipped' && (
          <span className="pg-tag">
            <Sep />
            Skipped
          </span>
        )}
      </span>
      <span className="pc-meta">
        {!live && <FaviconStack hosts={sites} size={14} />}
        {time && <span className="pc-time num">{time}</span>}
      </span>
      <Chev open={open} on={can} />
    </>
  )
  return (
    <li className={cx('pc-row', step.status, live && 'live')}>
      {can ? (
        <button type="button" className="pc-main" aria-expanded={open} onClick={() => setOpen(!open)}>
          {main}
        </button>
      ) : (
        <div className="pc-main">{main}</div>
      )}
      {step.note && <span className={cx('pc-note', step.status === 'failed' && 'bad')}>{step.note}</span>}
      {fold && (fold.shown.length > 0 || fold.failed.length > 0) && (
        <div className="pc-work">
          {fold.hidden > 0 && <span className="pc-earlier">{earlierLabel(fold.hidden)}</span>}
          {fold.failed.length > 0 && <FailedSteps steps={fold.failed} />}
          {fold.shown.map((g) => (
            <StepGroup key={g.key} g={g} tight />
          ))}
        </div>
      )}
      {open && (
        <div className="pc-work">
          {groups.map((g) => (
            <StepGroup key={g.key} g={g} tight />
          ))}
        </div>
      )}
    </li>
  )
}

// PlanDone is a finished run's plan, opened from its summary: the steps
// Alexa took before she made the plan, then the checklist with the work
// under each of its steps.
export function PlanDone({ plan, steps }: { plan: Plan; steps: Step[] }) {
  const rows = useMemo(() => byPlanStep(plan, steps), [plan, steps])
  const before = useMemo(() => groupSteps(steps.filter((s) => s.plan === undefined || !plan.steps[s.plan])), [plan, steps])
  return (
    <>
      {before.length > 0 && (
        <div className="steps pg-groups">
          {before.map((g) => (
            <StepGroup key={g.key} g={g} />
          ))}
        </div>
      )}
      <PlanRows plan={plan} rows={rows} />
    </>
  )
}

// PlanRecord is a plan with no finished run to fold into (its run still
// open, or none in view): one quiet line, "Find headphones · 4 of 4", that
// opens to the checklist.
export function PlanRecord({ plan, title, steps }: { plan: Plan; title: string; steps: Step[] }) {
  const [open, setOpen] = useState(false)
  const rows = useMemo(() => byPlanStep(plan, steps), [plan, steps])
  const done = doneCount(plan)
  const failed = plan.steps.filter((s) => s.status === 'failed').length
  const skipped = plan.steps.filter((s) => s.status === 'skipped').length
  const words = [title, `${done} of ${plan.steps.length}`, failed ? `${failed} failed` : '', skipped ? `${skipped} skipped` : ''].filter(Boolean).join(' · ')
  return (
    <div className={cx('steps-log pg-plan', open && 'open')}>
      <button type="button" className="pg-sum" aria-expanded={open} onClick={() => setOpen(!open)}>
        <span className="pg-ic">
          <Pie done={done} total={plan.steps.length} />
        </span>
        <span className="pg-text num">
          <Dotted text={words} />
        </span>
        <Chev open={open} />
      </button>
      {open && <PlanRows plan={plan} rows={rows} />}
    </div>
  )
}
