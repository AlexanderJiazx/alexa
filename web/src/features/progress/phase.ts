// The working line's words for each phase of a run (Appendix B.3): what
// Alexa is on (the reasoning headline, else what it's doing), the tool it's
// using and where, and the quieter phases (retrying, tidying up, stopping,
// waiting). A server that sends no run events gets today's activity label.
//
// The plan step it's on is the RunCard's to show: said again here, the same
// words would show twice, a line apart. So the line names the step only when
// it has nothing else.

import type { RunRetry, RunTool } from '../../api/progress'
import type { SessionState } from '../../api/types'
import type { RunInfo } from '../../state/reducer'

export type Writing = 'say' | 'ask' | 'report'

export type Line = {
  headline: string
  // sub is the tool line under it: the site's favicon and name, and what
  // the tool is doing.
  sub?: { host?: string; label?: string }
  // quiet lines (in control, stopping) don't shimmer.
  quiet?: boolean
  // now skips the activity hold: the user should see it at once.
  now?: boolean
  writing?: Writing
  retry?: RunRetry
}

// Why the model call is being retried, in plain words (Failure.reason).
const retryReasons: Record<string, string> = {
  model_unavailable: 'The model isn’t answering',
  rate_limited: 'The model is busy',
  stalled: 'The model went quiet',
  empty_response: 'The model sent nothing back',
  context_overflow: 'The conversation got too long',
  tool_crashed: 'A tool stopped working',
  internal: 'Something went wrong',
}

export function retryReason(reason: string) {
  return retryReasons[reason] ?? (/\s/.test(reason) ? reason : 'Something went wrong')
}

const writingWords: Record<Writing, string> = { say: 'Writing a reply…', ask: 'Writing a question…', report: 'Writing the report…' }

function toolSub(tool: RunTool | undefined, headline: string): Line['sub'] {
  if (!tool) return undefined
  const label = tool.label && tool.label !== headline ? tool.label : undefined
  if (!label && !tool.host) return undefined
  return { host: tool.host, label }
}

// workingLine is what the line under the exchange says while Alexa works.
// planStep is the text of the plan's active step, when there's a plan: the
// RunCard shows it, so it's only the last thing to fall back on.
export function workingLine(session: Pick<SessionState, 'activity' | 'control' | 'phase'>, run: RunInfo | null, planStep?: string): Line {
  if (session.control) return { headline: 'You’re in control', quiet: true, now: true }
  const live = run && run.phase !== 'ended' ? run : null
  const phase = live?.phase ?? session.phase
  const activity = session.activity && session.activity !== 'Thinking' ? session.activity : ''
  switch (phase) {
    case 'stopping':
      return { headline: 'Stopping…', quiet: true, now: true }
    case 'retrying': {
      const r = live?.retry
      return { headline: r ? `Retrying (${r.attempt} of ${r.of})…` : 'Retrying…', sub: r ? { label: retryReason(r.reason) } : undefined, retry: r }
    }
    case 'compacting':
      return { headline: 'Tidying up my notes…' }
    case 'waiting':
      return { headline: live?.tool?.label || activity || 'Waiting…' }
    case 'writing': {
      const t = live?.target
      return { headline: t ? writingWords[t] : 'Writing…', writing: t }
    }
    case 'tool': {
      const headline = live?.headline || live?.tool?.label || activity || planStep || 'Working on it'
      return { headline, sub: toolSub(live?.tool, headline) }
    }
    case 'started':
    case 'thinking':
      return { headline: live?.headline || 'Thinking…' }
  }
  return { headline: activity || planStep || (session.activity === 'Thinking' ? 'Thinking…' : 'Working on it') }
}
