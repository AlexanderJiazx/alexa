import { api } from '../api/client'
import type { AppNotification, Entry, FileInfo, Login, Memory, Report, Schedule, User } from '../api/types'

// Chrome fixtures (WP-12): what the sheets, the menu and Reflect show in the
// gallery. Times are relative to now so they read naturally.

const min = 60e3
const hour = 60 * min
const dayMs = 24 * hour
const now = Date.now()

// when is the next (or, with back, the last) time the clock in London reads
// h:m, on a weekday if asked, so the gallery's schedules run when their cron
// says they do.
function when(h: number, m: number, { weekdays = false, back = false } = {}) {
  const f = new Intl.DateTimeFormat('en-GB', { timeZone: 'Europe/London', hour: '2-digit', minute: '2-digit', weekday: 'short', hourCycle: 'h23' })
  const step = (back ? -15 : 15) * min
  const from = (back ? Math.floor : Math.ceil)(now / (15 * min)) * 15 * min
  for (let t = from, i = 0; i < 8 * 96; t += step, i++) {
    const p = Object.fromEntries(f.formatToParts(t).map((x) => [x.type, x.value]))
    if (+p.hour === h && +p.minute === m && !(weekdays && (p.weekday === 'Sat' || p.weekday === 'Sun'))) return t
  }
  return now
}

export const me: User = { id: 1, username: 'alex', displayName: 'Alexander', email: 'alex@example.com', timezone: 'Europe/London', about: 'I live in London and work in product design. Vegetarian. Prefer trains to planes under five hours.', isAdmin: true, createdAt: now - 90 * dayMs }

export const memories: Memory[] = [
  { id: 1, content: 'Vegetarian, and doesn’t eat fish either.', createdAt: now - 3 * dayMs },
  { id: 2, content: 'Prefers aisle seats on flights, and trains for anything under five hours.', createdAt: now - 9 * dayMs },
  { id: 3, content: 'Works from the Shoreditch office on Tuesdays and Thursdays.', createdAt: now - 20 * dayMs },
  { id: 4, content: 'Partner is Sam; anniversary is 14 June.', createdAt: now - 41 * dayMs },
]

export const schedules: Schedule[] = [
  { id: 's1', name: 'Morning briefing', cron: '30 7 * * 1-5', prompt: 'Check the BBC, the FT and Hacker News and send me a five-bullet briefing of what matters today.', enabled: true, lastRun: when(7, 30, { weekdays: true, back: true }), nextRun: when(7, 30, { weekdays: true }), createdAt: now - 30 * dayMs },
  { id: 's2', name: 'Eurostar price watch', cron: '0 9 * * *', prompt: 'Look up Eurostar fares London to Paris for 12–14 December and tell me if any return is under £120.', enabled: true, lastRun: when(9, 0, { back: true }), nextRun: when(9, 0), createdAt: now - 6 * dayMs },
  { id: 's3', name: 'Renew the parking permit', runAt: now + 4 * dayMs, prompt: 'Remind me to renew the residents’ parking permit and open the council page.', enabled: false, createdAt: now - 2 * dayMs },
]

export const logins: Login[] = [
  { id: 1, site: 'bestbuy.com', username: 'alex@example.com', createdAt: now - 40 * dayMs, updatedAt: now - 40 * dayMs, usedAt: now - 2 * hour },
  { id: 2, site: 'eurostar.com', username: 'alexander.k', createdAt: now - 12 * dayMs, updatedAt: now - 12 * dayMs },
  { id: 3, site: 'tfl.gov.uk', username: 'alex@example.com', createdAt: now - 70 * dayMs, updatedAt: now - 5 * dayMs, usedAt: now - 5 * dayMs },
]

export const notes: AppNotification[] = [
  { id: 3, ref: 'report:r1', title: 'Your morning briefing is ready', body: 'Five things today: rail strikes on Thursday, the Fed’s decision, and three more.', read: false, createdAt: now - 2 * hour },
  { id: 2, title: 'Alexa needs you', body: 'Eurostar wants a verification code before it shows fares.', read: false, createdAt: now - 5 * hour },
  { id: 1, ref: 'report:r2', title: 'Price watch: no change', body: 'Cheapest return is still £138.', read: true, createdAt: now - 26 * hour },
]

export const people: User[] = [me, { id: 2, username: 'sam', displayName: 'Sam', email: '', timezone: 'Europe/London', about: '', isAdmin: false, createdAt: now - 60 * dayMs }]

export const reports: Report[] = [
  { id: 'r1', title: 'Morning briefing, Thursday', format: 'markdown', createdAt: now - 2 * hour, updatedAt: now - 2 * hour, data: { takeaway: 'Rail strikes on Thursday; the Fed holds; two of your stocks moved more than 3%.' } },
  { id: 'r2', title: 'Noise-cancelling headphones under $300', format: 'markdown', createdAt: now - 2 * dayMs, updatedAt: now - 1 * dayMs },
  { id: 'r3', title: 'Lisbon in four days: an itinerary for a slow, food-first trip', format: 'html', createdAt: now - 3 * dayMs, updatedAt: now - 3 * dayMs },
  { id: 'r4', title: 'Energy tariffs compared', format: 'markdown', createdAt: now - 20 * dayMs, updatedAt: now - 20 * dayMs },
  { id: 'r5', title: 'Which standing desk', format: 'markdown', createdAt: now - 45 * dayMs, updatedAt: now - 45 * dayMs },
]

export const files: FileInfo[] = [
  { path: 'uploads/receipt-2026-09.pdf', size: 48213, modTime: now - 30 * min, isDir: false },
  { path: 'downloads/eurostar-fares.csv', size: 3120, modTime: now - 26 * hour, isDir: false },
  { path: 'images/lisbon-tram.jpg', size: 1840211, modTime: now - 3 * dayMs, isDir: false },
  { path: 'notes.md', size: 912, modTime: now - 9 * dayMs, isDir: false },
]

// The conversation as Reflect shows it: a scheduled job, a request with a
// question and its answer, one that was stopped and one that didn't finish.
let id = 5000
const e = (kind: string, text: string, at: number, rest: Partial<Entry> = {}): Entry => ({ id: id++, kind, text, createdAt: at, updatedAt: at, ...rest })
const y = now - dayMs - 3 * hour
const t = now - 3 * hour

export const feed: Entry[] = [
  e('user', 'Find me the best noise-cancelling headphones under $300 and check which store can deliver by Friday.', y, { data: { start: true } }),
  e('step', 'Searching the web', y + min, { status: 'done', doneText: 'Searched the web for “best noise cancelling headphones 2026”' }),
  e('step', 'Reading rtings.com', y + 2 * min, { status: 'done', doneText: 'Read rtings.com' }),
  e('ask', 'Two are close. Do you care more about comfort or battery life?', y + 3 * min, { status: 'answered', data: { options: ['Comfort', 'Battery life', 'Either'] } }),
  e('user', 'Comfort', y + 4 * min),
  e('say', 'Then the Bose QC Ultra: the most comfortable over long flights, $279 at Best Buy with delivery Thursday.', y + 6 * min),
  e('report', 'Noise-cancelling headphones under $300', y + 7 * min, { data: { id: 'r2' } }),
  e('user', 'Book a table for two at Dishoom Shoreditch tonight at 8', y + 5 * hour, { data: { start: true } }),
  e('step', 'Opening dishoom.com', y + 5 * hour + min, { status: 'done', doneText: 'Opened dishoom.com' }),
  e('note', 'Stopped', y + 5 * hour + 2 * min),
  e('note', 'Scheduled: Morning briefing', t, { data: { scheduled: true, start: true } }),
  e('step', 'Reading bbc.co.uk', t + min, { status: 'done', doneText: 'Read bbc.co.uk' }),
  e('report', 'Morning briefing, Thursday', t + 3 * min, { data: { id: 'r1' } }),
  e('say', 'Your briefing is ready: rail strikes on Thursday are the big one.', t + 3 * min),
  e('user', 'What’s the cheapest Eurostar return for 12 to 14 December?', t + 40 * min, { data: { start: true } }),
  e('ask', 'Standard or Standard Premier?', t + 42 * min, { status: 'answered', data: { options: ['Standard', 'Standard Premier'] } }),
  e('user', 'Whichever is cheaper, but only if it’s under £150', t + 43 * min),
  e('error', 'The model service didn’t answer in time.', t + 45 * min),
]

// fakeApi answers the chrome's requests from the fixtures, so the gallery
// needs no backend. empty answers every list with nothing.
export function fakeApi(empty = false) {
  const list = <T>(v: T[]) => async () => (empty ? [] : v)
  const ok = async () => ({})
  Object.assign(api, {
    memories: list(memories),
    schedules: list(schedules),
    logins: list(logins),
    users: list(people),
    reports: list(reports),
    files: list(files),
    notifications: list(notes),
    markRead: ok,
    deleteMemory: ok,
    deleteSchedule: ok,
    deleteFile: ok,
    deleteReport: ok,
    deleteLogin: ok,
    addMemory: ok,
    runSchedule: ok,
    setScheduleEnabled: ok,
  })
}
