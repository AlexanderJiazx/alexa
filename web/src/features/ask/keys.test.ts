import { describe, expect, it } from 'vitest'
import { askKey, enterIsFree, keyOwner, type KeyIn, type KeyState } from './keys'

// The ask's keys (WP-10): 1 to 4 answer, Enter takes the recommendation or
// finishes a multi-select, a letter starts a typed answer, and nothing
// fires inside a field, under a modifier or on a confirm card.

const k = (key: string, more: Partial<KeyIn> = {}): KeyIn => ({ key, inField: false, ownEnter: false, mod: false, composing: false, ...more })
const choice: KeyState = { kind: 'choice', count: 3, recommended: 1, picked: 0 }

describe('askKey', () => {
  it('answers with 1 to the number of options', () => {
    expect(askKey(k('1'), choice)).toEqual({ do: 'pick', index: 0 })
    expect(askKey(k('3'), choice)).toEqual({ do: 'pick', index: 2 })
    expect(askKey(k('4'), choice)).toBeNull()
    expect(askKey(k('5'), { ...choice, count: 6 })).toBeNull()
  })
  it('takes the recommended option on Enter, and only that', () => {
    expect(askKey(k('Enter'), choice)).toEqual({ do: 'pick', index: 1 })
    expect(askKey(k('Enter'), { ...choice, recommended: undefined })).toBeNull()
    // A focused button, link or anything else focusable handles its own Enter.
    expect(askKey(k('Enter', { ownEnter: true }), choice)).toBeNull()
    expect(askKey(k('Enter', { ownEnter: true }), { kind: 'multi', count: 3, picked: 2 })).toBeNull()
  })
  it('leaves / to the composer shortcut, which focuses it without typing', () => {
    expect(askKey(k('/'), choice)).toBeNull()
    expect(askKey(k('/'), { kind: 'free', count: 0, picked: 0 })).toBeNull()
  })
  it('never binds Escape', () => {
    expect(askKey(k('Escape'), choice)).toBeNull()
  })
  it('stays out of fields, modifiers and IME composition', () => {
    expect(askKey(k('1', { inField: true }), choice)).toBeNull()
    expect(askKey(k('1', { mod: true }), choice)).toBeNull()
    expect(askKey(k('1', { composing: true }), choice)).toBeNull()
    expect(askKey(k('Enter', { inField: true }), choice)).toBeNull()
  })
  it('gives a confirm card and a form no shortcuts', () => {
    expect(askKey(k('1'), { ...choice, kind: 'confirm' })).toBeNull()
    expect(askKey(k('Enter'), { ...choice, kind: 'confirm', recommended: 0 })).toBeNull()
    expect(askKey(k('a'), { ...choice, kind: 'confirm' })).toBeNull()
    expect(askKey(k('2'), { ...choice, kind: 'form' })).toBeNull()
  })
  it('toggles on a multi-select and finishes with Enter once something is picked', () => {
    const multi: KeyState = { kind: 'multi', count: 4, picked: 0 }
    expect(askKey(k('2'), multi)).toEqual({ do: 'toggle', index: 1 })
    expect(askKey(k('Enter'), multi)).toBeNull()
    expect(askKey(k('Enter'), { ...multi, picked: 2 })).toEqual({ do: 'done' })
  })
  it('sends a letter on to the composer', () => {
    expect(askKey(k('s'), choice)).toEqual({ do: 'type' })
    expect(askKey(k('S'), { kind: 'free', count: 0, picked: 0 })).toEqual({ do: 'type' })
    expect(askKey(k(' '), choice)).toBeNull()
    expect(askKey(k('ArrowDown'), choice)).toBeNull()
    expect(askKey(k('0'), choice)).toBeNull()
  })
})

// FH-4: Enter on a focused link answered the open question with its
// recommended option, and the link didn't open. Enter is the ask's only
// while the page or the card itself has focus.
describe('enterIsFree', () => {
  const body = { tagName: 'BODY' }
  const html = { tagName: 'HTML' }
  const card = { tagName: 'SECTION', className: 'ask' }
  const group = { tagName: 'DIV', role: 'group', tabIndex: -1 }
  const free = [body, html, card, group]

  it('takes Enter with nothing focused, or the card itself', () => {
    expect(enterIsFree(null, free)).toBe(true)
    expect(enterIsFree(body, free)).toBe(true)
    expect(enterIsFree(html, free)).toBe(true)
    expect(enterIsFree(card, free)).toBe(true)
    expect(enterIsFree(group, free)).toBe(true)
  })
  it('leaves Enter to a link, a control or anything else that has focus', () => {
    for (const el of [
      { tagName: 'A', href: 'https://example.com' },
      { tagName: 'DIV', role: 'link', tabIndex: 0 },
      { tagName: 'SPAN', role: 'button', tabIndex: 0 },
      { tagName: 'BUTTON' },
      { tagName: 'SUMMARY' },
      { tagName: 'INPUT', type: 'checkbox' },
      { tagName: 'SELECT' },
      { tagName: 'DIV', contentEditable: 'true' },
      // A carousel track in the card's blocks, or a scroller elsewhere.
      { tagName: 'UL', tabIndex: 0 },
    ])
      expect(enterIsFree(el, free)).toBe(false)
  })
  it('turns into no answer on a focused link', () => {
    const link = { tagName: 'A', href: 'https://example.com' }
    expect(askKey(k('Enter', { ownEnter: !enterIsFree(link, free) }), choice)).toBeNull()
    expect(askKey(k('Enter', { ownEnter: !enterIsFree(body, free) }), choice)).toEqual({ do: 'pick', index: 1 })
  })
})

describe('keyOwner', () => {
  const card = (name: string, late = false, holds = false) => ({ name, late, holds })
  it('gives the keys to the newest card', () => {
    expect(keyOwner([card('a'), card('b')])?.name).toBe('b')
  })
  it('keeps them on the live question when a skipped one is opened again', () => {
    // B is open; the user opened A, skipped earlier, with Answer now.
    expect(keyOwner([card('B'), card('A', true)])?.name).toBe('B')
  })
  it('lets a skipped question answer while focus is inside it', () => {
    expect(keyOwner([card('B'), card('A', true, true)])?.name).toBe('A')
    expect(keyOwner([card('A', true, true)])?.name).toBe('A')
  })
  it('gives a skipped question alone no keys until it has focus', () => {
    expect(keyOwner([card('A', true)])).toBeUndefined()
    expect(keyOwner([])).toBeUndefined()
  })
  it('follows focus into an older live card', () => {
    expect(keyOwner([card('a', false, true), card('b')])?.name).toBe('a')
  })
})
