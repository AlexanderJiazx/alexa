import { renderToStaticMarkup } from 'react-dom/server'
import { afterEach, describe, expect, it } from 'vitest'
import type { Block, BlockOf, Placement } from '../../api/blocks'
import type { Entry } from '../../api/types'
import { store } from '../../state/store'
import { BlockList, replyFailed } from './BlockList'
import { barPath, model, niceTicks, pickTicks, spread } from './chart/scale'
import { answerAfter, chosenLabel } from './context'
import { fixtures, malformedFixtures, shapeFixtures, specFixtures } from './fixtures'
import { safeURL } from './parts/Action'
import { tone } from './stats/Stats'
import { expandable, headTitle, summarize } from './summary'
import { inlineRows } from './table/Table'
import { known, valid } from './valid'

// The blocks render without a DOM, so these check the markup each one
// promises: a named group per block, the fallback for a malformed one,
// frozen replies, the chart's table, the table's rows. What needs a
// browser (carousel arrows, the lightbox, container queries) was checked
// by hand in the gallery (?gallery=blocks).

const noop = () => {}
const esc = (s: string) => s.replace(/&/g, '&amp;').replace(/"/g, '&quot;').replace(/'/g, '&#x27;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
const html = (blocks: Block[], placement: Placement = 'inline', more: { frozen?: boolean; entryId?: number; echo?: boolean } = {}) =>
  renderToStaticMarkup(<BlockList blocks={blocks} placement={placement} onReply={noop} onExpand={noop} {...more} />)
const find = (name: string) => fixtures.find((f) => f.name === name)!.block
const count = (s: string, part: string) => s.split(part).length - 1

describe('every fixture', () => {
  for (const f of [...fixtures, ...malformedFixtures])
    it(`renders ${f.name} as a group named by its summary`, () => {
      const out = html([f.block])
      const name = summarize(f.block)
      expect(name.length).toBeGreaterThan(0)
      expect(name.length).toBeLessThanOrEqual(160)
      expect(out).toContain(`role="group" aria-label="${esc(name)}"`)
      expect(out).toContain(`class="b" data-type="${f.block.type}"`)
    })

  it('covers every block type', () => {
    const types = new Set(fixtures.map((f) => f.block.type))
    expect([...types].sort()).toEqual(['callout', 'cards', 'chart', 'checklist', 'compare', 'files', 'images', 'links', 'markdown', 'receipt', 'stats', 'table', 'timeline'])
    expect(fixtures.every((f) => valid(f.block))).toBe(true)
  })

  it('renders in the panel and the report too', () => {
    for (const p of ['panel', 'report'] as const) for (const f of fixtures) expect(() => html([f.block], p)).not.toThrow()
  })
})

describe('malformed blocks', () => {
  for (const f of malformedFixtures)
    it(`${f.name} falls back to its summary`, () => {
      expect(valid(f.block)).toBe(false)
      const out = html([f.block])
      expect(out).toContain('class="b-fallback"')
    })

  it('skips what this client doesn’t know, and draws nothing for nothing', () => {
    const odd = { type: 'hologram', text: 'hi' } as unknown as Block
    expect(known(odd)).toBe(false)
    expect(known(null)).toBe(false)
    expect(html([odd])).toBe('')
    expect(html([])).toBe('')
    expect(renderToStaticMarkup(<BlockList blocks={'nope' as unknown as Block[]} placement="inline" />)).toBe('')
  })

  it('summarise in plain words, never "undefined" or an empty list', () => {
    for (const f of malformedFixtures) {
      const s = summarize(f.block)
      expect(s).not.toMatch(/undefined|null|NaN|\[object|^0 |: $/)
    }
    expect(summarize(malformedFixtures[0].block)).toBe('Hotels in Alfama')
    expect(summarize(malformedFixtures[1].block)).toBe('1 product: $279')
    expect(summarize({ type: 'compare', items: 'x' } as unknown as Block)).toBe('')
  })

  it('never writes a summary longer than 160 characters', () => {
    const long: Block = { type: 'callout', tone: 'info', text: 'word '.repeat(80) }
    expect(summarize(long).length).toBeLessThanOrEqual(160)
    expect(summarize(long).endsWith('…')).toBe(true)
  })
})

describe('replies', () => {
  const pick = specFixtures[0].block
  const said = 900001
  afterEach(() => store.set((s) => ({ ...s, data: { ...s.data, feed: s.data.feed.filter((e) => e.id < said) } })))
  const seed = (answer?: string) => {
    const feed: Entry[] = [{ id: said, kind: 'say', text: 'This is the one I’d get.', data: { blocks: [pick] }, createdAt: 0, updatedAt: 0 }]
    if (answer) feed.push({ id: said + 1, kind: 'user', text: answer, createdAt: 0, updatedAt: 0 })
    store.set((s) => ({ ...s, data: { ...s.data, feed: [...s.data.feed, ...feed] } }))
  }

  it('are buttons while the exchange is open', () => {
    seed()
    const out = html([pick], 'inline', { entryId: said })
    expect(out).toContain('Choose this')
    expect(out).not.toContain('disabled')
    expect(out).not.toContain('b-answered')
  })

  // FE-3: the label is the button's name; the tooltip says it's sent.
  it('say what a reply sends, apart from its name', () => {
    seed()
    const out = html([find('cards: three products')], 'inline', { entryId: said })
    expect(out).toContain(`title="${esc('Sends “Let’s go with the Bose QuietComfort Ultra ($299).”')}">Choose this</button>`)
  })

  it('freeze once the user answered, marking the one chosen', () => {
    seed('Let\'s go with the Sony WH-1000XM6 ($279).')
    const out = html([pick, find('cards: three products')], 'inline', { entryId: said, frozen: true })
    expect(out).toContain('class="b-chosen" role="status"')
    expect(out).toContain('Chosen')
    // The user's own message follows, so the answer isn't said again.
    expect(out).not.toContain('b-answered')
    // The other cards' replies are disabled.
    expect(count(out, 'disabled=""')).toBe(3)
  })

  it('say what was chosen where the answer isn’t the next message', () => {
    const text = (pick as BlockOf<'cards'>).items[0].action!
    if (text.do !== 'reply') throw new Error('the fixture’s first card replies')
    seed(text.text)
    const out = html([pick], 'panel', { entryId: said, frozen: true, echo: true })
    expect(out).toContain(`You chose <b>${text.label}</b>`)
    expect(chosenLabel([pick], ` ${text.text} `)).toBe(text.label)
    expect(chosenLabel([pick], 'Something else')).toBeUndefined()
  })

  it('quote an answer in the user’s own words', () => {
    seed('Actually, show me cheaper ones')
    const out = html([find('cards: three products')], 'panel', { entryId: said, echo: true })
    expect(out).toContain('You answered <q>Actually, show me cheaper ones</q>')
  })

  it('freeze when answered in the user’s own words', () => {
    seed('Actually, show me cheaper ones')
    const out = html([find('cards: three products')], 'inline', { entryId: said })
    expect(out).not.toContain('b-chosen')
    expect(count(out, 'disabled=""')).toBe(3)
  })

  it('find what the user answered after an entry', () => {
    const at = (id: number, kind: Entry['kind'], text: string): Entry => ({ id, kind, text, createdAt: 0, updatedAt: 0 })
    const feed = [at(1, 'say', 'Pick one'), at(2, 'say', 'Or this'), at(3, 'user', 'The first'), at(4, 'user', 'Thanks')]
    expect(answerAfter(feed, 1)).toBe('The first')
    expect(answerAfter(feed, 3)).toBe('Thanks')
    expect(answerAfter(feed, 4)).toBeUndefined()
    expect(answerAfter(feed, undefined)).toBeUndefined()
    // A new request that came after isn't an answer to the blocks.
    const later = [at(1, 'say', 'Pick one'), { ...at(2, 'user', 'Plan a trip to Lisbon'), data: { start: true } }]
    expect(answerAfter(later, 1)).toBeUndefined()
  })

  it('only ever open web links', () => {
    expect(safeURL('https://www.bestbuy.com/x')).toBe('https://www.bestbuy.com/x')
    expect(safeURL('javascript:alert(1)')).toBeUndefined()
    expect(safeURL('file:///etc/passwd')).toBeUndefined()
    expect(safeURL('not a url')).toBeUndefined()
    const bad: Block = { type: 'cards', kind: 'item', items: [{ title: 'Trap', action: { do: 'open', label: 'Open', url: 'javascript:alert(1)' } }] }
    expect(html([bad])).not.toContain('javascript:')
  })

  // FH-11: a reply tapped inside a confirm card, sent by the card, stayed
  // "sent" when it couldn't go. Whoever sends it, a failure frees the
  // buttons.
  it('come back when the reply couldn’t go, whoever sent it', async () => {
    expect(await replyFailed(Promise.resolve(false))).toBe(true)
    expect(await replyFailed(Promise.resolve(true))).toBe(false)
    // The gallery's and the tests' stand-ins send nothing.
    expect(await replyFailed(undefined)).toBe(false)
  })
})

describe('cards', () => {
  it('lay out by count and placement', () => {
    expect(html([specFixtures[0].block])).toContain('class="b-cards one"')
    expect(html([find('cards: three products')])).toContain('class="b-cards grid"')
    const six = find('cards: six products (carousel)')
    expect(html([six])).toContain('class="b-track"')
    expect(html([six], 'panel')).toContain('class="b-cards grid"')
    expect(html([find('cards: a flight')])).toContain('class="b-cards stack"')
  })

  it('mark Alexa’s pick and show what a price was', () => {
    const out = html([specFixtures[0].block])
    expect(out).toContain('Alexa’s pick')
    expect(out).toContain('−15%')
    expect(out).toContain('<span class="sr">was </span>')
  })

  it('count a stay’s nights and give a place directions', () => {
    expect(html([find('cards: a stay')])).toMatch(/\d nights/)
    expect(html([find('cards: places')])).toContain('https://www.google.com/maps/dir/')
  })

  it('show a flight that lands the next day', () => {
    expect(html([find('cards: a flight with a connection')])).toContain('<sup>+1<span class="sr"> day</span></sup>')
  })
})

describe('table', () => {
  it('shows 8 rows, then Show all', () => {
    const b = find('table: 23 rows (a report)') as BlockOf<'table'>
    const out = html([b])
    expect(count(out, '<tr>')).toBe(inlineRows + 1)
    expect(out).toContain(`Show all ${b.rows.length} rows`)
    expect(out).toContain('aria-expanded="false"')
  })

  it('shows every row in the panel, where Expand puts it', () => {
    const b = find('table: 23 rows (a report)') as BlockOf<'table'>
    const out = html([b], 'panel')
    expect(count(out, '<tr>')).toBe(b.rows.length + 1)
    expect(out).toContain('aria-expanded="true"')
  })

  it('right-aligns numbers in tabular figures', () => {
    const out = html([find('table: prices by store')])
    expect(out).toContain('scope="row"')
    expect(out).toMatch(/<td class="a-right num">\$\d/)
    expect(out).toContain('<th scope="col" class="a-left">')
  })
})

describe('chart', () => {
  it('reads out as a table', () => {
    const b = find('chart: three series, with a mark') as BlockOf<'chart'>
    const out = html([b])
    expect(out).toContain('<table class="sr"><caption>')
    expect(out).toContain('aria-hidden="true"')
    // One row per x, after the header.
    const sr = out.slice(out.indexOf('<table class="sr">'))
    expect(count(sr.slice(0, sr.indexOf('</table>')), '<tr>')).toBe(b.series[0].points.length + 1)
  })

  it('chooses round ticks', () => {
    expect(niceTicks(405, 512)).toEqual([400, 450, 500, 550])
    expect(niceTicks(0, 1.2)).toEqual([0, 0.5, 1, 1.5])
    expect(niceTicks(-38, 180).length).toBeLessThanOrEqual(5)
    expect(niceTicks(0, 60)).toEqual([0, 20, 40, 60])
    expect(niceTicks(5, 5).length).toBeGreaterThan(1)
    expect(niceTicks(NaN, 1)).toEqual([0, 1])
  })

  it('stands bars in bands from zero', () => {
    const m = model(find('chart: bars, two series') as BlockOf<'chart'>)
    expect(m.ordinal).toBe(true)
    expect(m.keys).toEqual(['Sony', 'Bose', 'Sennheiser', 'Apple'])
    expect(m.lo).toBe(0)
    const t = model(find('chart: three series, with a mark') as BlockOf<'chart'>)
    expect(t.time).toBe(true)
    expect(t.ordinal).toBe(false)
    expect(t.xs).toEqual([...t.xs].sort((a, b) => a - b))
  })

  it('rounds a bar only at its free end', () => {
    // Up from the axis at 100 to 40: square at the bottom, rounded at the top.
    expect(barPath(10, 20, 40, 100, 4)).toBe('M10 100V44Q10 40 14 40H26Q30 40 30 44V100Z')
    // Down from the axis: rounded at the bottom.
    expect(barPath(10, 20, 140, 100, 4)).toBe('M10 100V136Q10 140 14 140H26Q30 140 30 136V100Z')
    // Too short to round: a plain rectangle.
    expect(barPath(0, 10, 100, 100)).toBe('M0 100V100H10V100Z')
  })

  it('keeps direct labels apart and in bounds', () => {
    const out = spread([100, 102, 104], 14, 0, 200)
    expect(out[1] - out[0]).toBeGreaterThanOrEqual(14)
    expect(out[2] - out[1]).toBeGreaterThanOrEqual(14)
    const top = spread([195, 198], 14, 0, 200)
    expect(Math.max(...top)).toBeLessThanOrEqual(200)
    expect(Math.abs(top[0] - top[1])).toBeGreaterThanOrEqual(14)
    expect(pickTicks(12, 4)).toEqual([0, 4, 7, 11])
    expect(pickTicks(3, 5)).toEqual([0, 1, 2])
  })
})

describe('the rest', () => {
  it('colours a change by whether it’s good', () => {
    expect(tone({ trend: 'down', good: 'down' })).toBe('ok')
    expect(tone({ trend: 'up', good: 'down' })).toBe('bad')
    expect(tone({ trend: 'flat', good: 'down' })).toBe('neutral')
    expect(tone({ trend: 'up' })).toBe('neutral')
  })

  it('offers Expand for compare, table and timeline in the exchange only', () => {
    const cmp = specFixtures[1].block
    expect(expandable(cmp)).toBe(true)
    expect(headTitle(cmp)).toBe('Top 3')
    expect(headTitle({ ...cmp, title: undefined } as Block)).toBe('Side by side')
    expect(html([cmp], 'inline', { entryId: 1 })).toContain('aria-label="Expand"')
    expect(html([cmp], 'panel', { entryId: 1 })).not.toContain('aria-label="Expand"')
    expect(html([specFixtures[0].block], 'inline', { entryId: 1 })).not.toContain('aria-label="Expand"')
  })

  it('marks the best cell in a comparison', () => {
    expect(html([specFixtures[1].block])).toContain('(best)')
  })

  it('offers the calendar for a timed itinerary', () => {
    expect(html([specFixtures[4].block], 'inline', { entryId: 7 })).toContain('/api/entries/7/calendar.ics')
  })

  it('writes a receipt’s total and status', () => {
    const out = html([find('receipt: cancelled, in euros')])
    expect(out).toContain('Cancelled')
    expect(out).toContain('€')
    expect(html([specFixtures[3].block])).toContain('Total to pay')
  })

  it('keeps every shape fixture valid', () => {
    expect(shapeFixtures.every((f) => valid(f.block))).toBe(true)
  })
})
