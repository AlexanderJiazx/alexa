// Progress, run lifecycle and failure contracts (SYNTHESIS Appendix B). The
// server fills these in; the RunCard, the working line and the error card
// render them. Everything here is optional on the wire, so a client keeps
// working against a server that doesn't send it yet.

import type { Source } from './blocks'

// B.1: the plan tool. Each call replaces the whole plan; exactly one step is
// active unless all are terminal.
export type PlanStepStatus = 'pending' | 'active' | 'done' | 'failed' | 'skipped'
export type PlanStep = { text: string /* ≤60 */; status: PlanStepStatus; note?: string /* ≤80 */ }
export type Plan = {
  title?: string // ≤60
  steps: PlanStep[] // 2-8
  note?: string // ≤140
}

// B.2: what the executor records on a step entry (entry.data.step).
export type StepKind = 'search' | 'read' | 'browse' | 'click' | 'type' | 'file' | 'shell' | 'memory' | 'schedule' | 'image' | 'report' | 'other'
export type StepStatus = 'running' | 'done' | 'failed' | 'skipped' | 'interrupted'
export type StepData = {
  tool: string
  kind: StepKind
  status: StepStatus
  host?: string
  url?: string
  target?: string /* element label ≤60 */
  query?: string
  ms?: number
  ok?: boolean
  error?: string /* ≤140, user-safe */
  shot?: string
  plan?: number /* active plan step index */
  attempt?: number
  sources?: Source[] /* ≤10 */
  text?: string /* browser_type: what was typed, ≤60 */
  secret?: boolean /* a login, or text that looked like one: never shown */
}

// B.4: why a run ended badly, stored on the error entry (entry.data.failure)
// and carried by the ended run event.
export type FailureReason =
  | 'model_unavailable'
  | 'rate_limited'
  | 'stalled'
  | 'context_overflow'
  | 'empty_response'
  | 'loop_blocked'
  | 'invalid_input'
  | 'tool_crashed'
  | 'internal'
export type Failure = {
  reason: FailureReason
  retryable: boolean
  retryAt?: number // unix ms, for a countdown
  partial?: boolean // some work was done; show what
  detail?: string // ≤200, user-safe
}

// B.3: the run lifecycle, published as hub events of type "run".
export type RunPhase = 'started' | 'thinking' | 'writing' | 'tool' | 'retrying' | 'compacting' | 'waiting' | 'stopping' | 'ended'
export type RunOutcome = 'done' | 'stopped' | 'failed' | 'suspended'
export type RunTool = { name: string; label: string; kind: StepKind; host?: string }
export type RunRetry = { attempt: number; of: number; reason: string; at: number }

export type RunEvent = {
  type: 'run'
  seq: number
  runId: string
  at: number
  phase: RunPhase
  headline?: string // thinking: reasoning-summary headline ≤60
  target?: 'say' | 'ask' | 'report' // writing
  tool?: RunTool
  retry?: RunRetry
  outcome?: RunOutcome
  failure?: Failure
}

// RunData is a run event as it travels inside the realtime envelope: type
// and seq live on the envelope ({type:"run", seq, epoch, data}). The reducer
// also accepts the flat B.3 shape.
export type RunData = Omit<RunEvent, 'type' | 'seq'> & { type?: 'run'; seq?: number }
