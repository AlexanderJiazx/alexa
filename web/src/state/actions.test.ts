import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { Surface } from '../api/types'

// show() opens a panel at once and tells the server. When the server says
// no (a report or an entry that's gone answers 404 and sends no state),
// the screen goes back to what it showed, since nothing else would.

const setSurface = vi.fn<(s: Surface) => Promise<unknown>>()
const calls: string[] = []
vi.mock('../api/client', () => ({
  api: {
    setSurface: (s: Surface) => setSurface(s),
    logout: async () => void calls.push('logout'),
  },
}))
const notify = vi.fn()
vi.mock('../sheets/toasts', () => ({ notify: (n: unknown) => notify(n) }))
vi.mock('../sheets/undo', () => ({
  undo: {
    flush: async () => {
      await new Promise((r) => setTimeout(r, 0))
      calls.push('flush')
    },
  },
}))

const { openRef, plainly, show, signOut } = await import('./actions')
const { dispatch, setUI, store } = await import('./store')
const { followStore, primeFollowing, startFollowing } = await import('./exchange')

// An error as the API client throws it: a status and the server's text.
const apiError = (status: number, message: string) => Object.assign(new Error(message), { status })

const surface = () => store.get().data.session.surface
const settle = () => new Promise((r) => setTimeout(r, 0))

beforeEach(() => {
  vi.stubGlobal('history', { state: null, back() {}, pushState() {} })
  dispatch({ type: 'clear' })
  setSurface.mockReset()
  notify.mockReset()
})
afterEach(() => vi.unstubAllGlobals())

describe('show', () => {
  it('keeps the panel the server accepted', async () => {
    setSurface.mockResolvedValue(null)
    show({ kind: 'report', id: 'r1' })
    expect(surface()).toEqual({ kind: 'report', id: 'r1' })
    await settle()
    expect(surface()).toEqual({ kind: 'report', id: 'r1' })
    expect(notify).not.toHaveBeenCalled()
  })

  it('goes back to what was there when the server refuses it', async () => {
    const before = surface()
    setSurface.mockRejectedValue(new Error('not found'))
    show({ kind: 'report', id: 'gone' })
    expect(surface()).toEqual({ kind: 'report', id: 'gone' })
    await settle()
    expect(surface()).toBe(before)
    expect(notify).toHaveBeenCalledTimes(1)
  })

  it('leaves a newer surface from the server alone', async () => {
    let fail: (e: Error) => void = () => {}
    setSurface.mockReturnValue(new Promise((_, no) => (fail = no)))
    show({ kind: 'report', id: 'gone' })
    // The server moved the screen on before the refusal came back.
    dispatch({ type: 'surface', surface: { kind: 'browser' } })
    fail(new Error('not found'))
    await settle()
    expect(surface()).toEqual({ kind: 'browser' })
  })

  // FH-5: going back was taken for Alexa's switch and held behind the
  // user's own, so the panel stayed on its error and the pill offered the
  // report the user had been reading as new.
  it('goes back as the user’s own switch, with no pill', async () => {
    store.set((s) => ({ ...s, data: { ...s.data, loaded: true, session: { ...s.data.session, surface: { kind: 'report', id: 'a' } } } }))
    primeFollowing()
    const stop = startFollowing(() => ({ typing: false, scrolled: false }))
    try {
      setSurface.mockRejectedValue(apiError(404, 'not found'))
      show({ kind: 'report', id: 'gone' })
      expect(followStore.get().shown).toBe('report:gone')
      await settle()
      expect(surface()).toEqual({ kind: 'report', id: 'a' })
      expect(followStore.get()).toMatchObject({ shown: 'report:a', server: 'report:a', pending: null })
    } finally {
      stop()
    }
  })

  // FH-15: the toast said the server's "not found".
  it('says in plain words what couldn’t be opened', async () => {
    setSurface.mockRejectedValue(apiError(404, 'not found'))
    show({ kind: 'report', id: 'gone' })
    await settle()
    expect(notify).toHaveBeenLastCalledWith(expect.objectContaining({ title: 'Couldn’t open that', body: 'That report isn’t there any more.' }))
    show({ kind: 'entry', id: '41' })
    await settle()
    expect(notify).toHaveBeenLastCalledWith(expect.objectContaining({ body: 'That message isn’t there any more.' }))
  })
})

describe('plainly', () => {
  it('never passes on the server’s shorthand', () => {
    expect(plainly(apiError(404, 'not found'))).toBe('That isn’t there any more.')
    expect(plainly(apiError(404, 'not found'), 'That report isn’t there any more.')).toBe('That report isn’t there any more.')
    expect(plainly(apiError(502, 'Bad Gateway'))).toBe('Try again in a moment.')
    expect(plainly(apiError(400, 'unknown surface'))).toBe('Try again in a moment.')
    expect(plainly(new TypeError('Failed to fetch'))).toBe('You’re offline.')
    expect(plainly(new TypeError('Load failed'))).toBe('You’re offline.')
    expect(plainly(undefined)).toBe('Try again in a moment.')
  })
  it('keeps what the server words for people', () => {
    expect(plainly(apiError(409, 'Alexa is working right now; try again when it’s done'))).toBe('Alexa is working right now; try again when it’s done')
  })
})

// Signing out sends the deletes still waiting on their Undo first, while
// the session they need is still valid (FE-2).
describe('signOut', () => {
  it('flushes the undoable deletes before it signs out', async () => {
    calls.length = 0
    vi.stubGlobal('location', { href: '/app' })
    await signOut()
    expect(calls).toEqual(['flush', 'logout'])
    expect(location.href).toBe('/')
  })
})

// A question's notification opens it where it can be seen: on the main
// screen while it waits, in Reflect's conversation once answered (D1).
describe('openRef', () => {
  const ui = () => store.get().ui

  it('opens Reflect at a question answered since', () => {
    dispatch({ type: 'session', session: { ...store.get().data.session, status: 'idle' } })
    openRef('entry:41')
    expect(ui().reflect).toBe(true)
    expect(ui().reflectTab).toBe('conversation')
  })

  it('leaves a question still waiting to the main screen', () => {
    setUI({ reflect: true, sheet: 'notifications' })
    dispatch({ type: 'session', session: { ...store.get().data.session, status: 'needs', needs: { kind: 'question', message: 'When?', entryId: 41 } } })
    openRef('entry:41')
    expect(ui().reflect).toBe(false)
    expect(ui().sheet).toBe(null)
  })
})
