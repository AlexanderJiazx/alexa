import { describe, expect, it } from 'vitest'
import { csp, withCSP, withHead, withoutTitle } from './html'

const meta = `<meta http-equiv="Content-Security-Policy" content="${csp}">`

describe('withHead (FE-05)', () => {
  it('puts the policy first, in a head of its own, before a page that starts with <header>', () => {
    const page = '<header><h1>Fares</h1></header><p>Hi</p><script>fetch("https://x.example")</script>'
    const out = withCSP(page)
    expect(out.startsWith(`<!doctype html><head>${meta}</head>`)).toBe(true)
    expect(out.indexOf(meta)).toBeLessThan(out.indexOf('<header>'))
    // The page itself is untouched, after the prelude.
    expect(out.endsWith(page)).toBe(true)
    // There's exactly one policy, and it isn't inside the <header>.
    expect(out.split('Content-Security-Policy').length).toBe(2)
    expect(out.slice(out.indexOf('<header>'))).not.toContain('Content-Security-Policy')
  })

  it('comes before a full document’s own doctype and head', () => {
    const page = '<!DOCTYPE html><html lang="pt"><head><title>T</title><script>evil()</script></head><body>x</body></html>'
    const out = withHead(page, '<style id="alexa-theme"></style>')
    expect(out.indexOf(meta)).toBe('<!doctype html><head>'.length)
    expect(out.indexOf(meta)).toBeLessThan(out.indexOf('<script>evil'))
    expect(out.indexOf('<style id="alexa-theme">')).toBeLessThan(out.indexOf('<!DOCTYPE html><html'))
  })

  it('can’t be pushed out by a "<head>" in a comment or a script', () => {
    for (const page of ['<!-- <head> --><script>steal()</script>', '<script>var s = "<head>"; steal()</script>', '<HEAD data-x="<head>">']) {
      const out = withCSP(page)
      expect(out.startsWith(`<!doctype html><head>${meta}`)).toBe(true)
    }
  })

  it('starts in standards mode even when the page has no doctype', () => {
    expect(withCSP('<p>hi</p>').toLowerCase().startsWith('<!doctype html>')).toBe(true)
  })
})

describe('withoutTitle', () => {
  it('drops a first heading that repeats the title', () => {
    expect(withoutTitle('# Flights to Lisbon\n\nBody', 'Flights to Lisbon')).toBe('Body')
    expect(withoutTitle('## Lisbon flights under $400\n\nBody', 'Flights to Lisbon under $400')).toBe('Body')
  })

  it('keeps a first heading about something else', () => {
    expect(withoutTitle('## Getting there\n\nBody', 'A weekend in Lisbon')).toBe('## Getting there\n\nBody')
  })
})
