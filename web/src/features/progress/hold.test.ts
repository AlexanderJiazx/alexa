import { describe, expect, it } from 'vitest'
import * as f from './fixtures'
import { createAnnouncer, createHold, type Clock } from './hold'
import { retryReason, workingLine } from './phase'

// fakeClock runs timers by hand: tick(ms) moves time on and fires what's due.
function fakeClock() {
  let t = 0
  let timers: { at: number; fn: () => void; id: number }[] = []
  let ids = 0
  const clock: Clock = {
    now: () => t,
    set: (fn, ms) => {
      const id = ++ids
      timers.push({ at: t + ms, fn, id })
      return id
    },
    clear: (id) => {
      timers = timers.filter((x) => x.id !== id)
    },
  }
  const tick = (ms: number) => {
    const end = t + ms
    for (;;) {
      const due = timers.filter((x) => x.at <= end).sort((a, b) => a.at - b.at)[0]
      if (!due) break
      timers = timers.filter((x) => x !== due)
      t = due.at
      due.fn()
    }
    t = end
  }
  return { clock, tick }
}

describe('createHold', () => {
  it('holds a label for three seconds before the next one shows', () => {
    const { clock, tick } = fakeClock()
    const seen: string[] = []
    const h = createHold('Opening Amazon', 3000, (v) => seen.push(v), clock)
    tick(500)
    h.push('Checking the price')
    h.push('Opening the listing')
    expect(h.get()).toBe('Opening Amazon')
    tick(2400)
    expect(seen).toEqual([])
    tick(100)
    // Only the latest of the waiting labels shows, and only once.
    expect(seen).toEqual(['Opening the listing'])
    tick(5000)
    expect(seen).toEqual(['Opening the listing'])
  })

  it('shows a change at once when the label has been up long enough', () => {
    const { clock, tick } = fakeClock()
    const seen: string[] = []
    const h = createHold('a', 3000, (v) => seen.push(v), clock)
    tick(4000)
    h.push('b')
    expect(seen).toEqual(['b'])
  })

  it('drops a waiting label when the shown one comes back', () => {
    const { clock, tick } = fakeClock()
    const seen: string[] = []
    const h = createHold('a', 3000, (v) => seen.push(v), clock)
    h.push('b')
    h.push('a')
    tick(5000)
    expect(seen).toEqual([])
  })

  it('skips the hold when asked', () => {
    const { clock } = fakeClock()
    const seen: string[] = []
    const h = createHold('a', 3000, (v) => seen.push(v), clock)
    h.push('Stopping…', true)
    expect(seen).toEqual(['Stopping…'])
  })
})

describe('createAnnouncer', () => {
  it('says at most one thing every ten seconds, the latest one', () => {
    const { clock, tick } = fakeClock()
    const said: string[] = []
    const a = createAnnouncer((t) => said.push(t), 10e3, clock)
    a.push('Searching the web')
    tick(1000)
    a.push('Opening Amazon')
    a.push('Checking the price')
    expect(said).toEqual(['Searching the web'])
    tick(8999)
    expect(said).toEqual(['Searching the web'])
    tick(1)
    expect(said).toEqual(['Searching the web', 'Checking the price'])
  })

  it('doesn’t repeat itself, and cancels what’s waiting', () => {
    const { clock, tick } = fakeClock()
    const said: string[] = []
    const a = createAnnouncer((t) => said.push(t), 10e3, clock)
    a.push('Thinking…')
    tick(11e3)
    a.push('Thinking…')
    expect(said).toEqual(['Thinking…'])
    a.push('Writing a reply…')
    // Back and forth inside the gap ends where it began: nothing new to say.
    a.push('Thinking…')
    a.push('Writing a reply…')
    tick(20e3)
    expect(said).toEqual(['Thinking…', 'Writing a reply…'])
    a.push('Stopping…')
    a.push('Done')
    a.cancel()
    tick(20e3)
    expect(said).toEqual(['Thinking…', 'Writing a reply…', 'Stopping…'])
  })
})

describe('workingLine', () => {
  const session = { activity: '', control: false }

  it('reads today’s activity label from a server with no run events', () => {
    expect(workingLine({ ...session, activity: 'Comparing prices on amazon.com' }, null).headline).toBe('Comparing prices on amazon.com')
    expect(workingLine({ ...session, activity: 'Thinking' }, null).headline).toBe('Thinking…')
    expect(workingLine(session, null).headline).toBe('Working on it')
  })

  it('leads with the reasoning headline, never the plan step the RunCard shows', () => {
    expect(workingLine(session, f.runs.thinking, 'Compare prices').headline).toBe('Weighing battery life against price')
    expect(workingLine(session, f.runs.thinkingBare, 'Compare prices').headline).toBe('Thinking…')
    expect(workingLine(session, f.runs.thinkingBare).headline).toBe('Thinking…')
  })

  it('leads with the tool and puts its site under it, not the plan step again', () => {
    // The RunCard says "Check delivery by Friday" with that step's time;
    // the line says what's being done to it, with the run's.
    const l = workingLine(session, f.runs.tool, 'Check delivery by Friday')
    expect(l.headline).toBe('Checking delivery dates')
    expect(l.sub).toEqual({ host: 'bestbuy.com', label: undefined })
    expect(workingLine(session, f.runs.tool)).toEqual(l)
    // With nothing else at all, the step is better than nothing.
    expect(workingLine(session, { ...f.runs.tool, tool: undefined }, 'Check delivery by Friday').headline).toBe('Check delivery by Friday')
  })

  it('names the quieter phases', () => {
    expect(workingLine(session, f.runs.retrying)).toMatchObject({ headline: 'Retrying (2 of 4)…', sub: { label: 'The model is busy' } })
    expect(workingLine(session, f.runs.compacting).headline).toBe('Tidying up my notes…')
    expect(workingLine(session, f.runs.stopping)).toMatchObject({ headline: 'Stopping…', now: true, quiet: true })
    expect(workingLine(session, f.runs.waiting).headline).toBe('Waiting for the page to load')
    expect(workingLine(session, f.runs.writingReport)).toMatchObject({ headline: 'Writing the report…', writing: 'report' })
    expect(workingLine({ ...session, control: true }, f.runs.tool).headline).toBe('You’re in control')
  })

  it('ignores a run that has ended', () => {
    expect(workingLine({ ...session, activity: 'Opening Amazon' }, { ...f.runs.tool, phase: 'ended' }).headline).toBe('Opening Amazon')
  })

  it('says why it’s retrying in plain words', () => {
    expect(retryReason('stalled')).toBe('The model went quiet')
    expect(retryReason('The provider is down')).toBe('The provider is down')
    expect(retryReason('weird_code')).toBe('Something went wrong')
  })
})
