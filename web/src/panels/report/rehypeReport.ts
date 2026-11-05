import type { Element, ElementContent, Root, Text } from 'hast'

// rehypeReport gives a report's markdown the shape of an article
// (review-visual 5.7). It works on the HTML tree, before React sees it, so
// the renderer stays a plain component map:
// - a list where every item starts with "**Label:**" becomes definition rows;
// - an h2 that starts with "1." gets its number in a hanging gutter;
// - a link whose text is its address becomes a link pill (favicon and site),
//   and so does a bare domain after a Link, URL or Site label;
// - a table gets a scrolling wrapper, and its numeric columns are marked so
//   they align right in tabular figures; a table of short cells is marked
//   too, so it scrolls sideways on a phone instead of breaking every cell;
// - a row that ranks things as a chain ("A (1277) > B (950) > C (222)")
//   becomes a list of bars, the leader in ink.

type Parent = Root | Element

const el = (tagName: string, className: string[] | null, children: ElementContent[]): Element => ({
  type: 'element',
  tagName,
  properties: className ? { className } : {},
  children,
})
const txt = (value: string): Text => ({ type: 'text', value })

// text is an element's text content.
export function text(n: ElementContent | Root): string {
  if (n.type === 'text') return n.value
  if (n.type === 'element' || n.type === 'root') return n.children.map((c) => text(c as ElementContent)).join('')
  return ''
}

const blank = (n: ElementContent) => n.type === 'text' && !n.value.trim()
const isEl = (n: ElementContent | undefined, tag: string): n is Element => n?.type === 'element' && n.tagName === tag

export default function rehypeReport() {
  return (tree: Root) => walk(tree)
}

function walk(parent: Parent) {
  for (let i = 0; i < parent.children.length; i++) {
    const n = parent.children[i]
    if (n.type !== 'element') continue
    if (n.tagName === 'ul') {
      const dl = definitions(n)
      if (dl) {
        parent.children[i] = dl
        walk(dl)
        continue
      }
    }
    if (n.tagName === 'table') {
      walk(n)
      const cells = cellsOf(n)
      numericColumns(cells)
      if (cells.every((r) => r.every((c) => text(c).trim().length <= shortCell))) n.properties = { ...n.properties, className: ['short'] }
      parent.children[i] = el('div', ['table-wrap'], [n])
      continue
    }
    if (n.tagName === 'h2') numbered(n)
    if (n.tagName === 'a') pill(n)
    walk(n)
  }
}

// ---------- definition rows ----------

// Labels that introduce an address, so a bare domain after them is a link.
const linkLabel = /^(links?|urls?|site|website|web|homepage|home page|source|repo|github|page|address)$/i
// A web address at the start of some text: an optional scheme, a host with
// a dot, an optional path.
const site = /^(https?:\/\/)?((?:[a-z0-9](?:[a-z0-9-]*[a-z0-9])?\.)+[a-z]{2,})(:\d{2,5})?(\/[^\s()<>"]*)?/i

type Def = { label: string; content: ElementContent[] }

// definition reads one list item as "**Label:** value", or returns null.
function definition(li: Element): Def | null {
  const kids = li.children.filter((k) => !blank(k))
  // A loose list wraps each item in a paragraph; the label is in the first.
  const first = kids[0]
  const box = isEl(first, 'p') ? first : null
  const inline = box ? box.children : li.children
  const at = inline.findIndex((k) => !blank(k))
  const lead = inline[at]
  if (!isEl(lead, 'strong')) return null
  let label = text(lead).trim()
  let after = inline.slice(at + 1)
  if (label.endsWith(':')) label = label.slice(0, -1).trim()
  else {
    const next = after[0]
    if (next?.type !== 'text' || !next.value.startsWith(':')) return null
    after = [txt(next.value.slice(1)), ...after.slice(1)]
  }
  if (!label || label.length > 24) return null
  // The value starts after the label's space.
  if (after[0]?.type === 'text') after = [txt(after[0].value.replace(/^\s+/, '')), ...after.slice(1)]
  if (linkLabel.test(label)) after = linkSite(after)
  // A ranking alone in its row is drawn as bars instead of a sentence.
  const ranked = !box || kids.length === 1 ? rankingOf(after) : null
  if (ranked) return { label, content: [ranked] }
  const value = box ? [el('p', null, after), ...kids.slice(1)] : after
  return { label, content: value }
}

// definitions turns a list of "**Label:** value" items into a <dl> of rows,
// when every item has that shape and there are at least two.
function definitions(ul: Element): Element | null {
  const items = ul.children.filter((k): k is Element => isEl(k, 'li'))
  if (items.length < 2 || items.length !== ul.children.filter((k) => !blank(k)).length) return null
  const defs = items.map(definition)
  if (defs.some((d) => !d)) return null
  return el(
    'dl',
    ['defs'],
    (defs as Def[]).map((d) => el('div', ['def'], [el('dt', null, [txt(d.label)]), el('dd', null, d.content)])),
  )
}

// linkSite links a bare domain at the start of a value: "mistral.ai/news"
// after "Link:".
function linkSite(content: ElementContent[]): ElementContent[] {
  const first = content[0]
  if (first?.type !== 'text') return content
  const m = first.value.match(site)
  if (!m) return content
  const raw = m[0].replace(/[.,;:!?]+$/, '')
  const href = /^https?:\/\//i.test(raw) ? raw : `https://${raw}`
  const a: Element = { type: 'element', tagName: 'a', properties: { href, className: ['pill'] }, children: [txt(raw)] }
  const rest = first.value.slice(raw.length)
  return [a, ...(rest ? [txt(rest)] : []), ...content.slice(1)]
}

// ---------- rankings ----------

export type Rank = { name: string; value: number; shown: string }

// One link of a ranking chain: a name, then its figure in brackets, maybe
// approximate ("Mistral Large 4 (~1277)").
const rankLink = /^(.{1,60}?)\s*\(\s*([~≈]?)\s*(\d[\d,]*(?:\.\d+)?)\s*\)$/

// ranking reads "A (1277) > B (950) > C (222)" as a ranking, from the most
// to the least, or returns null. It takes three to twelve links, each with
// a figure, in order: anything else is a sentence and stays one.
export function ranking(s: string): Rank[] | null {
  const links = s.trim().replace(/[.;]$/, '').split(/\s+>\s+/)
  if (links.length < 3 || links.length > 12) return null
  const out: Rank[] = []
  for (const link of links) {
    const m = link.trim().match(rankLink)
    if (!m) return null
    const value = Number(m[3].replace(/,/g, ''))
    if (!Number.isFinite(value)) return null
    // A long figure gets its thousands separators, as the rest of the
    // report writes them.
    const figure = /^\d{4,}$/.test(m[3]) ? value.toLocaleString('en-US') : m[3]
    out.push({ name: m[1], value, shown: m[2] + figure })
  }
  for (let i = 1; i < out.length; i++) if (out[i].value > out[i - 1].value) return null
  return out
}

// rankingOf draws a row's value as bars when it's a ranking and nothing
// else: the names, a bar for each as a share of the leader, and the
// figures in tabular numbers. The bars are decoration; the list reads as
// names and figures.
function rankingOf(content: ElementContent[]): Element | null {
  if (!content.length || content.some((c) => c.type !== 'text')) return null
  const ranks = ranking(content.map((c) => (c as Text).value).join(''))
  if (!ranks) return null
  const top = ranks[0].value || 1
  return el(
    'ol',
    ['ranks'],
    ranks.map((r, i) => ({
      type: 'element',
      tagName: 'li',
      properties: i === 0 ? { className: ['lead'] } : {},
      children: [
        el('span', ['rk-name'], [txt(r.name)]),
        { type: 'element', tagName: 'span', properties: { className: ['rk-bar'], ariaHidden: 'true', style: `--v: ${Math.max(2, Math.round((r.value / top) * 100))}%` }, children: [] },
        el('span', ['rk-v'], [txt(r.shown)]),
      ],
    })),
  )
}

// ---------- numbered sections ----------

// numbered moves a leading "1." or "1)" out of an h2's text into its own
// span, for the hanging gutter.
function numbered(h: Element) {
  const first = h.children[0]
  if (first?.type !== 'text') return
  const m = first.value.match(/^\s*(\d{1,2})[.)]\s+/)
  if (!m) return
  h.properties = { ...h.properties, className: ['numbered'] }
  h.children = [el('span', ['h-num'], [txt(m[1])]), txt(first.value.slice(m[0].length)), ...h.children.slice(1)]
}

// ---------- link pills ----------

// bareAddress says a link shows its own address ("https://mistral.ai",
// "mistral.ai/news"), which reads better as the site's name and icon.
export function bareAddress(href: string, label: string) {
  const norm = (s: string) =>
    s
      .trim()
      .replace(/^https?:\/\//i, '')
      .replace(/^www\./i, '')
      .replace(/\/+$/, '')
      .toLowerCase()
  return /^https?:\/\//i.test(href) && !!label.trim() && norm(href) === norm(label)
}

function pill(a: Element) {
  const href = typeof a.properties.href === 'string' ? a.properties.href : ''
  if (bareAddress(href, text(a))) a.properties = { ...a.properties, className: ['pill'] }
}

// ---------- tables ----------

// A cell that holds a number: an amount, a count, a percentage, a size or
// a duration, with its sign, currency and unit.
const numeric = /^[~≈±<>+−-]?\s?[$€£¥₹]?\s?\d[\d,.\s]*\s?(%|[kKmMbB]n?|x|×|h|hrs?|min|s|ms|km|mi|m|cm|mm|g|kg|lb|oz|GB|MB|TB|pts?|°[CF]?)?$/
// What a table says for "nothing here".
const empty = /^(|—|–|-|n\/a|na|none)$/i

export function isNumeric(s: string) {
  return numeric.test(s.trim())
}

// The longest cell, in characters, of a table that's kept on one line a row.
const shortCell = 32

// cellsOf is a table's cells, row by row.
function cellsOf(table: Element) {
  const rows: Element[] = []
  const collect = (n: Element) => {
    for (const c of n.children) if (c.type === 'element') (c.tagName === 'tr' ? rows.push(c) : collect(c))
  }
  collect(table)
  return rows.map((r) => r.children.filter((c): c is Element => isEl(c, 'td') || isEl(c, 'th')))
}

// numericColumns marks the columns whose every filled cell is a number,
// unless the markdown already set the column's alignment.
function numericColumns(cells: Element[][]) {
  const body = cells.filter((r) => r.every((c) => c.tagName === 'td'))
  const cols = Math.max(0, ...cells.map((r) => r.length))
  for (let i = 0; i < cols; i++) {
    const values = body.map((r) => (r[i] ? text(r[i]).trim() : '')).filter((v) => !empty.test(v))
    if (!values.length || !values.every(isNumeric)) continue
    for (const r of cells) {
      const c = r[i]
      if (!c || c.properties.align || (typeof c.properties.style === 'string' && c.properties.style.includes('text-align'))) continue
      const cls = Array.isArray(c.properties.className) ? c.properties.className : []
      c.properties = { ...c.properties, className: [...cls, 'num'] }
    }
  }
}
