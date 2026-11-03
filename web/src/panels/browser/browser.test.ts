import { describe, expect, it } from 'vitest'
import type { Entry } from '../../api/types'
import { actionFor, runningBrowserStep, secretMask } from './action'
import { addressOf, clickCount, hostParts } from './address'
import { glide, pathFor, pointAt } from './glide'
import { buttonBit, buttonName, chord, historyFor, maxZoom, modifiers, noZoom, pinchZoom, toPage, touchCancel, touchEnd, touchHold, touchMove, touchStart, wheelDelta } from './input'
import { isStale, liveStatus, retryDelay, staleAfter } from './status'

const step = (id: number, status: string, data: Record<string, unknown>): Entry => ({
  id,
  kind: 'step',
  text: 'Working on it',
  status,
  data: { step: { tool: 'browser_click', kind: 'click', status: 'running', ...data } as never },
  createdAt: 0,
  updatedAt: 0,
})

describe('liveStatus', () => {
  const base = { socket: 'open' as const, asleep: false, hasFrame: true, stale: false }
  it('is live once frames arrive', () => {
    expect(liveStatus(base)).toBe('live')
  })
  it('is connecting until the first frame', () => {
    expect(liveStatus({ ...base, hasFrame: false })).toBe('connecting')
    expect(liveStatus({ ...base, socket: 'connecting' })).toBe('connecting')
  })
  it('says reconnecting when the socket dropped, even with a picture', () => {
    expect(liveStatus({ ...base, socket: 'closed' })).toBe('reconnecting')
  })
  it('puts asleep before everything', () => {
    expect(liveStatus({ ...base, socket: 'closed', asleep: true })).toBe('asleep')
  })
  it('says stale only while connected', () => {
    expect(liveStatus({ ...base, stale: true })).toBe('stale')
    expect(liveStatus({ ...base, stale: true, socket: 'closed' })).toBe('reconnecting')
  })
})

describe('isStale', () => {
  it('needs something expected, and staleAfter without a frame', () => {
    expect(isStale(null, 1e6)).toBe(false)
    expect(isStale(1000, 1000 + staleAfter)).toBe(false)
    expect(isStale(1000, 1001 + staleAfter)).toBe(true)
  })
})

describe('retryDelay', () => {
  it('doubles from half a second up to ten, with jitter', () => {
    const mid = () => 0.5
    expect(retryDelay(1, mid)).toBe(500)
    expect(retryDelay(2, mid)).toBe(1000)
    expect(retryDelay(3, mid)).toBe(2000)
    expect(retryDelay(9, mid)).toBe(10000)
    expect(retryDelay(1, () => 0)).toBe(425)
    expect(retryDelay(1, () => 1)).toBe(575)
  })
})

describe('pinchZoom', () => {
  const w = 400
  const h = 250
  it('zooms around the spot between the fingers', () => {
    // Fingers 100px apart round (200, 100) spread to 200px apart.
    const z = pinchZoom(noZoom, { x: 150, y: 100 }, { x: 250, y: 100 }, { x: 100, y: 100 }, { x: 300, y: 100 }, w, h)
    expect(z).toEqual({ k: 2, x: -200, y: -100 })
    // That spot is still under them: (200 - x) / k is 200 on the picture.
    expect((200 - z.x) / z.k).toBe(200)
    // And a point maps back to the page through the zoomed box.
    const box = { left: z.x, top: z.y, width: w * z.k, height: h * z.k }
    expect(toPage(200, 100, box, { w: 1280, h: 800 })).toEqual({ x: 640, y: 320 })
  })

  it('pans with two fingers, keeping the picture over the screen', () => {
    const z0 = { k: 2, x: -200, y: -100 }
    expect(pinchZoom(z0, { x: 100, y: 100 }, { x: 200, y: 100 }, { x: 150, y: 120 }, { x: 250, y: 120 }, w, h)).toEqual({ k: 2, x: -150, y: -80 })
    // Pushed past an edge, it stops at the edge.
    expect(pinchZoom(z0, { x: 100, y: 100 }, { x: 200, y: 100 }, { x: 600, y: 400 }, { x: 700, y: 400 }, w, h)).toEqual({ k: 2, x: 0, y: 0 })
  })

  it('never shrinks the picture, nor zooms past the most', () => {
    expect(pinchZoom(noZoom, { x: 100, y: 100 }, { x: 300, y: 100 }, { x: 180, y: 100 }, { x: 220, y: 100 }, w, h)).toEqual(noZoom)
    expect(pinchZoom(noZoom, { x: 199, y: 100 }, { x: 201, y: 100 }, { x: 0, y: 100 }, { x: 400, y: 100 }, w, h).k).toBe(maxZoom)
  })
})

describe('glide', () => {
  it('matches the server: instant for no distance, capped at 700ms', () => {
    expect(glide(0)).toBe(0)
    expect(glide(100)).toBe(250)
    expect(glide(5000)).toBe(700)
  })
  it('starts and ends where it should, bending to one side', () => {
    const p = pathFor({ x: 0, y: 0 }, { x: 100, y: 0 }, 1)
    expect(pointAt(p, 0)).toEqual({ x: 0, y: 0 })
    expect(pointAt(p, 1)).toEqual({ x: 100, y: 0 })
    expect(pointAt(p, 0.5).y).toBeGreaterThan(0)
    expect(pointAt(pathFor({ x: 0, y: 0 }, { x: 100, y: 0 }, -1), 0.5).y).toBeLessThan(0)
  })
})

describe('take-over input', () => {
  const r = { left: 100, top: 50, width: 640, height: 400 }
  const size = { w: 1280, h: 800 }
  it('maps the screen to the page, and keeps a drag that leaves it on the edge', () => {
    expect(toPage(100, 50, r, size)).toEqual({ x: 0, y: 0 })
    expect(toPage(420, 250, r, size)).toEqual({ x: 640, y: 400 })
    expect(toPage(2000, -40, r, size)).toEqual({ x: 1279, y: 0 })
  })
  it('names buttons as the server does and masks them as the DOM does', () => {
    expect([0, 1, 2, 3].map(buttonName)).toEqual(['left', 'middle', 'right', null])
    expect([0, 1, 2, 3, 4].map(buttonBit)).toEqual([1, 4, 2, 8, 16])
    expect([3, 4, 0].map(historyFor)).toEqual(['back', 'forward', null])
  })
  it('sees a second button as a chord on a move', () => {
    expect(chord(-1, 1)).toBe(null)
    expect(chord(2, 3)).toBe('down')
    expect(chord(2, 1)).toBe('up')
  })
  it('counts modifiers as CDP does', () => {
    expect(modifiers({ altKey: true, ctrlKey: false, metaKey: true, shiftKey: true })).toBe(13)
  })
  it('turns lines and pages into pixels, scaled to the page', () => {
    expect(wheelDelta(0, 3, 1, size, 2)).toEqual({ dx: 0, dy: 240 })
    expect(wheelDelta(0, 1, 2, size, 1)).toEqual({ dx: 0, dy: 800 })
    expect(wheelDelta(5, 10, 0, size, 2)).toEqual({ dx: 10, dy: 20 })
  })
  it('counts double and triple clicks, and starts again after a pause or a move', () => {
    const first = { at: 0, x: 10, y: 10, button: 0, count: 1 }
    expect(clickCount(null, 0, 10, 10, 0)).toBe(1)
    expect(clickCount(first, 300, 12, 11, 0)).toBe(2)
    expect(clickCount({ ...first, count: 2 }, 300, 10, 10, 0)).toBe(3)
    expect(clickCount(first, 900, 10, 10, 0)).toBe(1)
    expect(clickCount(first, 300, 40, 10, 0)).toBe(1)
    expect(clickCount(first, 300, 10, 10, 2)).toBe(1)
  })
})

describe('touch', () => {
  it('taps where the finger went down', () => {
    const t = touchStart(10, 10)
    const [t2, out] = touchMove(t, 13, 12)
    expect(out).toEqual([])
    expect(touchEnd(t2, 13, 12)).toEqual([{ kind: 'tap', x: 10, y: 10 }])
  })
  it('scrolls the way the finger swipes', () => {
    const [t, out] = touchMove(touchStart(100, 300), 100, 200)
    expect(t.phase).toBe('scroll')
    expect(out).toEqual([{ kind: 'scroll', x: 100, y: 200, dx: 0, dy: 100 }])
    const [, more] = touchMove(t, 100, 180)
    expect(more).toEqual([{ kind: 'scroll', x: 100, y: 180, dx: 0, dy: 20 }])
    expect(touchHold(t)[1]).toEqual([])
  })
  it('drags after a long press, and lets go on lift or cancel', () => {
    const [t, down] = touchHold(touchStart(50, 60))
    expect(down).toEqual([{ kind: 'down', x: 50, y: 60 }])
    const [t2, move] = touchMove(t, 90, 60)
    expect(move).toEqual([{ kind: 'move', x: 90, y: 60 }])
    expect(touchEnd(t2, 95, 60)).toEqual([{ kind: 'up', x: 95, y: 60 }])
    expect(touchCancel(t2)).toEqual([{ kind: 'up', x: 90, y: 60 }])
    expect(touchCancel(touchStart(0, 0))).toEqual([])
  })
})

describe('action overlay', () => {
  it('names the verb and the element', () => {
    expect(actionFor(step(1, 'running', { target: 'Add to cart' }))).toEqual({ verb: 'Clicking', label: 'Add to cart', typing: undefined })
    expect(actionFor(step(1, 'running', { tool: 'browser_type', kind: 'type' }))).toEqual({ verb: 'Typing', label: undefined, typing: undefined })
    expect(actionFor(step(1, 'running', { tool: 'browser_open', kind: 'browse', host: 'emberandoak.com', target: 'x' }))).toEqual({ verb: 'Opening', label: 'emberandoak.com', typing: undefined, page: true })
  })
  it('shows typed text, but never a secret', () => {
    expect(actionFor(step(1, 'running', { tool: 'browser_type', kind: 'type', target: 'Search', text: 'burr grinder' }))?.typing).toEqual({ text: 'burr grinder', secret: false })
    expect(actionFor(step(1, 'running', { tool: 'browser_type', kind: 'type', text: 'hunter2', secret: true }))?.typing).toEqual({ text: secretMask, secret: true })
    expect(actionFor(step(1, 'running', { tool: 'browser_login', kind: 'type', text: 'hunter2' }))?.typing).toEqual({ text: secretMask, secret: true })
    expect(JSON.stringify(actionFor(step(1, 'running', { tool: 'browser_login', kind: 'type', text: 'hunter2' })))).not.toContain('hunter2')
  })
  it('says nothing for a step it can’t name', () => {
    expect(actionFor(null)).toBe(null)
    expect(actionFor(step(1, 'running', { tool: 'web_search', kind: 'search' }))).toBe(null)
  })
  it('finds the running browser step at the end of the feed only', () => {
    const say: Entry = { id: 9, kind: 'say', text: 'Hi', createdAt: 0, updatedAt: 0 }
    const running = step(2, 'running', {})
    expect(runningBrowserStep([say, running])).toBe(running)
    expect(runningBrowserStep([running, say])).toBe(running)
    expect(runningBrowserStep([running, step(3, 'done', {})])).toBe(null)
    expect(runningBrowserStep([step(4, 'running', { tool: 'web_search', kind: 'search' })])).toBe(null)
  })
})

describe('address', () => {
  it('splits the host from the rest', () => {
    expect(addressOf('https://www.emberandoak.com/grinders/hearth-64?c=1')).toEqual({ host: 'emberandoak.com', rest: '/grinders/hearth-64?c=1', secure: true, blank: false })
    expect(addressOf('http://example.org/')).toEqual({ host: 'example.org', rest: '', secure: false, blank: false })
    expect(addressOf('about:blank').blank).toBe(true)
    expect(addressOf('').blank).toBe(true)
  })
})

// M1: the head never cuts the site; what comes before it goes first.
describe('hostParts', () => {
  it('splits off the site someone registered', () => {
    expect(hostParts('news.ycombinator.com')).toEqual({ sub: 'news.', site: 'ycombinator.com' })
    expect(hostParts('secure.acmesavings.example')).toEqual({ sub: 'secure.', site: 'acmesavings.example' })
    expect(hostParts('accounts.example.com.evil.io')).toEqual({ sub: 'accounts.example.com.', site: 'evil.io' })
    expect(hostParts('bestbuy.com')).toEqual({ sub: '', site: 'bestbuy.com' })
  })

  it('keeps a country’s second level and a hosted site whole', () => {
    expect(hostParts('www.bbc.co.uk')).toEqual({ sub: 'www.', site: 'bbc.co.uk' })
    expect(hostParts('shop.amazon.com.au')).toEqual({ sub: 'shop.', site: 'amazon.com.au' })
    expect(hostParts('docs.alex.github.io')).toEqual({ sub: 'docs.', site: 'alex.github.io' })
    expect(hostParts('mallory.github.io')).toEqual({ sub: '', site: 'mallory.github.io' })
    // Two letters after an ordinary name are still just a domain.
    expect(hostParts('mail.google.de')).toEqual({ sub: 'mail.', site: 'google.de' })
  })

  it('keeps addresses, single names and ports whole', () => {
    expect(hostParts('localhost:5173')).toEqual({ sub: '', site: 'localhost:5173' })
    expect(hostParts('192.168.1.20:8080')).toEqual({ sub: '', site: '192.168.1.20:8080' })
    expect(hostParts('[::1]:8091')).toEqual({ sub: '', site: '[::1]:8091' })
    expect(hostParts('app.staging.example.com:8443')).toEqual({ sub: 'app.staging.', site: 'example.com:8443' })
  })
})
