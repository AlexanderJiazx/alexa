import type { Block } from '../../api/blocks'

// valid checks that a block has what its renderer needs before it is
// drawn. The server validates every block (internal/agent/blocks_check.go),
// so this is a seatbelt for old entries, a server that changed, or a
// fixture with a typo: a block that fails shows its summary instead of
// breaking the message (the ErrorBoundary catches whatever this misses).

const list = (v: unknown): v is unknown[] => Array.isArray(v) && v.length > 0
const str = (v: unknown): v is string => typeof v === 'string' && v.trim() !== ''
const num = (v: unknown): v is number => typeof v === 'number' && Number.isFinite(v)
const obj = (v: unknown): v is Record<string, unknown> => !!v && typeof v === 'object' && !Array.isArray(v)
const every = (v: unknown, f: (x: Record<string, unknown>) => boolean) => list(v) && v.every((x) => obj(x) && f(x))
const moneyOk = (m: unknown) => m === undefined || (obj(m) && num(m.amount) && str(m.currency))

const checks: Record<Block['type'], (b: Record<string, unknown>) => boolean> = {
  cards: (b) => every(b.items, (c) => str(c.title) && moneyOk(c.price) && (c.segments === undefined || every(c.segments, (s) => str(s.from) && str(s.to)))),
  compare: (b) => every(b.items, (i) => str(i.name)) && every(b.rows, (r) => str(r.label) && Array.isArray(r.values)),
  table: (b) => list(b.columns) && Array.isArray(b.rows) && (b.rows as unknown[]).every(Array.isArray),
  stats: (b) => every(b.items, (s) => str(s.label) && (str(s.value) || num(s.value))),
  chart: (b) => every(b.series, (s) => Array.isArray(s.points) && (s.points as unknown[]).every((p) => obj(p) && num(p.y) && (str(p.x) || num(p.x)))),
  timeline: (b) => every(b.days, (d) => str(d.date) && every(d.items, (i) => str(i.title))),
  checklist: (b) => every(b.items, (i) => str(i.text)),
  receipt: (b) => Array.isArray(b.lines) && obj(b.total) && num(b.total.amount) && str(b.total.currency),
  links: (b) => every(b.items, (i) => str(i.url) && str(i.title)),
  images: (b) => every(b.items, (i) => str(i.src)),
  files: (b) => list(b.items) && (b.items as unknown[]).every(str),
  callout: (b) => str(b.text),
  markdown: (b) => str(b.text),
}

// known says whether this client has a renderer for the block's type. An
// unknown type is skipped silently (a newer server's block).
export const known = (b: unknown): b is Block => obj(b) && typeof b.type === 'string' && b.type in checks

export function valid(b: Block) {
  try {
    return checks[b.type](b as unknown as Record<string, unknown>)
  } catch {
    return false
  }
}
