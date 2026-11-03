import { describe, expect, it } from 'vitest'
import type { Report } from '../../api/types'
import { hn } from './fixtures'
import { clip, copyFailure, eventWhen, excerpt, meta, parts, plain, readMinutes, sectionCount, setTitle, splitLede, wordCount } from './text'

describe('splitLede', () => {
  it('takes a first paragraph of plain prose', () => {
    const { lede, rest } = splitLede(hn.content!)
    expect(lede).toBe('Snapshot taken from https://news.ycombinator.com front page. Points/comments move fast – these are as-seen when opened.')
    expect(rest.startsWith('## 1. Binance')).toBe(true)
  })

  it('leaves headings, lists, tables and short lines alone', () => {
    for (const md of ['## Prices\n\nBody text here.', '- one\n- two', '| a | b |\n| - | - |', 'Hi there.\n\nMore.', 'Title of it all here\n===\n\nBody']) {
      expect(splitLede(md)).toEqual({ lede: '', rest: md })
    }
  })

  it('takes a lede that is the whole text', () => {
    expect(splitLede('Prices are for two nights, with breakfast.')).toEqual({ lede: 'Prices are for two nights, with breakfast.', rest: '' })
  })
})

describe('words and reading time', () => {
  it('reads markdown as words', () => {
    expect(plain('## Hi\n- **Link:** [Mistral](https://mistral.ai) and `code`')).toBe('Hi Link: Mistral and code')
    expect(wordCount('')).toBe(0)
  })

  it('counts the Hacker News report as five sections and a couple of minutes', () => {
    expect(sectionCount(hn.content!)).toBe(5)
    expect(readMinutes(wordCount(hn.content!))).toBe(2)
    expect(readMinutes(40)).toBe(0)
  })

  it('keeps a spaced dash with the words before it', () => {
    expect(setTitle('Hacker News Top 5 – Points & More')).toBe('Hacker News Top 5\u00a0– Points & More')
    expect(setTitle('Well-known')).toBe('Well-known')
  })

  it('clips at a word', () => {
    expect(clip('The quick brown fox jumps over', 18)).toBe('The quick brown…')
  })
})

const report = (r: Partial<Report>): Report => ({ id: 'r', title: 'T', format: 'markdown', createdAt: 0, updatedAt: 0, ...r })

describe('the card’s lines', () => {
  it('prefers the takeaway, then the lede', () => {
    expect(excerpt('Get the Sony.', hn)).toBe('Get the Sony.')
    expect(excerpt(undefined, report({ data: { takeaway: 'Memmo is the pick.' } }))).toBe('Memmo is the pick.')
    expect(excerpt(undefined, hn)).toBe('Snapshot taken from news.ycombinator.com front page. Points/comments move fast – these are as-seen when opened.')
    expect(excerpt(undefined, null)).toBe('')
  })

  it('says what’s inside', () => {
    expect(meta(hn)).toBe('5 sections · 2 min read')
    expect(meta(report({ format: 'html' }))).toBe('Interactive page')
    expect(meta(report({ data: { blocks: [] } }), '3 options, comparison')).toBe('3 options, comparison')
    expect(meta(null, 'table')).toBe('table')
  })
})

describe('parts', () => {
  it('puts the content first, then the blocks, with markdown blocks as prose between runs', () => {
    const p = parts('Intro', [
      { type: 'stats', items: [{ label: 'A', value: '1' }] },
      { type: 'callout', tone: 'tip', text: 'x' },
      { type: 'markdown', text: 'Between' },
      { type: 'links', items: [{ title: 'L', url: 'https://x.example' }] },
    ])
    expect(p.map((x) => (x.kind === 'md' ? x.text : x.blocks.map((b) => b.type).join('+')))).toEqual(['Intro', 'stats+callout', 'Between', 'links'])
    expect(parts('', undefined)).toEqual([])
  })
})

describe('eventWhen', () => {
  it('puts a timed event in the user’s zone', () => {
    const w = eventWhen({ title: 'Dinner', start: '2026-11-14T20:00:00Z', end: '2026-11-14T22:00:00Z' }, 'Europe/Lisbon')!
    expect(w.day).toBe('14')
    expect(w.line).toMatch(/^Sat · 8:00\s?–\s?10:00\sPM$/)
  })

  it('keeps a date-only event on its day, whatever the zone', () => {
    const w = eventWhen({ title: 'Sintra', start: '2026-11-15' }, 'America/Los_Angeles')!
    expect([w.month, w.day, w.line]).toEqual(['Nov', '15', 'Sun · All day'])
  })

  it('gives nothing for a date it can’t read', () => {
    expect(eventWhen({ title: 'x', start: 'soon' })).toBeNull()
  })
})

// D8: a failed Copy speaks plainly, never in the browser's exception text.
describe('copyFailure', () => {
  it('points a blocked clipboard at Download', () => {
    const denied = Object.assign(new Error("Failed to execute 'write' on 'Clipboard': Write permission denied."), { name: 'NotAllowedError' })
    expect(copyFailure(denied, true)).toBe('Your browser blocked copying. Use Download instead.')
    expect(copyFailure(new TypeError("Cannot read properties of undefined (reading 'writeText')"), false)).toBe('Your browser blocked copying. Use Download instead.')
  })

  it('never passes the exception on', () => {
    for (const e of [new TypeError('Failed to fetch'), new Error('Bad Gateway'), null, 'x']) {
      const out = copyFailure(e, true)
      expect(out).toBe('Something went wrong. Try again, or use Download instead.')
    }
  })
})
