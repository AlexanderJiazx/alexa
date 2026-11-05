// The progress views' own glyphs: a plan step's status, a step that didn't
// go as planned, and the plan's progress as a pie. They're drawn on a 16px
// grid (not ui/icons.tsx's 24px one) because they're shown at 16px, where
// a scaled-down 1.5 stroke would turn to fuzz. Colours come from CSS
// (progress.css), through currentColor. Their modifiers are prefixed
// (pg-g-done) because other features style bare .stopped and .open.

import type { ReactNode } from 'react'
import type { PlanStepStatus, StepStatus } from '../../api/progress'
import type { Outcome } from './group'

const G = ({ className, children }: { className: string; children: ReactNode }) => (
  <svg className={`i pg-g pg-g-${className}`} width="16" height="16" viewBox="0 0 16 16" aria-hidden="true">
    {children}
  </svg>
)

const ring = <circle cx="8" cy="8" r="6.25" />

// PlanGlyph marks a plan step: an open ring to do, a ring with a dot while
// it's under way, a filled tick when done, a cross when it failed and a
// slash when it was skipped.
export function PlanGlyph({ status }: { status: PlanStepStatus }) {
  switch (status) {
    case 'done':
      return (
        <G className="done">
          <circle className="fill" cx="8" cy="8" r="7" />
          <path className="knock" d="M5.1 8.2l1.9 1.9 3.9-4.1" />
        </G>
      )
    case 'active':
      return (
        <G className="active">
          {ring}
          <circle className="core" cx="8" cy="8" r="2.75" />
        </G>
      )
    case 'failed':
      return <FailedGlyph />
    case 'skipped':
      return <SkippedGlyph />
    default:
      return <G className="pending">{ring}</G>
  }
}

export const FailedGlyph = () => (
  <G className="failed">
    {ring}
    <path d="M5.9 5.9l4.2 4.2M10.1 5.9l-4.2 4.2" />
  </G>
)

export const SkippedGlyph = () => (
  <G className="skipped">
    {ring}
    <path d="M5.4 10.6l5.2-5.2" />
  </G>
)

// InterruptedGlyph is a step that Stop cut short: a ring with a bar.
export const InterruptedGlyph = () => (
  <G className="interrupted">
    {ring}
    <path d="M5.25 8h5.5" />
  </G>
)

// StepStatusGlyph is the glyph for a step that didn't simply finish, or
// null for one that did (it shows its kind instead).
export function StepStatusGlyph({ status }: { status: StepStatus }) {
  if (status === 'failed') return <FailedGlyph />
  if (status === 'skipped') return <SkippedGlyph />
  if (status === 'interrupted') return <InterruptedGlyph />
  return null
}

// OutcomeGlyph leads a finished run's summary.
export function OutcomeGlyph({ outcome }: { outcome: Outcome }) {
  if (outcome === 'failed')
    return (
      <G className="failed">
        {ring}
        <path d="M8 4.9v3.6M8 11.1v.01" />
      </G>
    )
  if (outcome === 'stopped')
    return (
      <G className="stopped">
        {ring}
        <rect className="fill" x="5.6" y="5.6" width="4.8" height="4.8" rx="1" />
      </G>
    )
  if (outcome === 'open')
    return (
      <G className="open">
        <circle className="fill" cx="8" cy="8" r="2" />
      </G>
    )
  return (
    <G className="ok">
      <path d="M3.6 8.4l2.9 2.9 5.9-6.3" />
    </G>
  )
}

// Pie shows how much of the plan is done: a ring that fills clockwise.
// Complete, it's the done tick.
export function Pie({ done, total }: { done: number; total: number }) {
  const f = total ? Math.min(1, done / total) : 0
  if (f >= 1) return <PlanGlyph status="done" />
  // A circle of radius 2.25 stroked 4.5 wide paints a pie a hair inside the
  // ring; its dash is the share done.
  const c = 2 * Math.PI * 2.25
  return (
    <G className="pie">
      {ring}
      {f > 0 && <circle className="wedge" cx="8" cy="8" r="2.25" strokeDasharray={`${(c * f).toFixed(2)} ${c.toFixed(2)}`} transform="rotate(-90 8 8)" />}
    </G>
  )
}

// StopSquare is Stop on its own, for a phone's working line: a filled
// square reads as "stop" without its word, and can't be taken for a plan
// step's ring. It's drawn on ui/icons.tsx's 24px grid, as it's shown at
// that size.
export const StopSquare = ({ size = 20 }: { size?: number }) => (
  <svg className="i pg-stop-glyph" width={size} height={size} viewBox="0 0 24 24" aria-hidden="true">
    <rect className="fill" x="7" y="7" width="10" height="10" rx="2.25" />
  </svg>
)
