import { describe, expect, it } from 'vitest'
import type { Entry } from '../../api/types'
import type { RunInfo } from '../../state/reducer'
import * as f from './fixtures'
import { toSteps } from './group'
import { blockContext, hiddenByPlan, livePlan, usedBrowser } from './runState'

const ends = (run: Entry[]) => {
  const steps = run.filter((e) => e.kind === 'step')
  return [steps[0].id, steps[steps.length - 1].id] as const
}

describe('blockContext', () => {
  it('reads a run followed by its answer as done, timed from the request', () => {
    const [a, b] = ends(f.doneRun)
    expect(blockContext(f.doneRun, a, b, 'idle', null)).toEqual({ outcome: 'done', reason: undefined, ms: (3 * 60 + 12) * 1e3 })
  })

  it('reads a Stopped note as stopped and an error as failed', () => {
    const [a, b] = ends(f.stoppedRun)
    expect(blockContext(f.stoppedRun, a, b, 'idle', null).outcome).toBe('stopped')
    const [c, d] = ends(f.failedRun)
    const ctx = blockContext(f.failedRun, c, d, 'idle', null)
    expect(ctx.outcome).toBe('failed')
    expect(ctx.reason).toBe('Couldn’t sign in to united.com')
  })

  it('keeps a run open while Alexa is at it, or more steps follow', () => {
    const [a, b] = ends(f.doneRun)
    const working = f.doneRun.slice(0, -1)
    expect(blockContext(working, a, b, 'working', null).outcome).toBe('open')
    const midway = [...f.doneRun.slice(0, 4), f.finalSay, ...f.doneRun.slice(4, -1)]
    const firstHalf = midway.slice(1, 4)
    expect(blockContext(midway.sort((x, y) => x.id - y.id), firstHalf[0].id, firstHalf[2].id, 'idle', null).outcome).toBe('open')
  })

  it('takes the outcome of the run that just ended', () => {
    const [a, b] = ends(f.doneRun)
    const run: RunInfo = { runId: 'r', at: 0, phase: 'ended', outcome: 'failed', failure: { reason: 'stalled', retryable: true, detail: 'The model went quiet' } }
    const steps = f.doneRun.slice(0, -1)
    expect(blockContext(steps, a, b, 'idle', run)).toMatchObject({ outcome: 'failed', reason: 'The model went quiet' })
  })

  it('reads an interrupted step as stopped', () => {
    const steps = f.stoppedRun.slice(0, -1)
    const [a, b] = ends(steps)
    expect(blockContext(steps, a, b, 'idle', null, toSteps(steps)).outcome).toBe('stopped')
  })

  it('ends a run where the next exchange starts', () => {
    const [a, b] = ends(f.stoppedRun)
    const feed = [...f.stoppedRun.slice(0, -1), ...f.failedRun].sort((x, y) => x.id - y.id)
    expect(blockContext(feed, a, b, 'working', null).outcome).toBe('done')
  })
})

describe('the fixture feed', () => {
  it('is in id order, as the reducer keeps it', () => {
    expect(f.feed.map((e) => e.id)).toEqual([...f.feed.map((e) => e.id)].sort((a, b) => a - b))
  })

  it('reads every ended run in it', () => {
    const outcome = (run: Entry[]) => {
      const [a, b] = ends(run)
      return blockContext(f.feed, a, b, 'working', null).outcome
    }
    expect([outcome(f.doneRun), outcome(f.stoppedRun), outcome(f.failedRun)]).toEqual(['done', 'stopped', 'failed'])
    expect(outcome(f.liveExchange)).toBe('open')
  })
})

describe('livePlan', () => {
  it('finds the exchange’s plan and the steps that ran under it', () => {
    const lp = livePlan(f.liveExchange, null)
    expect(lp?.entryId).toBe(f.planEntry.id)
    expect(lp?.from).toBe(f.liveExchange[0].id)
    expect(lp?.steps).toHaveLength(14)
    expect(lp?.steps.filter((s) => s.plan === 2)).toHaveLength(4)
  })

  it('uses the session’s plan only from a server that writes no plan entries', () => {
    const feed = [f.user, ...f.run14]
    expect(livePlan(feed, f.planLive)?.plan).toBe(f.planLive)
    // An older exchange's plan entry says this server writes them.
    expect(livePlan([{ ...f.planDoneEntry, id: 1 }, ...feed], f.planLive)).toBeNull()
  })

  it('has none while a quiet scheduled job works', () => {
    const note: Entry = { ...f.user, id: 9000, kind: 'note', text: 'Scheduled: Price watch', data: { start: true, scheduled: true } }
    const plan: Entry = { ...f.planEntry, id: 9001 }
    expect(livePlan([...f.liveExchange, note, plan], null)).toBeNull()
  })

  it('leaves the plan’s steps to the RunCard', () => {
    const lp = livePlan(f.liveExchange, null)!
    expect(hiddenByPlan(f.liveExchange[2], lp.from)).toBe(true)
    // An earlier exchange's steps, and steps that ran under no plan step, stay.
    expect(hiddenByPlan(f.run14[0], lp.from)).toBe(false)
    expect(hiddenByPlan({ ...f.plainRun[0], id: f.liveExchange[3].id }, lp.from)).toBe(false)
    expect(hiddenByPlan(f.liveExchange[2], 0)).toBe(false)
  })
})

// D9: the working line's eye is for the page Alexa is at work on now.
describe('usedBrowser', () => {
  const entry = (id: number, kind: string, text: string, step?: { tool: string; kind: 'browse' | 'click' | 'search' | 'read' | 'file' }): Entry => ({
    id,
    kind,
    text,
    createdAt: id,
    updatedAt: id,
    data: step ? { step: { ...step, status: 'done' } } : undefined,
  })
  const run = (tool?: { name: string; kind: 'browse' | 'search' | 'read' }, phase: RunInfo['phase'] = 'tool') => ({ runId: 'r', at: 0, phase, tool: tool && { ...tool, label: '' } }) as RunInfo

  it('is false for an exchange that has only thought, searched or read', () => {
    const ex = [entry(1, 'user', 'Find me a lunch spot'), entry(2, 'step', 'Searched the web', { tool: 'web_search', kind: 'search' }), entry(3, 'step', 'Read eater.com', { tool: 'fetch_url', kind: 'read' })]
    expect(usedBrowser(ex, null)).toBe(false)
    expect(usedBrowser(ex, run(undefined, 'thinking'))).toBe(false)
  })

  it('is true once a step was on a page, or a browser tool is under way', () => {
    expect(usedBrowser([entry(1, 'user', 'Book it'), entry(2, 'step', 'Opened opentable.com', { tool: 'browser_open', kind: 'browse' })], null)).toBe(true)
    expect(usedBrowser([entry(1, 'step', 'Read the menu', { tool: 'browser_read', kind: 'read' })], null)).toBe(true)
    expect(usedBrowser([entry(1, 'user', 'Book it')], run({ name: 'browser_click', kind: 'browse' }))).toBe(true)
    // A tool from a run that's over says nothing about now.
    expect(usedBrowser([entry(1, 'user', 'Book it')], run({ name: 'browser_click', kind: 'browse' }, 'ended'))).toBe(false)
  })

  it('reads a step without step data by its words', () => {
    expect(usedBrowser([entry(1, 'step', 'Clicking “Add to cart”')], null)).toBe(true)
    expect(usedBrowser([entry(1, 'step', 'Searching for “burr grinder”')], null)).toBe(false)
  })
})
