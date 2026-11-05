import type { Block } from '../../api/blocks'
import type { CalEvent, Report } from '../../api/types'

// Text helpers for the report: the lede, reading time, excerpts for the
// card, and how dates and times read. Pure, so they're tested without a DOM.

// A line that starts a markdown block other than a paragraph: a heading, a
// list item, a quote, a table row, a fence, raw HTML, an image or an
// indented code line. A paragraph containing one isn't a lede.
const blockLine = /^(#{1,6}\s|[-*+]\s|\d{1,3}[.)]\s|>|\||```|~~~|<|!\[|\s{4}|[=-]{3,}\s*$|\*{3,}\s*$)/

// splitLede takes the first paragraph off a markdown text when it reads as
// a lede: plain prose of a sentence or two, not a heading, a list or a
// table. Anything else leaves the text whole.
export function splitLede(md: string): { lede: string; rest: string } {
  const text = md.replace(/^\s*\n/, '').replace(/^[ \t]+(?=\S)/, '')
  const end = text.search(/\n[ \t]*\n/)
  const para = (end < 0 ? text : text.slice(0, end)).trim()
  const lines = para.split('\n')
  if (para.length < 24 || para.length > 420 || lines.some((l) => blockLine.test(l.trimStart()) || /^[=-]+\s*$/.test(l))) return { lede: '', rest: md }
  return { lede: para, rest: end < 0 ? '' : text.slice(end).replace(/^\s+/, '') }
}

// plain turns markdown into its words: links keep their text, emphasis and
// code lose their marks, headings and list marks go.
export function plain(md: string) {
  return md
    .replace(/```[\s\S]*?```/g, ' ')
    .replace(/!\[([^\]]*)\]\([^)]*\)/g, '$1')
    .replace(/\[([^\]]+)\]\([^)]*\)/g, '$1')
    .replace(/<[^>]+>/g, ' ')
    .replace(/^\s{0,3}(#{1,6}|>|[-*+]|\d+[.)])\s+/gm, '')
    .replace(/(\*\*|__|\*|_|~~|`)/g, '')
    .replace(/\|/g, ' ')
    .replace(/\s+/g, ' ')
    .trim()
}

// clip shortens text to about n characters at a word boundary.
export function clip(s: string, n: number) {
  if (s.length <= n) return s
  const cut = s.slice(0, n)
  const at = cut.lastIndexOf(' ')
  return `${(at > n * 0.6 ? cut.slice(0, at) : cut).replace(/[\s,;:.–—-]+$/, '')}…`
}

// prose is all the markdown a report reads as: its content and its
// markdown blocks.
export function prose(r: Pick<Report, 'content' | 'format' | 'data'>) {
  if (r.format === 'html') return ''
  const blocks = (r.data?.blocks ?? []).flatMap((b) => (b.type === 'markdown' ? [b.text] : []))
  return [r.content ?? '', ...blocks].join('\n\n')
}

export function wordCount(md: string) {
  const p = plain(md)
  return p ? p.split(' ').length : 0
}

// readMinutes is the reading time at a calm 220 words a minute; 0 for a
// report too short to bother saying.
export function readMinutes(words: number) {
  return words < 80 ? 0 : Math.max(1, Math.round(words / 220))
}

export function sectionCount(md: string) {
  return (md.match(/^\s{0,3}##\s+\S/gm) ?? []).length
}

// excerpt is the line under a report card's title: the takeaway Alexa gave
// it, or the start of its lede.
export function excerpt(takeaway: string | undefined, r: Report | null) {
  if (takeaway?.trim()) return takeaway.trim()
  if (!r || r.format === 'html') return r?.data?.takeaway ?? ''
  if (r.data?.takeaway) return r.data.takeaway
  const { lede } = splitLede(prose(r))
  // An address reads as its site on a card: "news.ycombinator.com".
  return lede ? clip(plain(lede).replace(/\bhttps?:\/\/(www\.)?/g, ''), 160) : ''
}

// meta is the small line on a report card and in its document: "5 sections
// · 3 min read", or what its blocks are when it's built from them.
export function meta(r: Report | null, kinds?: string) {
  if (!r) return kinds ?? ''
  if (r.format === 'html') return 'Interactive page'
  const md = prose(r)
  const parts: string[] = []
  const n = sectionCount(md)
  if (n > 1) parts.push(`${n} sections`)
  else if (kinds) parts.push(kinds)
  const min = readMinutes(wordCount(md))
  if (min) parts.push(`${min} min read`)
  if (!parts.length && kinds) parts.push(kinds)
  return parts.join(' · ')
}

// written is a report's date for its eyebrow: "Thursday, October 30".
export function written(ts: number, now: number) {
  const d = new Date(ts)
  const sameYear = d.getFullYear() === new Date(now).getFullYear()
  return d.toLocaleDateString(undefined, { weekday: 'long', month: 'long', day: 'numeric', year: sameYear ? undefined : 'numeric' })
}

// A date-only start ("2026-11-14") is a day, not midnight UTC.
const dateOnly = /^\d{4}-\d{2}-\d{2}$/

function validZone(tz?: string) {
  if (!tz) return undefined
  try {
    new Intl.DateTimeFormat(undefined, { timeZone: tz })
    return tz
  } catch {
    return undefined
  }
}

export type When = { month: string; day: string; line: string }

// eventWhen is how an event's date tile and time line read, in the user's
// zone: {month: "Nov", day: "14", line: "Fri · 7:30 – 9:00 PM"}.
export function eventWhen(ev: CalEvent, tz?: string): When | null {
  const allDay = ev.allDay || dateOnly.test(ev.start)
  const zone = allDay ? 'UTC' : validZone(tz)
  const start = new Date(dateOnly.test(ev.start) ? `${ev.start}T00:00:00Z` : ev.start)
  if (Number.isNaN(start.getTime())) return null
  const f = (o: Intl.DateTimeFormatOptions) => new Intl.DateTimeFormat(undefined, { timeZone: zone, ...o })
  const month = f({ month: 'short' }).format(start)
  const day = f({ day: 'numeric' }).format(start)
  const weekday = f({ weekday: 'short' }).format(start)
  if (allDay) return { month, day, line: `${weekday} · All day` }
  const end = ev.end ? new Date(ev.end) : null
  const time = f({ hour: 'numeric', minute: '2-digit' })
  const range = end && !Number.isNaN(end.getTime()) ? time.formatRange(start, end) : time.format(start)
  return { month, day, line: `${weekday} · ${range}` }
}

// Parts of a report body, in reading order: its markdown content, then its
// blocks, with each run of blocks that aren't markdown kept together for
// the block list.
export type Part = { kind: 'md'; text: string } | { kind: 'blocks'; blocks: Block[] }

export function parts(content: string | undefined, blocks: Block[] | undefined): Part[] {
  const out: Part[] = []
  if (content?.trim()) out.push({ kind: 'md', text: content })
  for (const b of blocks ?? []) {
    if (b.type === 'markdown') {
      if (b.text.trim()) out.push({ kind: 'md', text: b.text })
      continue
    }
    const last = out[out.length - 1]
    if (last?.kind === 'blocks') last.blocks.push(b)
    else out.push({ kind: 'blocks', blocks: [b] })
  }
  return out
}

// setTitle keeps a spaced dash at the end of its line, so a title that
// wraps never opens a line with "– ".
export function setTitle(title: string) {
  return title.replace(/ ([–—-]) /g, '\u00a0$1 ')
}

// copyFailure says why Copy didn't work in plain words, never the
// browser's own ("Failed to execute 'write' on 'Clipboard'…"). A browser
// that won't let the page write to the clipboard (permission, an unfocused
// page, no clipboard at all) points at Download, which always works.
export function copyFailure(e: unknown, clipboard: boolean): string {
  const name = (e as { name?: unknown } | null)?.name
  if (!clipboard || name === 'NotAllowedError' || name === 'SecurityError') return 'Your browser blocked copying. Use Download instead.'
  return 'Something went wrong. Try again, or use Download instead.'
}
