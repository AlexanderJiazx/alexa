// What the progress views read from the store: the plan of the run in
// progress, and how each run of steps in the feed ended. The functions are
// pure (and cached per feed), so the Log's steps can ask about their
// surroundings without re-rendering when anything else changes.

import type { Plan } from '../../api/progress'
import type { Entry, SessionStatus } from '../../api/types'
import { split, type Split } from '../../state/exchange'
import type { RunInfo } from '../../state/reducer'
import type { AppState } from '../../state/store'
import { toStep, type Outcome, type Step } from './group'

const splits = new WeakMap<Entry[], Split>()

// splitOf is state/exchange.ts's split(), once per feed.
function splitOf(feed: Entry[]): Split {
  let s = splits.get(feed)
  if (!s) {
    s = split(feed)
    splits.set(feed, s)
  }
  return s
}

// currentExchange is state/exchange.ts's current(), once per feed.
export const currentExchange = (feed: Entry[]): Entry[] => splitOf(feed).shown

// usedBrowser says whether the exchange has had Alexa in the browser: a
// browser tool under way, or a step on a page (opened, clicked, typed in).
// A page left open from an earlier exchange isn't what she's doing now.
const browserKinds = new Set<string>(['browse', 'click', 'type'])
const isBrowserTool = (tool?: { name?: string; kind?: string }) => !!tool && (!!tool.name?.startsWith('browser_') || browserKinds.has(tool.kind ?? ''))

export function usedBrowser(exchange: Entry[], run: RunInfo | null): boolean {
  if (run && run.phase !== 'ended' && isBrowserTool(run.tool)) return true
  return exchange.some((e) => {
    if (e.kind !== 'step') return false
    const d = e.data?.step
    return d ? isBrowserTool({ name: d.tool, kind: d.kind }) : browserKinds.has(toStep(e).kind)
  })
}

// LivePlan is the plan of the run in progress, as the RunCard shows it: the
// exchange's plan entry (updated in place), or the session's plan from a
// server that keeps no plan entries.
export type LivePlan = {
  plan: Plan
  entryId?: number
  // updatedAt is when the plan last changed: roughly when its active step
  // started, for a step that has no tool steps yet.
  updatedAt: number
  // from is the first entry of the exchange the plan belongs to.
  from: number
  // steps are the exchange's tool steps that ran under a plan step.
  steps: Step[]
}

let memo: { feed: Entry[]; plan: Plan | null | undefined; idle: boolean; out: LivePlan | null } | null = null

export function selectLivePlan(s: AppState): LivePlan | null {
  const { feed, session } = s.data
  const idle = session.status === 'idle'
  if (memo && memo.feed === feed && memo.plan === session.plan && memo.idle === idle) return memo.out
  memo = { feed, plan: session.plan, idle, out: idle ? null : livePlan(feed, session.plan ?? null) }
  return memo.out
}

export function livePlan(feed: Entry[], sessionPlan: Plan | null): LivePlan | null {
  // A quiet scheduled job's run is its own, off the screen: the plan on
  // screen is an earlier exchange's, over.
  const { shown: ex, job } = splitOf(feed)
  if (job) return null
  const from = ex[0]?.id ?? 0
  const steps = () => ex.filter((e) => e.kind === 'step' && typeof e.data?.step?.plan === 'number').map(toStep)
  for (let i = ex.length - 1; i >= 0; i--) {
    const e = ex[i]
    if (e.kind === 'plan' && e.data?.plan?.steps?.length) return { plan: e.data.plan, entryId: e.id, updatedAt: e.updatedAt, from, steps: steps() }
  }
  // Only trust the session's plan from a server that doesn't write plan
  // entries at all; otherwise it may be an older exchange's.
  if (sessionPlan?.steps?.length && !feed.some((e) => e.kind === 'plan')) return { plan: sessionPlan, updatedAt: 0, from, steps: steps() }
  return null
}

// selectHiddenFrom is the first entry of the exchange whose plan steps the
// RunCard is showing (0 when it isn't): the Log leaves those steps to it.
export const selectHiddenFrom = (s: AppState) => selectLivePlan(s)?.from ?? 0

// hiddenByPlan says whether a step entry is shown in the RunCard instead.
export const hiddenByPlan = (e: Entry, from: number) => from > 0 && e.id >= from && typeof e.data?.step?.plan === 'number'

// BlockContext is what a run of steps needs to know about what came after
// it, for its summary line.
export type BlockContext = { outcome: Outcome; reason?: string; ms?: number }

function indexOf(feed: Entry[], id: number) {
  let lo = 0
  let hi = feed.length - 1
  while (lo <= hi) {
    const mid = (lo + hi) >> 1
    if (feed[mid].id === id) return mid
    if (feed[mid].id < id) lo = mid + 1
    else hi = mid - 1
  }
  return feed.findIndex((e) => e.id === id)
}

// The entries that answer the request: when one comes, the run's work is
// over, and the time to it is how long the run took.
const results = new Set(['say', 'ask', 'report', 'error'])

// blockContext reads how a run of steps (first to last, by id) ended: it's
// open while more steps follow in its exchange or Alexa is still at it,
// stopped when a "Stopped" note follows, failed when an error does, and
// done otherwise. A finished run's time is from the request to the answer.
export function blockContext(feed: Entry[], firstId: number, lastId: number, status: SessionStatus, run: RunInfo | null, steps?: Step[]): BlockContext {
  const i = indexOf(feed, lastId)
  if (i < 0) return { outcome: status === 'idle' ? 'done' : 'open' }
  let outcome: Outcome | null = null
  let reason: string | undefined
  let answer: Entry | undefined
  let latest = true
  for (let j = i + 1; j < feed.length; j++) {
    const e = feed[j]
    if (e.data?.start) {
      latest = false
      break
    }
    if (e.kind === 'step') return { outcome: 'open' }
    if (!answer && results.has(e.kind)) answer = e
    if (e.kind === 'error') {
      outcome = 'failed'
      reason = e.data?.failure?.detail || e.text
      break
    }
    if (e.kind === 'note' && /^stopped\b/i.test(e.text)) {
      outcome = 'stopped'
      break
    }
  }
  if (!outcome) {
    if (latest && status !== 'idle') return { outcome: 'open' }
    const ended = latest && run?.phase === 'ended' ? run.outcome : undefined
    if (ended === 'stopped' || ended === 'failed') {
      outcome = ended
      reason = run?.failure?.detail
    } else if (steps?.some((s) => s.status === 'interrupted')) outcome = 'stopped'
    else outcome = 'done'
  }
  // The run began with the message that started the exchange: the one
  // marked as its start, or (from an older server) the last message.
  const fi = indexOf(feed, firstId)
  let start: number | undefined
  for (let j = fi; j >= 0 && start === undefined; j--) if (feed[j].data?.start) start = feed[j].createdAt
  for (let j = fi; j >= 0 && start === undefined; j--) if (feed[j].kind === 'user') start = feed[j].createdAt
  const end = answer?.createdAt ?? feed[i].updatedAt
  const ms = start !== undefined && end > start ? end - start : undefined
  return { outcome, reason, ms }
}
