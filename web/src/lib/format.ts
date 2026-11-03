// Formatting helpers. The first group (elapsed, ago, fmtSize, clock, day)
// is what the conversation, Reflect and the sheets have always used and
// keeps its behaviour. The rest is the client-side Intl formatting the
// blocks need (WP-04): money, dates in the user's zone, relative times and
// compact numbers. Every helper takes an optional locale so tests can pin
// one; the app passes nothing and gets the browser's.

export function elapsed(from: number, to: number) {
  const s = Math.max(0, Math.round((to - from) / 1000))
  if (s >= 3600) return `${Math.floor(s / 3600)}:${String(Math.floor((s % 3600) / 60)).padStart(2, '0')}:${String(s % 60).padStart(2, '0')}`
  return `${Math.floor(s / 60)}:${String(s % 60).padStart(2, '0')}`
}

export function ago(ts: number) {
  const s = Math.round((Date.now() - ts) / 1000)
  if (s < 60) return 'just now'
  if (s < 3600) return `${Math.floor(s / 60)}m ago`
  if (s < 86400) return `${Math.floor(s / 3600)}h ago`
  const d = new Date(ts)
  return d.toLocaleDateString(undefined, { month: 'short', day: 'numeric' })
}

export function fmtSize(n: number) {
  if (n < 1024) return `${n} B`
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(0)} KB`
  return `${(n / 1024 / 1024).toFixed(1)} MB`
}

export function clock(ts: number) {
  return new Date(ts).toLocaleTimeString(undefined, { hour: 'numeric', minute: '2-digit' })
}

// day labels a timestamp for the conversation's time dividers.
export function day(ts: number) {
  const d = new Date(ts)
  const today = new Date()
  const yesterday = new Date(Date.now() - 864e5)
  if (d.toDateString() === today.toDateString()) return 'Today'
  if (d.toDateString() === yesterday.toDateString()) return 'Yesterday'
  return d.toLocaleDateString(undefined, { weekday: 'long', month: 'short', day: 'numeric', year: d.getFullYear() === today.getFullYear() ? undefined : 'numeric' })
}

// ---------- numbers and money ----------

// Intl formatters are slow to build and cheap to reuse.
const cache = new Map<string, Intl.NumberFormat | Intl.DateTimeFormat>()
function nf(locale: string | undefined, o: Intl.NumberFormatOptions) {
  const k = `n|${locale}|${JSON.stringify(o)}`
  let f = cache.get(k) as Intl.NumberFormat | undefined
  if (!f) cache.set(k, (f = new Intl.NumberFormat(locale, o)))
  return f
}

// moneyFormat formats an amount in a currency: whole amounts without
// cents ("$279"), others with them ("$303.76"). force2 keeps the cents on
// whole amounts too, for totals and receipt lines that should line up.
function moneyFormat(amount: number, currency: string, locale?: string, force2 = false) {
  const whole = Number.isInteger(amount) && !force2
  try {
    return nf(locale, { style: 'currency', currency, minimumFractionDigits: whole ? 0 : 2, maximumFractionDigits: whole ? 0 : 2 })
  } catch {
    return null // not an ISO 4217 code
  }
}

export function money(amount: number, currency: string, locale?: string, force2 = false) {
  const f = moneyFormat(amount, currency, locale, force2)
  return f ? f.format(amount) : `${number(amount, locale)} ${currency}`
}

// moneyParts splits a formatted amount into the currency symbol and the
// rest, so the symbol can sit a little smaller than the figure.
export function moneyParts(amount: number, currency: string, locale?: string, force2 = false): { symbol: string; value: string; symbolFirst: boolean } {
  const runs = moneyRuns(amount, currency, locale, force2)
  const i = runs.findIndex((r) => r.cur)
  return {
    symbol: i >= 0 ? runs[i].text : '',
    value: runs
      .filter((r) => !r.cur)
      .map((r) => r.text)
      .join('')
      .trim(),
    symbolFirst: i === 0 || (i > 0 && !/\d/.test(runs.slice(0, i).map((r) => r.text).join(''))),
  }
}

// moneyRuns is a formatted amount as runs of text in the locale's order,
// the currency symbol marked, so it can be set apart wherever it falls
// ("$279", "279 €", "-$38").
export function moneyRuns(amount: number, currency: string, locale?: string, force2 = false): { cur: boolean; text: string }[] {
  const f = moneyFormat(amount, currency, locale, force2)
  if (!f) return [{ cur: false, text: `${number(amount, locale)} ` }, { cur: true, text: currency }]
  const out: { cur: boolean; text: string }[] = []
  for (const p of f.formatToParts(amount)) {
    const cur = p.type === 'currency'
    const last = out[out.length - 1]
    if (last && last.cur === cur && !cur) last.text += p.value
    else out.push({ cur, text: p.value })
  }
  return out
}

export function number(n: number, locale?: string, digits = 2) {
  return nf(locale, { maximumFractionDigits: digits }).format(n)
}

// compact is a short count: 3,120 → "3.1K", 1,800,000 → "1.8M".
export function compact(n: number, locale?: string) {
  if (Math.abs(n) < 1000) return number(n, locale, 1)
  return nf(locale, { notation: 'compact', maximumFractionDigits: 1 }).format(n)
}

// minus sets a change the model wrote with a hyphen ("-$38", "-2") with a
// true minus sign, as wide as the plus and the digits beside it.
export const minus = (s: string) => s.replace(/^(\s*)-(?=\s?[\d$€£¥₹₩₽.])/, '$1−')

// percentOff is the saving from was to now, in whole percent, or 0.
export function percentOff(now: number, was?: number) {
  if (!was || was <= now || was <= 0) return 0
  return Math.round((1 - now / was) * 100)
}

// numericCell says whether a table cell reads as a number: "$279.00",
// "-$38", "1,234", "12%", "30 h", "4.5/5". Such columns align right.
export function numericCell(s: string) {
  return /^[\s(+\-−~≈]*[$€£¥₹₩₽]?\s?\d[\d.,'\s]*(?:\s?(?:%|[a-zA-Zµ°/.]{1,4}\d*))?\)?$/.test(s.trim())
}

// numericColumn decides a column's alignment from its cells: numeric when
// most of the filled cells are.
export function numericColumn(cells: string[]) {
  const filled = cells.map((c) => (c ?? '').trim()).filter((c) => c && c !== '—' && c !== '-')
  if (!filled.length) return false
  return filled.filter(numericCell).length / filled.length >= 0.6
}

// ---------- dates ----------

// A block date is "2026-11-14", "2026-11-14T19:30" (a wall-clock time in
// the user's zone) or RFC 3339 with an offset (an instant). A wall-clock
// time is kept as it was written, whatever zone the browser is in; an
// instant is shown in the user's zone.
export type When = { ms: number; zone: string | undefined; dateOnly: boolean }

const naive = /^(\d{4})-(\d{2})-(\d{2})(?:[T ](\d{2}):(\d{2})(?::(\d{2}))?(?:\.\d+)?)?$/

export function parseWhen(s: string | undefined, zone?: string): When | null {
  if (!s) return null
  const t = s.trim()
  const m = naive.exec(t)
  if (m) {
    const [, y, mo, d, h, mi, se] = m
    const ms = Date.UTC(+y, +mo - 1, +d, h ? +h : 0, mi ? +mi : 0, se ? +se : 0)
    return Number.isNaN(ms) ? null : { ms, zone: 'UTC', dateOnly: !h }
  }
  const ms = Date.parse(t)
  return Number.isNaN(ms) ? null : { ms, zone, dateOnly: false }
}

// wallClock reads a date as the clock on the wall where it was written: an
// RFC 3339 time keeps its own offset rather than moving to the user's
// zone. A flight leaves at 18:45 in Newark and lands at 06:50 in Lisbon.
export function wallClock(s: string | undefined): When | null {
  if (!s) return null
  const m = /([+-])(\d{2}):?(\d{2})$|Z$/i.exec(s.trim())
  const w = parseWhen(s)
  if (!w || !m || w.zone === 'UTC') return w
  const off = m[1] ? (m[1] === '-' ? -1 : 1) * (+m[2] * 60 + +m[3]) : 0
  return { ms: w.ms + off * 60e3, zone: 'UTC', dateOnly: false }
}

function df(locale: string | undefined, o: Intl.DateTimeFormatOptions) {
  const k = `d|${locale}|${JSON.stringify(o)}`
  let f = cache.get(k) as Intl.DateTimeFormat | undefined
  if (!f) {
    try {
      f = new Intl.DateTimeFormat(locale, o)
    } catch {
      f = new Intl.DateTimeFormat(locale, { ...o, timeZone: undefined }) // an unknown zone name
    }
    cache.set(k, f)
  }
  return f
}

export type DateStyle = 'day' | 'long' | 'time' | 'dayTime' | 'month' | 'dom' | 'weekday' | 'short'
const styles: Record<DateStyle, Intl.DateTimeFormatOptions> = {
  day: { weekday: 'short', month: 'short', day: 'numeric' }, // Sat, Nov 14
  long: { weekday: 'long', month: 'long', day: 'numeric' }, // Saturday, November 14
  short: { month: 'short', day: 'numeric' }, // Nov 14
  time: { hour: 'numeric', minute: '2-digit' }, // 8:10 AM
  dayTime: { weekday: 'short', month: 'short', day: 'numeric', hour: 'numeric', minute: '2-digit' },
  month: { month: 'short' }, // Nov
  dom: { day: 'numeric' }, // 14
  weekday: { weekday: 'short' }, // Sat
}

// fmtWhen formats a parsed date in one of the styles; a date-only value
// asked for a time shows its day.
export function fmtWhen(w: When, style: DateStyle, locale?: string) {
  const st = w.dateOnly && (style === 'time' || style === 'dayTime') ? 'day' : style
  return df(locale, { ...styles[st], timeZone: w.zone }).format(w.ms)
}

// fmtDate formats a block date string, or gives it back as written when it
// can't be read.
export function fmtDate(s: string | undefined, style: DateStyle, zone?: string, locale?: string) {
  const w = parseWhen(s, zone)
  return w ? fmtWhen(w, style, locale) : (s ?? '')
}

// fmtClock formats a timeline's "08:10" in the locale's way ("8:10 AM").
export function fmtClock(hhmm: string | undefined, locale?: string) {
  const m = /^(\d{1,2}):(\d{2})$/.exec((hhmm ?? '').trim())
  if (!m) return hhmm ?? ''
  return df(locale, { hour: 'numeric', minute: '2-digit', timeZone: 'UTC' }).format(Date.UTC(2000, 0, 1, +m[1], +m[2]))
}

// calendarDay is a date's day number in its zone, for counting nights and
// a flight's "+1".
function calendarDay(w: When) {
  const p = df('en-US', { year: 'numeric', month: 'numeric', day: 'numeric', timeZone: w.zone }).formatToParts(w.ms)
  const get = (t: string) => Number(p.find((x) => x.type === t)?.value)
  return Math.round(Date.UTC(get('year'), get('month') - 1, get('day')) / 864e5)
}

// daysBetween is how many calendar days y is after x, each in its own zone.
export const daysBetween = (x: When, y: When) => calendarDay(y) - calendarDay(x)

// dayDiff is daysBetween for two block dates.
export function dayDiff(a: string | undefined, b: string | undefined, zone?: string) {
  const x = parseWhen(a, zone)
  const y = parseWhen(b, zone)
  return x && y ? daysBetween(x, y) : 0
}

// nights is a stay's length.
export const nights = (checkIn?: string, checkOut?: string, zone?: string) => Math.max(0, dayDiff(checkIn, checkOut, zone))

// dateRange is "Nov 14 – 17", or "Nov 30 – Dec 2" across months.
export function dateRange(a: string | undefined, b: string | undefined, zone?: string, locale?: string) {
  const x = parseWhen(a, zone)
  const y = parseWhen(b, zone)
  if (!x) return b ?? ''
  if (!y) return fmtWhen(x, 'short', locale)
  const f = df(locale, { month: 'short', day: 'numeric', timeZone: x.zone }) as Intl.DateTimeFormat & { formatRange?: (a: number, b: number) => string }
  if (x.zone === y.zone && typeof f.formatRange === 'function') return f.formatRange(x.ms, y.ms)
  return `${fmtWhen(x, 'short', locale)} – ${fmtWhen(y, 'short', locale)}`
}

// relative is "in 3 days", "2 hours ago", "now".
export function relative(ts: number, now = Date.now(), locale?: string) {
  const s = Math.round((ts - now) / 1000)
  const a = Math.abs(s)
  const rtf = new Intl.RelativeTimeFormat(locale, { numeric: 'auto' })
  if (a < 45) return rtf.format(0, 'second')
  if (a < 2700) return rtf.format(Math.round(s / 60), 'minute')
  if (a < 64800) return rtf.format(Math.round(s / 3600), 'hour')
  if (a < 2592000) return rtf.format(Math.round(s / 86400), 'day')
  if (a < 31536000) return rtf.format(Math.round(s / 2592000), 'month')
  return rtf.format(Math.round(s / 31536000), 'year')
}
