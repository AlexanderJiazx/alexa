import type { BlockOf } from '../../../api/blocks'
import { fmtWhen, money, number, parseWhen } from '../../../lib/format'

// The chart's arithmetic, kept pure so it tests without a DOM: nice ticks,
// the x domain, value formats and spreading direct labels apart.

type B = BlockOf<'chart'>

// niceTicks are 3 to count+1 round values covering lo..hi (1, 2, 2.5 or
// 5 times a power of ten apart): the smallest step that needs no more.
export function niceTicks(lo: number, hi: number, count = 4): number[] {
  if (!Number.isFinite(lo) || !Number.isFinite(hi)) return [0, 1]
  if (lo === hi) {
    const pad = Math.abs(lo) * 0.1 || 1
    lo -= pad
    hi += pad
  }
  const mag = 10 ** Math.floor(Math.log10((hi - lo) / count))
  let step = mag
  for (const f of [1, 2, 2.5, 5, 10, 20]) {
    step = f * mag
    if (Math.ceil(hi / step - 1e-9) - Math.floor(lo / step + 1e-9) <= count) break
  }
  const start = Math.floor(lo / step + 1e-9) * step
  const end = Math.ceil(hi / step - 1e-9) * step
  const out: number[] = []
  for (let v = start; v <= end + step / 2; v += step) out.push(Math.round((v / step) * 1e6) / 1e6 * step)
  return out
}

// A point placed on the chart's own x axis: time in ms, or a category's
// index.
export type Pt = { key: string | number; x: number; y: number }
export type Model = {
  time: boolean
  ordinal: boolean // x is an index into keys (categories, and bars)
  keys: (string | number)[] // every x, in order
  xs: number[] // their positions on the axis
  series: { name: string; pts: Pt[] }[]
  lo: number
  hi: number
  ticks: number[]
}

export function model(b: B, zone?: string): Model {
  const time = b.x === 'time'
  const xOf = (k: string | number, i: number) => {
    if (!time) return i
    if (typeof k === 'number') return k
    return parseWhen(k, zone)?.ms ?? i
  }
  // Categories keep the order they first appear in.
  const keys: (string | number)[] = []
  const seen = new Map<string, number>()
  for (const s of b.series)
    for (const p of s.points) {
      const id = String(p.x)
      if (!seen.has(id)) {
        seen.set(id, keys.length)
        keys.push(p.x)
      }
    }
  let xs = keys.map((k, i) => xOf(k, i))
  if (time) {
    const order = keys.map((_, i) => i).sort((a, c) => xs[a] - xs[c])
    const k2 = order.map((i) => keys[i])
    xs = order.map((i) => xs[i])
    keys.splice(0, keys.length, ...k2)
    k2.forEach((k, i) => seen.set(String(k), i))
  }
  // Bars stand in bands, one per x, whatever the axis.
  const ordinal = !time || b.style === 'bar'
  if (ordinal) xs = keys.map((_, i) => i)
  const series = b.series.slice(0, 3).map((s) => ({
    name: s.name,
    pts: s.points
      .map((p) => {
        const i = seen.get(String(p.x)) ?? 0
        return { key: p.x, x: xs[i], y: p.y }
      })
      .sort((a, c) => a.x - c.x),
  }))
  const ys = series.flatMap((s) => s.pts.map((p) => p.y))
  let lo = Math.min(...ys)
  let hi = Math.max(...ys)
  if (b.style === 'bar') {
    lo = Math.min(0, lo)
    hi = Math.max(0, hi)
  }
  const ticks = niceTicks(lo, hi, 4)
  return { time, ordinal, keys, xs, series, lo: ticks[0], hi: ticks[ticks.length - 1], ticks }
}

// formats are how the chart writes a value in full, on an axis tick and
// for an x.
export function formats(b: B, zone?: string) {
  const cents = b.series.some((s) => s.points.some((p) => !Number.isInteger(p.y)))
  const full = (v: number) => {
    if (b.currency) return money(v, b.currency, undefined, cents)
    if (b.unit === '%') return `${number(v)}%`
    return b.unit ? `${number(v)} ${b.unit}` : number(v)
  }
  const tick = (v: number) => {
    if (b.currency) return money(v, b.currency)
    if (b.unit === '%') return `${number(v)}%`
    return number(v)
  }
  const x = (k: string | number) => {
    if (b.x !== 'time') return String(k)
    const w = typeof k === 'number' ? { ms: k, zone, dateOnly: false } : parseWhen(k, zone)
    return w ? fmtWhen(w, 'short') : String(k)
  }
  return { full, tick, x }
}

// spread moves labels apart so none is closer than gap to the next,
// keeping each as near its own value as it can and all within lo..hi.
export function spread(ys: number[], gap: number, lo: number, hi: number): number[] {
  const order = ys.map((y, i) => ({ y, i })).sort((a, b) => a.y - b.y)
  const out = order.map((o) => Math.min(hi, Math.max(lo, o.y)))
  for (let k = 1; k < out.length; k++) out[k] = Math.max(out[k], out[k - 1] + gap)
  // If the stack ran past hi, push it back up from the end.
  if (out.length && out[out.length - 1] > hi) {
    out[out.length - 1] = hi
    for (let k = out.length - 2; k >= 0; k--) out[k] = Math.min(out[k], out[k + 1] - gap)
  }
  const res = new Array<number>(ys.length)
  order.forEach((o, k) => (res[o.i] = out[k]))
  return res
}

// pickTicks chooses up to max evenly spaced indexes of n, always the first
// and the last.
export function pickTicks(n: number, max: number): number[] {
  if (n <= 0) return []
  if (n <= max) return Array.from({ length: n }, (_, i) => i)
  const m = Math.max(2, max)
  return Array.from({ length: m }, (_, k) => Math.round((k * (n - 1)) / (m - 1)))
}

// textWidth estimates how wide a label is at the chart's 12px type.
export const textWidth = (s: string) => Math.ceil(s.length * 6.6)

// barPath draws a bar from the baseline at base to the value at top,
// rounded only at its free end: the top of a bar going up, the bottom of
// one going down, so every bar stands squarely on the axis.
export function barPath(x: number, w: number, top: number, base: number, r = 4): string {
  const h = Math.abs(base - top)
  const k = Math.max(0, Math.min(r, w / 2, h))
  const f = (v: number) => Number(v.toFixed(2))
  if (!k) return `M${f(x)} ${f(base)}V${f(top)}H${f(x + w)}V${f(base)}Z`
  const d = top < base ? 1 : -1 // 1 when the bar goes up from the baseline
  return [
    `M${f(x)} ${f(base)}`,
    `V${f(top + d * k)}`,
    `Q${f(x)} ${f(top)} ${f(x + k)} ${f(top)}`,
    `H${f(x + w - k)}`,
    `Q${f(x + w)} ${f(top)} ${f(x + w)} ${f(top + d * k)}`,
    `V${f(base)}Z`,
  ].join('')
}
