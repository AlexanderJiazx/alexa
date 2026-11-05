import type { Block, BlockOf } from '../../api/blocks'
import { money } from '../../lib/format'

// summarize describes a block in a line, as the server's summarizeBlocks
// does (internal/agent/blocks_text.go): the block's accessible name, and
// what shows in its place when it can't be drawn. It never throws, even on
// a block that doesn't match its type.

const nouns: Record<string, [string, string]> = {
  product: ['product', 'products'],
  place: ['place', 'places'],
  stay: ['stay', 'stays'],
  flight: ['flight', 'flights'],
  event: ['event', 'events'],
  item: ['option', 'options'],
}

const plural = (n: number, one: string, many = `${one}s`) => `${n} ${n === 1 ? one : many}`
const cap = (s: string) => s.charAt(0).toUpperCase() + s.slice(1)
export const clip = (s: string, n = 160) => (s.length <= n ? s : `${s.slice(0, n - 1).trimEnd()}…`)

// The summary has to read well for a block that doesn't match its type,
// because that's exactly when it shows in the block's place. So every
// field goes through these: a list that isn't one is empty, and text that
// isn't text is left out rather than printed as "undefined".
type Loose = Record<string, unknown>
const arr = (v: unknown): Loose[] => (Array.isArray(v) ? v.filter((x): x is Loose => !!x && typeof x === 'object') : [])
const txt = (v: unknown) => (typeof v === 'string' ? v.trim() : typeof v === 'number' && Number.isFinite(v) ? String(v) : '')
const cash = (m: unknown) => {
  const o = (m && typeof m === 'object' ? m : {}) as Loose
  return typeof o.amount === 'number' && Number.isFinite(o.amount) && txt(o.currency) ? money(o.amount, txt(o.currency)) : ''
}
const join = (parts: string[], sep = ' ') => parts.filter(Boolean).join(sep)
// list is "3 options: a, b, c", just "3 options" when none has a name, and
// nothing for none.
const list = (n: number, [one, many]: [string, string], names: string[]) => (n ? join([plural(n, one, many), names.filter(Boolean).join(', ')], ': ') : '')

function one(b: Block): string {
  const o = b as unknown as Loose
  switch (b.type) {
    case 'cards': {
      const cards = arr(o.items)
      const items = cards.map((c) => join([txt(c.title), cash(c.price), c.pick && txt(c.title) ? '(pick)' : '']))
      if (cards.length === 1 && txt(cards[0].title)) return items[0]
      return list(items.length, nouns[txt(o.kind) || 'item'] ?? nouns.item, items)
    }
    case 'compare': {
      const names = arr(o.items)
        .map((i) => join([txt(i.name), txt(i.name) && i.pick ? '(pick)' : '']))
        .filter(Boolean)
      return names.length ? join([`Comparison of ${names.join(', ')}`, txt(o.verdict)], '. ') : ''
    }
    case 'table': {
      const what = txt(o.title) || (Array.isArray(o.columns) ? o.columns.map(txt).filter(Boolean).join(', ') : '')
      return `Table: ${join([what, plural(Array.isArray(o.rows) ? o.rows.length : 0, 'row')], ', ')}`
    }
    case 'stats':
      return arr(o.items)
        .map((s) => join([txt(s.label), txt(s.value), txt(s.delta) && `(${txt(s.delta)})`]))
        .filter(Boolean)
        .join('; ')
    case 'chart':
      return join([`${cap(txt(o.style) || 'line')} chart`, txt(o.title) || arr(o.series).map((s) => txt(s.name)).filter(Boolean).join(', ')], ': ')
    case 'timeline': {
      const items = arr(o.days).flatMap((d) => arr(d.items).map((it) => join([txt(it.time), txt(it.title)])))
      return join([txt(o.title) || 'Itinerary', items.filter(Boolean).join('; ')], ': ')
    }
    case 'checklist': {
      const items = arr(o.items)
      const done = items.filter((i) => i.status === 'done').length
      const failed = items.filter((i) => i.status === 'failed').length
      return `${txt(o.title) || 'Checklist'}: ${done} of ${items.length} done${failed ? `, ${failed} failed` : ''}`
    }
    case 'receipt': {
      const total = cash(o.total)
      return join([`${cap(txt(o.status) || 'draft')} receipt`, txt(o.merchant) && `from ${txt(o.merchant)}`]) + (total ? `: ${total}` : '') + (txt(o.ref) ? `, ref ${txt(o.ref)}` : '')
    }
    case 'links':
      return list(arr(o.items).length, ['link', 'links'], arr(o.items).map((i) => txt(i.title)))
    case 'images':
      return list(arr(o.items).length, ['image', 'images'], arr(o.items).map((i) => txt(i.caption)))
    case 'files': {
      const files = Array.isArray(o.items) ? o.items.map(txt).filter(Boolean) : []
      return list(files.length, ['file', 'files'], files.map((f) => f.split('/').pop() || f))
    }
    case 'callout':
      return txt(o.text)
    case 'markdown':
      return txt(o.text).split('\n')[0].replace(/^#+\s*/, '').replace(/[*_`]/g, '')
  }
  return ''
}

export function summarize(b: Block): string {
  const title = txt((b as unknown as Loose | undefined)?.title)
  try {
    return clip(one(b) || title)
  } catch {
    return clip(title)
  }
}

// noun names a block's type in a sentence: "Couldn’t show this comparison".
export function noun(b: Block) {
  const n: Record<string, string> = {
    cards: 'list',
    compare: 'comparison',
    table: 'table',
    stats: 'summary',
    chart: 'chart',
    timeline: 'itinerary',
    checklist: 'checklist',
    receipt: 'receipt',
    links: 'list of links',
    images: 'gallery',
    files: 'list of files',
    callout: 'note',
    markdown: 'text',
  }
  return n[b?.type] ?? 'part'
}

// headTitle is a block's heading: its own title, or a name for the kinds
// that always show a heading (they carry the Expand button).
export function headTitle(b: Block): string {
  if (b.title) return b.title
  if (b.type === 'compare') return 'Side by side'
  if (b.type === 'timeline') return 'Itinerary'
  return ''
}

export type Expandable = BlockOf<'compare'> | BlockOf<'table'> | BlockOf<'timeline'>
export const expandable = (b: Block): b is Expandable => b.type === 'compare' || b.type === 'table' || b.type === 'timeline'
