import { describe, expect, it } from 'vitest'
import type { Entry } from '../../api/types'
import { buildCommands, withAsk, type CommandContext } from './commands'
import { runTitle, firstLine } from './context'
import { filterCommands, score } from './filter'
import { shortcut, type KeyInfo } from './shortcuts'

const ctx: CommandContext = { status: 'idle', control: false, surface: '', reflect: false, isAdmin: false, reports: [] }
const titles = (c: CommandContext, q = '') => filterCommands(buildCommands(c), q).map((x) => x.title)

describe('filterCommands', () => {
  const items = [{ title: 'Open Reflect', keywords: 'history' }, { title: 'New schedule', keywords: 'cron every' }, { title: 'Settings' }, { title: 'Scheduled', keywords: 'schedules jobs' }, { title: 'Stop' }]

  it('keeps everything, in order, with nothing typed', () => {
    expect(filterCommands(items, '  ').map((x) => x.title)).toEqual(items.map((x) => x.title))
  })

  it('puts a title that starts with the query first', () => {
    expect(filterCommands(items, 'sch').map((x) => x.title)).toEqual(['Scheduled', 'New schedule'])
    expect(filterCommands(items, 'st')[0].title).toBe('Stop')
  })

  it('finds by a word, a keyword and letters in order', () => {
    expect(filterCommands(items, 'reflect')[0].title).toBe('Open Reflect')
    expect(filterCommands(items, 'history')[0].title).toBe('Open Reflect')
    expect(filterCommands(items, 'cron')[0].title).toBe('New schedule')
    expect(filterCommands(items, 'nsch')[0].title).toBe('New schedule')
  })

  it('ignores case and accents, and drops what doesn’t match', () => {
    expect(score({ title: 'Résumé review' }, 'resume')).toBeGreaterThan(0)
    expect(filterCommands(items, 'SETT').map((x) => x.title)).toEqual(['Settings'])
    expect(filterCommands(items, 'zzz')).toEqual([])
  })
})

describe('buildCommands', () => {
  it('offers Stop only while Alexa is busy', () => {
    expect(titles(ctx)).not.toContain('Stop')
    expect(titles({ ...ctx, status: 'working' })[0]).toBe('Stop')
    expect(titles({ ...ctx, status: 'needs' })).toContain('Stop')
  })

  it('has the commands the palette promises', () => {
    const t = titles(ctx)
    for (const want of ['Open Reflect', 'New schedule', 'Memory', 'Logins', 'Settings', 'Sign out']) expect(t).toContain(want)
    expect(t.some((x) => x.startsWith('Switch to'))).toBe(true)
  })

  it('finds them by what people type', () => {
    expect(titles({ ...ctx, status: 'working' }, 'stop')[0]).toBe('Stop')
    expect(titles(ctx, 'reflect')[0]).toBe('Open Reflect')
    expect(titles(ctx, 'new sch')[0]).toBe('New schedule')
    expect(titles(ctx, 'dark mode')[0]).toMatch(/^Switch to/)
    expect(titles(ctx, 'zebra mode')).toEqual([])
    expect(titles(ctx, 'theme')[0]).toMatch(/^Switch to/)
  })

  it('shows Users to admins only, and the panel command by the surface', () => {
    expect(titles(ctx)).not.toContain('Users')
    expect(titles({ ...ctx, isAdmin: true })).toContain('Users')
    expect(titles(ctx)).toContain('Show the browser')
    expect(titles({ ...ctx, surface: 'report' })).toContain('Close the panel')
    expect(titles({ ...ctx, reflect: true })).toContain('Back to Alexa')
  })

  it('lists recent reports to jump to', () => {
    const reports = [{ id: 'r1', title: 'Flights to Lisbon', format: 'markdown' as const, createdAt: 0, updatedAt: 0 }]
    expect(titles({ ...ctx, reports }, 'lisbon')).toEqual(['Flights to Lisbon'])
  })
})

describe('withAsk', () => {
  it('ends a search with asking Alexa, so a request typed here isn’t a dead end', () => {
    const found = withAsk(filterCommands(buildCommands(ctx), 'book a table for two'), 'book a table for two ')
    expect(found.map((c) => c.id)).toEqual(['ask'])
    expect(found[0].title).toBe('Ask Alexa “book a table for two”')
    const some = withAsk(filterCommands(buildCommands(ctx), 'set'), 'set')
    expect(some[some.length - 1].id).toBe('ask')
    expect(some.length).toBeGreaterThan(1)
  })
  it('adds nothing with nothing typed', () => {
    expect(withAsk(buildCommands(ctx), '  ').some((c) => c.id === 'ask')).toBe(false)
  })
})

describe('shortcut', () => {
  const k = (key: string, o: Partial<KeyInfo> = {}): KeyInfo => ({ key, meta: false, ctrl: false, alt: false, shift: false, typing: false, ...o })

  it('reads ⌘ on a Mac and Ctrl elsewhere', () => {
    expect(shortcut(k('k', { meta: true }), true)).toBe('palette')
    expect(shortcut(k('k', { ctrl: true }), true)).toBe(null)
    expect(shortcut(k('k', { ctrl: true }), false)).toBe('palette')
    expect(shortcut(k('K', { meta: true }), true)).toBe('palette')
  })

  it('maps the app’s shortcuts, even while typing', () => {
    expect(shortcut(k('.', { meta: true, typing: true }), true)).toBe('stop')
    expect(shortcut(k('j', { meta: true }), true)).toBe('reflect')
    expect(shortcut(k('b', { meta: true }), true)).toBe('panel')
    expect(shortcut(k(',', { meta: true, typing: true }), true)).toBe('settings')
    expect(shortcut(k(',', { ctrl: true }), false)).toBe('settings')
    expect(shortcut(k('Escape'), true)).toBe('escape')
  })

  it('lets / type while typing, and leaves other chords alone', () => {
    expect(shortcut(k('/'), true)).toBe('composer')
    expect(shortcut(k('/', { typing: true }), true)).toBe(null)
    expect(shortcut(k('k', { meta: true, shift: true }), true)).toBe(null)
    expect(shortcut(k('c', { meta: true }), true)).toBe(null)
  })
})

describe('runTitle', () => {
  const entry = (p: Partial<Entry>): Entry => ({ id: 1, kind: 'user', text: '', createdAt: 0, updatedAt: 0, ...p })

  it('says nothing while idle', () => {
    expect(runTitle({ status: 'idle', control: false }, [entry({ text: 'Find flights' })])).toBe('')
  })

  it('prefers the plan’s title, then the job, then the request', () => {
    const ex = [entry({ text: 'Find me flights to Lisbon\nnext week' })]
    expect(runTitle({ status: 'working', control: false, plan: { title: 'Lisbon flights', steps: [] } }, ex)).toBe('Lisbon flights')
    expect(runTitle({ status: 'working', control: false }, ex)).toBe('Find me flights to Lisbon')
    expect(runTitle({ status: 'working', control: false }, [entry({ kind: 'note', text: 'Scheduled: Morning briefing', data: { scheduled: true } })])).toBe('Morning briefing · scheduled')
  })

  it('cuts a long line at a word', () => {
    const t = firstLine('Compare the three cheapest direct flights from London to Lisbon next weekend with a window seat')
    expect(t.length).toBeLessThanOrEqual(60)
    expect(t.endsWith('…')).toBe(true)
    expect(t).not.toMatch(/\s…$/)
  })
})
