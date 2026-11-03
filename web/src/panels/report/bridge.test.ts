import { describe, expect, it } from 'vitest'
import { checkSend, clampHeight, frameMessage, frameScript, prelude, sendGap, themeCSS } from './bridge'

describe('checkSend (alexa.send)', () => {
  it('refuses a send without a tap or click', () => {
    expect(checkSend('Book it', false, 10_000, -Infinity)).toEqual({ ok: false, error: 'alexa.send needs a tap or click' })
  })

  it('lets one through a burst of five', () => {
    let last = -Infinity
    const sent: string[] = []
    for (let i = 0; i < 5; i++) {
      const now = 10_000 + i * 100
      const r = checkSend(`Book option ${i + 1}`, true, now, last)
      if (r.ok) {
        last = now
        sent.push(r.text)
      }
    }
    expect(sent).toEqual(['Book option 1'])
  })

  it('lets another through once 2 seconds have passed', () => {
    expect(checkSend('Again', true, 10_000 + sendGap - 1, 10_000).ok).toBe(false)
    expect(checkSend('Again', true, 10_000 + sendGap, 10_000).ok).toBe(true)
  })

  it('takes 1 to 300 characters of text, tidied', () => {
    expect(checkSend('  Book\n the   Memmo  ', true, 0, -Infinity)).toEqual({ ok: true, text: 'Book the Memmo' })
    expect(checkSend('x'.repeat(300), true, 0, -Infinity).ok).toBe(true)
    expect(checkSend('x'.repeat(301), true, 0, -Infinity)).toEqual({ ok: false, error: 'Keep it under 300 characters' })
    expect(checkSend('   ', true, 0, -Infinity).ok).toBe(false)
    expect(checkSend({ text: 'sneaky' }, true, 0, -Infinity).ok).toBe(false)
  })
})

describe('frameMessage', () => {
  it('reads heights and sends, and nothing else', () => {
    expect(frameMessage({ type: 'alexa:height', height: 1200 })).toEqual({ type: 'alexa:height', height: 1200 })
    expect(frameMessage({ type: 'alexa:send', id: 3, text: 'Hi' })).toEqual({ type: 'alexa:send', id: 3, text: 'Hi' })
    expect(frameMessage({ type: 'alexa:height', height: 'tall' })).toBeNull()
    expect(frameMessage({ type: 'alexa:height', height: Infinity })).toBeNull()
    expect(frameMessage({ type: 'gallery:height', height: 10 })).toBeNull()
    expect(frameMessage('alexa:send')).toBeNull()
    expect(frameMessage(null)).toBeNull()
  })
})

describe('the frame', () => {
  it('clamps the page’s height to 160–8000px', () => {
    expect(clampHeight(20)).toBe(160)
    expect(clampHeight(1234.2)).toBe(1235)
    expect(clampHeight(90_000)).toBe(8000)
  })

  it('writes the theme as one :root rule, refusing values that could end it', () => {
    const css = themeCSS({ scheme: 'dark', vars: { '--ink': 'rgb(238, 238, 234)', '--bad name': 'x', '--line': 'red}body{display:none' } })
    expect(css).toBe(':root{color-scheme:dark;--ink:rgb(238, 238, 234)}')
  })

  it('carries the policy-safe bridge: no closing script tag, the height and send messages', () => {
    expect(frameScript).not.toMatch(/<\/script/i)
    expect(frameScript).toContain("type: 'alexa:height'")
    expect(frameScript).toContain("type: 'alexa:send'")
    expect(frameScript).toContain('navigator.userActivation')
    const p = prelude({ scheme: 'light', vars: { '--ink': 'black' } }, 'body{}</style><script>x()</script>')
    // The base styles can't close their own <style>.
    expect(p).not.toContain('</style><script>x()')
    expect(p.indexOf('id="alexa-base"')).toBeLessThan(p.indexOf('id="alexa-theme"'))
  })
})
