import { useMemo, useState } from 'react'
import type { Plan } from '../../api/progress'
import { stop } from '../../state/actions'
import { useSession } from '../../state/selectors'
import { useApp } from '../../state/store'
import { Button } from '../../ui/Button'
import { cx } from '../../ui/cx'
import { Stop } from '../../ui/icons'
import { Mark } from '../../ui/Mark'
import { useExit } from '../../lib/useExit'
import { Pie } from './glyphs'
import type { Step } from './group'
import { activeIndex, byPlanStep, doneCount, PlanRows } from './PlanCard'
import { Chev } from './StepGroup'
import './progress.css'
import { selectLivePlan, type LivePlan } from './runState'

// RunCard heads the exchange column while Alexa works on a plan: the plan
// with "2 of 4", each step done, under way or to do, the active one with
// its work so far, and Stop. It stays at the top of the
// column as the exchange scrolls under it. Without a plan there's nothing
// to head, and the working line below says what's going on.
export function RunCard() {
  const lp = useApp(selectLivePlan)
  const status = useSession((s) => s.status)
  const { shown, leaving } = useExit<LivePlan>(lp, 200)
  if (!shown) return null
  return <RunCardView plan={shown.plan} steps={shown.steps} needs={status === 'needs'} leaving={leaving} onStop={stop} />
}

type ViewProps = {
  plan: Plan
  steps: Step[]
  needs?: boolean
  leaving?: boolean
  onStop?: () => void
}

const phone = () => typeof matchMedia === 'function' && matchMedia('(max-width: 640px)').matches

// RunCardView is the RunCard without the store, for the gallery.
export function RunCardView({ plan, steps, needs, leaving, onStop }: ViewProps) {
  // A long plan starts folded on a phone, so the exchange keeps the screen.
  const [open, setOpen] = useState(() => !(phone() && plan.steps.length > 4))
  const rows = useMemo(() => byPlanStep(plan, steps), [plan, steps])
  const done = doneCount(plan)
  const total = plan.steps.length
  const active = activeIndex(plan)
  const title = plan.title || 'Plan'
  return (
    <section className={cx('run-card', open ? 'open' : 'folded', leaving && 'leaving')} aria-label={`${title}, ${done} of ${total} done`}>
      {/* The whole head opens and folds the plan: the button lies under
          it, so Stop can sit beside the count. The count and the chevron
          end on the same edges as the times and chevrons below. */}
      <div className="rc-head">
        <button type="button" className="rc-toggle" aria-expanded={open} aria-label={`${title}, ${done} of ${total} done`} onClick={() => setOpen(!open)} />
        <span className="pg-ic" aria-hidden="true">
          <Pie done={done} total={total} />
        </span>
        <span className="rc-title" aria-hidden="true">
          {title}
        </span>
        {needs ? (
          <span className="rc-needs">
            <Mark state="needs" size="sm" />
            Needs you
          </span>
        ) : (
          onStop && (
            <Button size="sm" variant="ghost" className="rc-stop" icon={<Stop size={14} />} onClick={onStop}>
              Stop
            </Button>
          )
        )}
        <span className="rc-count num" aria-hidden="true">
          {done} of {total}
        </span>
        <Chev open={open} />
      </div>
      {(open || active >= 0) && <PlanRows plan={plan} rows={rows} live={!needs} only={open ? undefined : [active]} />}
      {open && plan.note && <p className="rc-note">{plan.note}</p>}
    </section>
  )
}
