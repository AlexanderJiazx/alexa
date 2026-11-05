import { firstLine } from '../features/palette/context'
import { zoneCity } from './zones'

// Schedules in words, and a small builder so nobody has to type cron for
// the common cases (IX-F17): every day, weekdays, or one day a week, at a
// time. Anything else stays as cron, under Custom.

export type Freq = 'daily' | 'weekdays' | 'weekly' | 'custom'
export type When = { freq: Freq; time: string; day: number; cron: string }

export const freqs: readonly (readonly [Freq, string])[] = [
  ['daily', 'Every day'],
  ['weekdays', 'Weekdays'],
  ['weekly', 'Weekly'],
  ['custom', 'Custom'],
]

export const dayNames = ['Sunday', 'Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday']

const num = (s: string) => /^\d+$/.test(s)

// fromCron reads a cron line into the builder's terms; shapes it doesn't
// know become Custom, kept as they were.
export function fromCron(c: string): When {
  const p = c.trim().split(/\s+/)
  const fallback: When = { freq: 'custom', time: '08:00', day: 1, cron: c.trim() }
  if (p.length !== 5) return fallback
  const [m, h, dom, mon, dow] = p
  if (!num(m) || !num(h) || dom !== '*' || mon !== '*' || +h > 23 || +m > 59) return fallback
  const time = `${h.padStart(2, '0')}:${m.padStart(2, '0')}`
  if (dow === '*') return { freq: 'daily', time, day: 1, cron: c.trim() }
  if (dow === '1-5') return { freq: 'weekdays', time, day: 1, cron: c.trim() }
  if (num(dow) && +dow <= 7) return { freq: 'weekly', time, day: +dow % 7, cron: c.trim() }
  return fallback
}

// toCron writes the builder's choice as cron.
export function toCron(w: When): string {
  if (w.freq === 'custom') return w.cron.trim()
  const [h, m] = (w.time || '08:00').split(':').map((x) => String(Number(x) || 0))
  const dow = w.freq === 'daily' ? '*' : w.freq === 'weekdays' ? '1-5' : String(w.day)
  return `${m} ${h} * * ${dow}`
}

// clockOf says a 24-hour HH:MM the way this device says times.
export function clockOf(hhmm: string) {
  const [h, m] = hhmm.split(':').map(Number)
  const d = new Date(2000, 0, 1, h, m)
  return d.toLocaleTimeString(undefined, { hour: 'numeric', minute: '2-digit' })
}

// describeCron says a cron schedule in words, for the common shapes.
export function describeCron(c: string) {
  const p = c.trim().split(/\s+/)
  if (p.length !== 5) return c
  const [m, h, dom, mon, dow] = p
  const time = num(m) && num(h) ? clockOf(`${h}:${m}`) : null
  const days: Record<string, string> = { '1-5': 'weekdays', '0,6': 'weekends', '6,0': 'weekends', '*': 'every day', '0': 'Sundays', '1': 'Mondays', '2': 'Tuesdays', '3': 'Wednesdays', '4': 'Thursdays', '5': 'Fridays', '6': 'Saturdays', '7': 'Sundays' }
  if (time && dom === '*' && mon === '*' && days[dow]) return `${days[dow][0].toUpperCase()}${days[dow].slice(1)} at ${time}`
  if (time && num(dom) && mon === '*' && dow === '*') return `Monthly on day ${dom} at ${time}`
  if (m.startsWith('*/') && h === '*') return `Every ${m.slice(2)} minutes`
  if (num(m) && h.startsWith('*/')) return `Every ${h.slice(2)} hours`
  return c
}

// zoneOr is the timezone to show times in: the user's, when this device
// knows it, so "Next run" agrees with "Weekdays at 8:00 AM" (the server
// runs cron in the user's timezone, not the device's).
function zoneOr(tz?: string) {
  if (!tz) return undefined
  try {
    new Intl.DateTimeFormat(undefined, { timeZone: tz })
    return tz
  } catch {
    return undefined
  }
}

const dayOf = (ts: number, tz?: string) => new Intl.DateTimeFormat('en-CA', { timeZone: tz, year: 'numeric', month: '2-digit', day: '2-digit' }).format(ts)

// fmtRun says when a schedule runs, in the user's timezone: "Today at 8:00
// AM", "Tomorrow at 8:00 AM", "Monday at 8:00 AM" within the week, and
// "Fri, Nov 7 at 8:00 AM" after that.
export function fmtRun(ts?: number, tz?: string, now = Date.now()) {
  if (!ts) return ''
  const zone = zoneOr(tz)
  const time = new Date(ts).toLocaleTimeString(undefined, { timeZone: zone, hour: 'numeric', minute: '2-digit' })
  const day = dayOf(ts, zone)
  if (day === dayOf(now, zone)) return `Today at ${time}`
  if (day === dayOf(now + 864e5, zone)) return `Tomorrow at ${time}`
  const soon = ts > now && ts - now < 6 * 864e5
  const date = new Date(ts).toLocaleDateString(undefined, soon ? { timeZone: zone, weekday: 'long' } : { timeZone: zone, weekday: 'short', month: 'short', day: 'numeric' })
  return `${date} at ${time}`
}

// zoneNote names the user's timezone when this device is in another one
// ("Los Angeles time"), since that's the clock the schedule keeps.
export function zoneNote(tz?: string, here = Intl.DateTimeFormat().resolvedOptions().timeZone) {
  const zone = zoneOr(tz)
  if (!zone || zone === here) return ''
  const city = zoneCity(zone)
  return city ? `${city} time` : ''
}

// inSentence lowers "Today" and "Tomorrow" for the middle of a sentence.
export const inSentence = (s: string) => s.replace(/^(Today|Tomorrow)\b/, (w) => w.toLowerCase())

// nameFrom is a new schedule's name when none was given: the start of what
// it does, cut at a word.
export const nameFrom = (prompt: string) => firstLine(prompt, 40)

// namedByPrompt says whether a schedule's name is only the start of its
// prompt (a name nobody gave it), so its row needn't say it twice.
export function namedByPrompt(name: string, prompt: string) {
  const squash = (s: string) => s.trim().replace(/\s+/g, ' ').toLowerCase()
  const n = squash(name.replace(/…$/, ''))
  return !n || squash(prompt).startsWith(n)
}
