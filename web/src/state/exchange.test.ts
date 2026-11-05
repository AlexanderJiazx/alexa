import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import type { Entry, EntryData, SessionState } from '../api/types'
import {
  backgroundJob,
  current,
  dismissPending,
  exchangeStart,
  follow,
  followStart,
  followStore,
  followUps,
  holdReason,
  inBrowser,
  jobLabel,
  latestSaid,
  needsEarlier,
  newExchange,
  offerJob,
  onScreen,
  primeFollowing,
  settle,
  stale,
  settleMs,
  split,
  startFollowing,
  surfaceKey,
  surfaceOf,
  userSwitch,
  type Change,
  type Follow,
  type Guards,
} from './exchange'
import { blankSession } from './reducer'
import { initialState, store } from './store'

let id = 0
const e = (kind: string, data?: EntryData): Entry => ({ id: ++id, kind, text: kind, data, createdAt: id, updatedAt: id })

describe('exchangeStart', () => {
  it('starts at the last message that set Alexa to work', () => {
    const feed = [e('user', { start: true }), e('say'), e('user', { start: true }), e('step'), e('user'), e('say')]
    expect(exchangeStart(feed)).toBe(2)
  })

  it('falls back to the last user message before exchanges were marked', () => {
    const feed = [e('user'), e('say'), e('user'), e('say')]
    expect(exchangeStart(feed)).toBe(2)
  })

  it('is the start of the feed when there is nothing to go by', () => {
    expect(exchangeStart([e('say'), e('note')])).toBe(0)
  })
})

describe('current', () => {
  it('is the entries from the start of the exchange on', () => {
    const feed = [e('user', { start: true }), e('say'), e('user', { start: true }), e('say')]
    expect(current(feed)).toEqual(feed.slice(2))
  })

  it('keeps a quiet scheduled job off the screen, and the exchange before it in view', () => {
    const before = [e('user', { start: true }), e('say')]
    const job = [e('note', { start: true, scheduled: true }), e('step')]
    expect(current([...before, ...job])).toEqual(before)
  })

  it('shows a scheduled job once it has something to say', () => {
    const job = [e('note', { start: true, scheduled: true }), e('step'), e('say')]
    expect(current([e('user', { start: true }), ...job])).toEqual(job)
  })

  it('keeps a scheduled job with only a plan so far off the screen', () => {
    const before = [e('user', { start: true }), e('say')]
    const job = [e('note', { start: true, scheduled: true }), e('plan'), e('step')]
    expect(split([...before, ...job])).toMatchObject({ shown: before, job })
  })

  it('is empty when the only exchange is a quiet job', () => {
    expect(current([e('note', { scheduled: true }), e('step')])).toEqual([])
  })
})

describe('topics', () => {
  it('shows a follow-up with the exchanges it carries on from', () => {
    const before = [e('user', { start: true }), e('say')]
    const topic = [e('user', { start: true }), e('say'), e('user', { start: true, followUp: true }), e('say')]
    const s = split([...before, ...topic])
    expect(s.topic).toEqual(topic)
    expect(s.shown).toEqual(topic.slice(2))
    expect(current([...before, ...topic])).toEqual(topic.slice(2))
  })

  it('starts afresh with a new topic', () => {
    const feed = [e('user', { start: true }), e('say'), e('user', { start: true }), e('say')]
    expect(split(feed).topic).toEqual(feed.slice(2))
  })

  it('leaves a quiet job between a topic’s exchanges out of it', () => {
    const a = [e('user', { start: true }), e('say')]
    const job = [e('note', { start: true, scheduled: true }), e('step')]
    const b = [e('user', { start: true, followUp: true }), e('say')]
    const s = split([...a, ...job, ...b])
    expect(s.topic).toEqual([...a, ...b])
    expect(s.job).toBeNull()
  })

  it('shows a pending message as the screen was: with the topic on screen, alone after a rest', () => {
    const a = [e('user', { start: true }), e('say')]
    const b = [e('user', { start: true, topic: 'pending' })]
    expect(split([...a, ...b]).topic).toEqual([...a, ...b])
    expect(split([...a, ...b], a[1].id).topic).toEqual(b)
  })
})

describe('onScreen', () => {
  const topic = [e('user', { start: true }), e('say')]

  it('is the topic, or nothing while it rests', () => {
    expect(onScreen(topic, 0)).toEqual(topic)
    expect(onScreen(topic, topic[1].id)).toEqual([])
  })

  it('brings the whole topic back when a follow-up comes in after a rest', () => {
    const rest = topic[1].id
    const later = [e('user', { start: true, followUp: true }), e('say')]
    expect(onScreen([...topic, ...later], rest)).toEqual([...topic, ...later])
  })

  it('shows only the new topic when one starts after a rest', () => {
    const rest = topic[1].id
    const later = [e('user', { start: true }), e('say')]
    expect(onScreen([...topic, ...later], rest)).toEqual(later)
  })

  it('brings the topic back with an answer to its question', () => {
    const asked = [e('user', { start: true }), e('ask')]
    const rest = asked[1].id
    const answer = e('user')
    expect(onScreen([...asked, answer], rest)).toEqual([...asked, answer])
  })

  it('stays at rest while a quiet job works', () => {
    const rest = topic[1].id
    expect(onScreen([...topic, e('note', { start: true, scheduled: true }), e('step')], rest)).toEqual([])
  })

  it('ignores a mark from before the entries were renumbered', () => {
    expect(onScreen(topic, topic[1].id + 100)).toEqual(topic)
  })
})

describe('needsEarlier (FE-12)', () => {
  it('asks for more when no loaded entry starts an exchange', () => {
    expect(needsEarlier([e('step'), e('step')], true)).toBe(true)
  })

  it('asks for more when the topic carries on from before the first entry loaded', () => {
    expect(needsEarlier([e('user', { start: true, followUp: true }), e('say')], true)).toBe(true)
    expect(needsEarlier([e('say'), e('user', { start: true, followUp: true }), e('say')], true)).toBe(true)
  })

  it('doesn’t when the start is loaded or there is nothing before', () => {
    expect(needsEarlier([e('user', { start: true }), e('step')], true)).toBe(false)
    expect(needsEarlier([e('say'), e('user', { start: true }), e('user', { start: true, followUp: true })], true)).toBe(false)
    expect(needsEarlier([e('step')], false)).toBe(false)
    expect(needsEarlier([], true)).toBe(false)
  })
})

describe('latestSaid', () => {
  it('is the last say or error since the user spoke', () => {
    const s = e('say')
    expect(latestSaid([e('user'), e('say'), s, e('step')])).toBe(s)
    expect(latestSaid([e('say'), e('user'), e('step')])).toBeNull()
  })
})

describe('split (IX-F5)', () => {
  it('keeps a quiet job off the screen and hands it back as the job', () => {
    const before = [e('user', { start: true }), e('say')]
    const job = [e('note', { start: true, scheduled: true }), e('step')]
    const s = split([...before, ...job])
    expect(s.shown).toEqual(before)
    expect(s.job).toEqual(job)
    expect(jobLabel(job)).toBe('note')
  })

  it('has no job once the job has spoken', () => {
    const job = [e('note', { start: true, scheduled: true }), e('step'), e('say')]
    expect(split([e('user', { start: true }), ...job])).toEqual({ shown: job, topic: job, job: null })
  })

  it('names a job after its note', () => {
    expect(jobLabel([{ ...e('note'), text: 'Scheduled: Morning briefing' }])).toBe('Morning briefing')
    expect(jobLabel([])).toBe('A scheduled job')
  })
})

describe('stale (Appendix C.5)', () => {
  const hour = 3600e3
  const at = (kind: string, ago: number): Entry => ({ ...e(kind), updatedAt: 100 * hour - ago })
  const question = { ...blankSession, status: 'needs' as const, needs: { kind: 'question' as const, message: 'Which one?' } }

  it('puts away a seen exchange that ended more than six hours ago', () => {
    const ex = [at('user', 7 * hour), at('say', 7 * hour)]
    expect(stale(blankSession, ex, ex[1].id, 100 * hour)).toBe(true)
    expect(stale(blankSession, ex, ex[1].id - 1, 100 * hour)).toBe(false)
    expect(stale(blankSession, [at('user', 5 * hour)], 1e9, 100 * hour)).toBe(false)
    expect(stale(blankSession, [], 1e9, 100 * hour)).toBe(false)
  })

  it('puts away a question left waiting that long, for the board’s Needs you', () => {
    const ex = [at('user', 8 * hour), at('ask', 7 * hour)]
    expect(stale(question, ex, ex[1].id, 100 * hour)).toBe(true)
    // Never while Alexa works, or waits on you in the browser.
    expect(stale({ ...blankSession, status: 'working' }, ex, ex[1].id, 100 * hour)).toBe(false)
    expect(stale({ ...question, needs: { kind: 'takeover', message: 'Code?' } }, ex, ex[1].id, 100 * hour)).toBe(false)
  })
})

describe('backgroundJob and inBrowser (IX-F5, R6)', () => {
  const working = { ...blankSession, status: 'working' as const }
  const step = (kind: string, tool = 'x') => e('step', { step: { tool, kind: kind as never, status: 'done' } })

  it('is the scheduled exchange Alexa is at work on, said or quiet', () => {
    const job = [e('note', { start: true, scheduled: true }), step('search'), e('say')]
    expect(backgroundJob(working, [e('user', { start: true }), ...job])).toEqual(job)
    expect(backgroundJob(blankSession, job)).toBeNull()
    expect(backgroundJob(working, [...job, e('user', { start: true })])).toBeNull()
  })

  it('says whether the job’s latest work is in the browser', () => {
    expect(inBrowser([step('search'), step('browse')], null)).toBe(true)
    expect(inBrowser([step('browse'), step('read')], null)).toBe(false)
    expect(inBrowser([step('other', 'browser_scroll')], null)).toBe(true)
    // An older server's step says nothing about its tool.
    expect(inBrowser([e('step')], null)).toBe(false)
    const tool = (kind: 'click' | 'search') => ({ runId: 'r', at: 1, phase: 'tool' as const, tool: { name: 't', label: 't', kind } })
    expect(inBrowser([step('search')], tool('click'))).toBe(true)
    expect(inBrowser([step('browse')], tool('search'))).toBe(false)
  })
})

describe('followUps', () => {
  const say = (suggestions: string[], final?: boolean) => e('say', { suggestions, final })

  it('offers up to three, trimmed and without repeats, under the last say', () => {
    const s = say([' Make it shorter ', 'make it shorter', 'Track this daily', '', 'Email it to me', 'Compare 6–10'])
    expect(followUps([e('user'), s], false)).toEqual({ id: s.id, items: ['Make it shorter', 'Track this daily', 'Email it to me'] })
  })

  it('waits until Alexa is done, unless the say is final', () => {
    expect(followUps([say(['Go on'])], true)).toBeNull()
    expect(followUps([say(['Go on'], true)], true)?.items).toEqual(['Go on'])
  })

  it('goes away once something follows the say', () => {
    expect(followUps([say(['Go on']), e('user')], false)).toBeNull()
    expect(followUps([say([])], false)).toBeNull()
    expect(followUps([], false)).toBeNull()
  })
})

describe('surfaceKey and surfaceOf', () => {
  const view = (surface: SessionState['surface'], control = false): SessionState => ({ ...blankSession, surface, control })

  it('names the panel a surface calls for', () => {
    expect(surfaceKey(view({ kind: '' }))).toBeNull()
    expect(surfaceKey(view({ kind: 'browser' }))).toBe('browser')
    expect(surfaceKey(view({ kind: 'report', id: 'r1' }))).toBe('report:r1')
    expect(surfaceKey(view({ kind: 'entry', id: '42' }))).toBe('entry:42')
    expect(surfaceKey(view({ kind: 'report' }))).toBeNull()
    expect(surfaceKey(view({ kind: 'report', id: 'r1' }, true))).toBe('browser')
  })

  it('turns a key back into a surface', () => {
    expect(surfaceOf(null)).toEqual({ kind: '' })
    expect(surfaceOf('browser')).toEqual({ kind: 'browser' })
    expect(surfaceOf('report:r:1')).toEqual({ kind: 'report', id: 'r:1' })
  })
})

describe('follow guards (Appendix C.2)', () => {
  const calm: Guards = { typing: false, scrolled: false, overlay: false }
  const t = 1_000_000
  const server = (guards: Partial<Guards> = {}, now = t): Change => ({ by: 'server', now, guards: { ...calm, ...guards } })
  const synced = (shown: string | null, extra: Partial<Follow> = {}): Follow => ({ ...followStart, shown, server: shown, synced: true, ...extra })

  it('takes the first surface as it is', () => {
    expect(follow(followStart, 'report:a', server({ typing: true }))).toMatchObject({ shown: 'report:a', server: 'report:a', pending: null, synced: true })
  })

  it('follows Alexa when nothing holds the screen', () => {
    expect(follow(synced(null), 'browser', server())).toMatchObject({ shown: 'browser', pending: null })
    expect(follow(synced('report:a'), 'browser', server())).toMatchObject({ shown: 'browser', pending: null })
  })

  it('holds the switch back while the composer has text (acceptance 5)', () => {
    const f = follow(synced('report:a'), 'browser', server({ typing: true }))
    expect(f).toMatchObject({ shown: 'report:a', server: 'browser', pending: 'browser' })
  })

  it('holds it while the column is scrolled up, or Reflect or a sheet is open', () => {
    expect(follow(synced(null), 'browser', server({ scrolled: true })).pending).toBe('browser')
    expect(follow(synced(null), 'report:b', server({ overlay: true })).pending).toBe('report:b')
  })

  it('holds it for ten seconds after the user switched', () => {
    const f = synced('report:a', { userAt: t })
    expect(follow(f, 'browser', server({}, t + 9_000)).pending).toBe('browser')
    expect(follow(f, 'browser', server({}, t + 10_500)).shown).toBe('browser')
    expect(holdReason(f, calm, t + 1)).toBe('recent')
    expect(holdReason(f, { ...calm, typing: true }, t + 1)).toBe('typing')
    expect(holdReason(f, calm, t + settleMs)).toBe('')
  })

  it('always lets the user switch, and remembers when', () => {
    const f = follow(synced('report:a', { pending: 'browser', server: 'browser' }), null, { by: 'user', now: t, guards: calm })
    expect(f).toEqual({ shown: null, server: null, pending: null, userAt: t, synced: true })
  })

  it('always shows a take-over or a sign-in', () => {
    const f = follow(synced('report:a'), 'browser', { by: 'forced', now: t, guards: { typing: true, scrolled: true, overlay: true } })
    expect(f).toMatchObject({ shown: 'browser', pending: null })
  })

  it('drops the pill when Alexa goes back to what is shown', () => {
    const held = follow(synced('report:a'), 'browser', server({ typing: true }))
    expect(follow(held, 'report:a', server({ typing: true }))).toMatchObject({ shown: 'report:a', server: 'report:a', pending: null })
  })

  it('closes only the panel the close was about', () => {
    expect(follow(synced('browser'), null, server({ typing: true }))).toMatchObject({ shown: null, pending: null })
    const kept = follow(synced('report:a'), 'browser', server({ typing: true }))
    expect(follow(kept, null, server())).toMatchObject({ shown: 'report:a', server: null, pending: null })
  })

  it('ignores a repeat of the same surface', () => {
    const f = synced('browser')
    expect(follow(f, 'browser', server())).toBe(f)
  })

  it('holds back a switch a background job asks for (R6)', () => {
    expect(follow(synced('report:a'), 'browser', server({ background: true }))).toMatchObject({ shown: 'report:a', pending: 'browser' })
    expect(holdReason(synced(null), { ...calm, background: true }, t)).toBe('background')
  })

  it('offers a background job’s browsing only over another panel', () => {
    expect(offerJob(synced('report:a'), true)).toMatchObject({ shown: 'report:a', pending: 'browser' })
    for (const f of [synced(null), synced('browser'), synced('report:a', { pending: 'entry:1' }), followStart]) expect(offerJob(f, true)).toBe(f)
    const r = synced('report:a')
    expect(offerJob(r, false)).toBe(r)
  })

  it('drops only a browser pill once Alexa is idle', () => {
    expect(settle(synced(null, { server: 'browser', pending: 'browser' }), true).pending).toBeNull()
    const report = synced(null, { server: 'report:b', pending: 'report:b' })
    expect(settle(report, true)).toBe(report)
    const working = synced(null, { server: 'browser', pending: 'browser' })
    expect(settle(working, false)).toBe(working)
  })

  it('starts a new topic with what the session shows', () => {
    const kept = follow(synced('report:a'), 'browser', server({ typing: true }))
    // The session closed its panel for the new topic: a clean screen.
    expect(newExchange(follow(kept, null, server()))).toMatchObject({ shown: null, pending: null })
    // The new request's run took the browser before the topic was settled.
    expect(newExchange(kept)).toMatchObject({ shown: 'browser', pending: null })
    const plain = synced('browser')
    expect(newExchange(plain)).toBe(plain)
    expect(newExchange(synced(null, { server: 'browser', pending: 'browser' }))).toMatchObject({ shown: 'browser', pending: null })
  })
})

describe('startFollowing', () => {
  const session = (surface: SessionState['surface'], rest: Partial<SessionState> = {}): SessionState => ({ ...blankSession, surface, ...rest })
  const load = (s: SessionState, feed: Entry[] = []) => store.set((a) => ({ ...a, data: { ...a.data, loaded: true, session: s, feed } }))
  let guards = { typing: false, scrolled: false }
  let stop = () => {}

  beforeEach(() => {
    store.set(initialState)
    followStore.set(followStart)
    guards = { typing: false, scrolled: false }
    stop = startFollowing(() => guards)
  })
  afterEach(() => stop())

  it('follows the session, and holds a switch while typing', () => {
    load(session({ kind: 'report', id: 'a' }))
    expect(followStore.get().shown).toBe('report:a')
    guards = { typing: true, scrolled: false }
    load(session({ kind: 'browser' }, { status: 'working' }))
    expect(followStore.get()).toMatchObject({ shown: 'report:a', pending: 'browser' })
  })

  it('primes a new stage from a loaded session, ready to guard the next switch', () => {
    stop()
    load(session({ kind: 'report', id: 'a' }))
    followStore.set({ ...followStart, shown: 'browser', pending: 'entry:1', synced: true })
    primeFollowing()
    expect(followStore.get()).toMatchObject({ shown: 'report:a', server: 'report:a', pending: null, synced: true })
    stop = startFollowing(() => guards)
    expect(followStore.get().shown).toBe('report:a')
    guards = { typing: true, scrolled: false }
    load(session({ kind: 'browser' }, { status: 'working' }))
    expect(followStore.get()).toMatchObject({ shown: 'report:a', pending: 'browser' })
  })

  it('lets a switch the user made through', () => {
    load(session({ kind: '' }))
    guards = { typing: true, scrolled: true }
    userSwitch()
    load(session({ kind: 'report', id: 'b' }))
    expect(followStore.get()).toMatchObject({ shown: 'report:b', pending: null })
    expect(followStore.get().userAt).toBeGreaterThan(0)
  })

  it('a scheduled run opening the browser while the user reads a report shows the pill (acceptance 3)', () => {
    // A server that still moves the screen for a background job: nothing
    // on screen holds it, the job itself does (R6).
    const feed = [e('user', { start: true }), e('say'), e('report', { id: 'a' })]
    load(session({ kind: 'report', id: 'a' }), feed)
    const job = [...feed, e('note', { start: true, scheduled: true })]
    load(session({ kind: 'browser' }, { status: 'working' }), job)
    expect(followStore.get()).toMatchObject({ shown: 'report:a', server: 'browser', pending: 'browser' })
  })

  it('offers the pill once when a background job browses and the screen stays put (R6)', () => {
    const feed = [e('user', { start: true }), e('say'), e('report', { id: 'a' })]
    load(session({ kind: 'report', id: 'a' }), feed)
    const note = e('note', { start: true, scheduled: true })
    const search = e('step', { step: { tool: 'web_search', kind: 'search', status: 'done' } })
    load(session({ kind: 'report', id: 'a' }, { status: 'working' }), [...feed, note, search])
    expect(followStore.get().pending).toBeNull()
    const open = e('step', { step: { tool: 'browser_open', kind: 'browse', status: 'running', host: 'bbc.com' } })
    load(session({ kind: 'report', id: 'a' }, { status: 'working' }), [...feed, note, search, open])
    expect(followStore.get()).toMatchObject({ shown: 'report:a', server: 'report:a', pending: 'browser' })
    // Not now: the next step doesn't bring it back.
    dismissPending()
    const click = e('step', { step: { tool: 'browser_click', kind: 'click', status: 'running' } })
    load(session({ kind: 'report', id: 'a' }, { status: 'working' }), [...feed, note, search, open, click])
    expect(followStore.get().pending).toBeNull()
  })

  it('a held switch for a background job is offered once, too', () => {
    const feed = [e('user', { start: true }), e('say')]
    load(session({ kind: 'report', id: 'a' }), feed)
    const job = [...feed, e('note', { start: true, scheduled: true }), e('step', { step: { tool: 'browser_open', kind: 'browse', status: 'running' } })]
    load(session({ kind: 'browser' }, { status: 'working' }), job)
    expect(followStore.get().pending).toBe('browser')
    dismissPending()
    load(session({ kind: 'browser' }, { status: 'working', activity: 'Reading' }), [...job, e('step', { step: { tool: 'browser_click', kind: 'click', status: 'done' } })])
    expect(followStore.get()).toMatchObject({ shown: 'report:a', pending: null })
  })

  it('drops the browser pill once Alexa is idle, and keeps a report’s', () => {
    load(session({ kind: '' }))
    guards = { typing: true, scrolled: false }
    load(session({ kind: 'browser' }, { status: 'working' }))
    expect(followStore.get().pending).toBe('browser')
    load(session({ kind: 'browser' }, { status: 'idle' }))
    expect(followStore.get()).toMatchObject({ shown: null, pending: null })
    load(session({ kind: 'report', id: 'b' }, { status: 'idle' }))
    expect(followStore.get()).toMatchObject({ shown: null, pending: 'report:b' })
  })

  it('shows a take-over even while the user types', () => {
    load(session({ kind: 'report', id: 'a' }))
    guards = { typing: true, scrolled: true }
    load(session({ kind: 'browser' }, { status: 'needs', needs: { kind: 'takeover', message: 'Enter the code' } }))
    expect(followStore.get()).toMatchObject({ shown: 'browser', pending: null })
  })

  it('lets go of a kept panel when a new exchange starts', () => {
    const feed = [e('user', { start: true }), e('say')]
    load(session({ kind: 'report', id: 'a' }), feed)
    guards = { typing: true, scrolled: false }
    load(session({ kind: 'browser' }, { status: 'working' }), feed)
    expect(followStore.get().pending).toBe('browser')
    // The new request's run has the browser: the screen shows it.
    const next = [...feed, e('user', { start: true })]
    load(session({ kind: 'browser' }, { status: 'working' }), next)
    expect(followStore.get()).toMatchObject({ shown: 'browser', pending: null })
    // The session closed its panel for the new topic: a clean screen.
    load(session({ kind: 'report', id: 'a' }), feed)
    load(session({ kind: 'browser' }, { status: 'working' }), feed)
    load(session({ kind: '' }, { status: 'working' }), next)
    expect(followStore.get()).toMatchObject({ shown: null, pending: null })
  })
})
