import { describe, expect, it } from 'vitest'
import type { Entry, EntryData, Report, Schedule } from '../../api/types'
import { greeting, nextRun, preview, recentWork, status, when } from './home'

const at = (h: number, m = 0, dayOffset = 0) => new Date(2026, 9, 30 + dayOffset, h, m).getTime()
const sched = (id: string, nextRun: number | undefined, enabled = true): Schedule => ({ id, name: `Job ${id}`, prompt: '', enabled, nextRun, createdAt: 0 })
const report = (id: string, updatedAt: number, rest: Partial<Report> = {}): Report => ({ id, title: `Report ${id}`, format: 'markdown', createdAt: updatedAt, updatedAt, ...rest })

describe('greeting', () => {
  it('fits the time of day and uses the first name', () => {
    expect(greeting('Alexander Jia', 9)).toBe('Good morning, Alexander')
    expect(greeting('Alex', 14)).toBe('Good afternoon, Alex')
    expect(greeting('Alex', 20)).toBe('Good evening, Alex')
    expect(greeting('', 3)).toBe('Up late')
  })
})

describe('nextRun', () => {
  it('is the enabled schedule that runs soonest', () => {
    const now = at(8)
    const list = [sched('a', at(18)), sched('b', at(9)), sched('c', at(7)), sched('d', at(8, 30), false), sched('e', undefined), sched('f', at(12))]
    expect(nextRun(list, now)?.id).toBe('b')
    expect(nextRun([sched('c', at(7))], now)).toBeUndefined()
  })
})

describe('when', () => {
  const now = at(8)
  it('says it like you would', () => {
    expect(when(now + 20 * 60e3, now)).toBe('in 20 min')
    expect(when(now, now)).toBe('now')
    expect(when(at(19, 30), now)).toMatch(/^today at /)
    expect(when(at(7, 30, 1), now)).toMatch(/^tomorrow at /)
    expect(when(at(7, 30, 3), now)).toMatch(/^\S+ at /)
  })
})

describe('status', () => {
  const now = at(8)
  const base = { offline: false, now }

  it('says what Alexa is busy with off screen, else what runs within a day', () => {
    expect(status({ ...base, job: 'Morning briefing', next: sched('a', at(9)) })).toBe('Working on Morning briefing in the background')
    expect(status({ ...base, next: { ...sched('a', at(9)), name: 'Price watch' } })).toMatch(/^Price watch runs (in 60 min|today at .+)$/)
  })

  it('says nothing rather than filler', () => {
    expect(status(base)).toBeNull()
    expect(status({ ...base, job: null, next: sched('a', at(9, 0, 2)) })).toBeNull()
  })

  it('says when you’re offline, first', () => {
    expect(status({ ...base, offline: true, job: 'x' })).toMatch(/^You’re offline/)
  })
})

// ---------- recentWork ----------

const min = 60e3
const now = at(12)
let id = 1
const e = (kind: string, minsAgo: number, text = kind, data?: EntryData, rest: Partial<Entry> = {}): Entry => {
  const t = now - minsAgo * min
  return { id: id++, kind, text, createdAt: t, updatedAt: t, data, ...rest }
}
const ask = (minsAgo: number, text: string, data: EntryData = {}) => e('user', minsAgo, text, { start: true, ...data })
const step = (minsAgo: number) => e('step', minsAgo, 'Searching the web', undefined, { status: 'done' })
const say = (minsAgo: number, text: string) => e('say', minsAgo, text, { final: true })
const steps = (from: number, n: number) => Array.from({ length: n }, (_, i) => step(from - i * 0.1))

describe('recentWork', () => {
  const idle = { now, busy: false }

  it('lists long runs, scheduled runs and reports; not quick answers', () => {
    const feed = [
      ask(300, 'What time is it in Tokyo?'),
      step(299.9),
      say(299.8, 'It’s 2am.'),
      ask(200, 'Compare flights to Lisbon'),
      ...steps(199, 2),
      say(196, 'TAP is the one: **$412** return.\n\nMore below.'),
      e('note', 100, 'Scheduled: Morning briefing', { start: true, scheduled: true }),
      step(99),
      say(98, 'Three things today.'),
      ask(50, 'Find the best headphones'),
      step(49),
      e('report', 48, 'Headphones under $300', { id: 'r1' }),
      say(47, 'The Sony is the one.'),
    ]
    const rows = recentWork(feed, [report('r1', now - 48 * min, { title: 'Headphones under $300', data: { takeaway: 'Sony XM6 at $279.' } })], idle)
    expect(rows.map((r) => [r.title, r.line])).toEqual([
      ['Headphones under $300', 'Sony XM6 at $279.'],
      ['Morning briefing', 'Three things today.'],
      ['Compare flights to Lisbon', 'TAP is the one: $412 return.'],
    ])
    expect(rows[0]).toMatchObject({ report: 'r1' })
    expect(rows[2]).toMatchObject({ exchange: feed[3].id, at: now - 196 * min })
  })

  it('counts five steps as long, and leaves out the time spent waiting on you', () => {
    const many = [ask(30, 'Check five shops'), ...steps(29.9, 5), say(29, 'Done.')]
    expect(recentWork(many, [], idle)).toHaveLength(1)
    const waited = [ask(300, 'Order the Sony'), step(299.9), e('ask', 299.8, 'Which colour?', undefined, { status: 'answered' }), e('user', 100, 'Black'), step(99.9), say(99.8, 'Ordered.')]
    expect(recentWork(waited, [], idle)).toHaveLength(0)
  })

  it('counts work Alexa made a plan for, however quick', () => {
    const planned = [ask(30, 'Compare three museums'), e('plan', 29.9, 'Compare museums'), ...steps(29.8, 3), say(29.5, 'Only the Louvre is open today.')]
    expect(recentWork(planned, [], idle).map((r) => r.title)).toEqual(['Compare three museums'])
  })

  it('leaves out what didn’t finish, what’s still going and what’s over a week old', () => {
    const stopped = [ask(300, 'Long one'), ...steps(299, 6), e('note', 290, 'Stopped')]
    expect(recentWork(stopped, [], idle)).toEqual([])
    const going = [e('note', 3, 'Scheduled: Price watch', { start: true, scheduled: true }), step(2), say(1, 'Still $279.')]
    expect(recentWork(going, [], { now, busy: true })).toEqual([])
    expect(recentWork(going, [], idle)).toHaveLength(1)
    const old = [ask(8 * 1440, 'Old'), ...steps(8 * 1440 - 1, 6), say(8 * 1440 - 2, 'Done.')]
    expect(recentWork(old, [report('r9', now - 8 * 1440 * min)], idle)).toEqual([])
  })

  it('lists a schedule once, at its latest run that said something', () => {
    const run = (m: number, text?: string) => [e('note', m, 'Scheduled: Price watch', { start: true, scheduled: true }), step(m - 0.1), ...(text ? [say(m - 0.2, text)] : [])]
    const rows = recentWork([...run(500, 'Was $299.'), ...run(300, 'Now $279.'), ...run(100)], [], idle)
    expect(rows.map((r) => r.line)).toEqual(['Now $279.'])
  })

  it('merges in reports, once each, newest first, four at most', () => {
    const feed = [ask(60, 'Research headphones'), step(59), e('report', 58, 'Headphones', { id: 'r1', takeaway: 'Sony.' }), say(57, 'Done.')]
    const list = [report('r1', now - 58 * min, { title: 'Headphones' }), report('r2', now - 10 * min), report('r3', now - 600 * min), report('r4', now - 700 * min), report('r5', now - 800 * min)]
    expect(recentWork(feed, list, idle).map((r) => r.key)).toEqual(['rr2', 'rr1', 'rr3', 'rr4'])
  })

  it('reads a report from its entry until the list loads, and drops one the list no longer has', () => {
    const feed = [ask(60, 'Research headphones'), step(59.9), e('report', 59.8, 'Headphones', { id: 'r1', takeaway: 'Sony.' }), say(59.7, 'The Sony.')]
    expect(recentWork(feed, null, idle)).toEqual([expect.objectContaining({ title: 'Headphones', line: 'Sony.', report: 'r1' })])
    expect(recentWork(feed, [], idle)).toEqual([])
  })

  it('lists a topic once: its first request, at its latest activity', () => {
    const feed = [
      ask(300, 'Compare flights to Lisbon'),
      ...steps(299, 3),
      say(298, 'TAP is the one.'),
      e('note', 200, 'Scheduled: Price watch', { start: true, scheduled: true }),
      step(199),
      ask(100, 'And hotels near Alfama?', { followUp: true }),
      ...steps(99, 3),
      say(97, 'Memmo Alfama, $180 a night.'),
    ]
    const rows = recentWork(feed, [], idle)
    expect(rows).toEqual([expect.objectContaining({ title: 'Compare flights to Lisbon', line: 'Memmo Alfama, $180 a night.', at: now - 97 * min, exchange: feed[0].id })])
    // While its follow-up is still going, the topic waits.
    expect(recentWork(feed.slice(0, -1), [], { now, busy: true })).toEqual([])
  })

  it('keeps a follow-up on a job that said something with that job', () => {
    const feed = [
      e('note', 300, 'Scheduled: Morning briefing', { start: true, scheduled: true }),
      step(299),
      say(298, 'A strike on the Northern line.'),
      ask(100, 'How long is the strike?', { followUp: true }),
      step(99),
      say(98, 'Until Thursday.'),
    ]
    expect(recentWork(feed, [], idle)).toEqual([expect.objectContaining({ title: 'Morning briefing', line: 'Until Thursday.' })])
  })
})

describe('preview', () => {
  it('prefers the takeaway, then what’s inside', () => {
    expect(preview(report('a', 1, { data: { takeaway: 'Mistral leads.' } }))).toBe('Mistral leads.')
    expect(preview(report('a', 1, { data: { preview: { stat: { label: 'Lowest fare', value: '$412' } } } }))).toBe('Lowest fare: $412')
    expect(preview(report('a', 1, { data: { preview: { kinds: '3 options, comparison' } } }))).toBe('3 options, comparison')
  })

  it('falls back to the first line of prose, without markdown', () => {
    const content = '# Report a\n\n## Summary\n\n- **Sony** is the [quietest](https://x.y) pick\n\nMore.'
    expect(preview(report('a', 1, { content }))).toBe('Sony is the quietest pick')
  })

  it('keeps it short and skips HTML', () => {
    expect(preview(report('a', 1, { content: 'x'.repeat(300) })).length).toBeLessThanOrEqual(158)
    expect(preview(report('a', 1, { format: 'html', content: '<h1>Hi</h1>' }))).toBe('')
  })
})
