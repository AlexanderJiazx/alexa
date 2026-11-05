import type { Entry, Login, Report, Schedule, User } from '../../api/types'

// Fixtures for the stage and the home board (?gallery=stage): users, a
// conversation, schedules and reports. Times are relative to now, so they
// read naturally.

const min = 60e3
const now = Date.now()
let next = 5000

export function entry(kind: string, text: string, rest: Partial<Entry> = {}, minsAgo = 0): Entry {
  const id = next++
  const createdAt = now - minsAgo * min
  return { id, kind, text, createdAt, updatedAt: createdAt, ...rest }
}

export const newcomer: User = { id: 9001, username: 'alex', displayName: 'Alexander', email: '', timezone: 'Europe/London', about: '', isAdmin: false, createdAt: now - 2 * 864e5 }
export const regular: User = { ...newcomer, about: 'I live in London and work in product design.', createdAt: now - 120 * 864e5 }

export const schedules: Schedule[] = [
  { id: 's1', name: 'Morning briefing', cron: '30 7 * * *', prompt: 'Brief me', enabled: true, nextRun: now + 9 * 3600e3, createdAt: now - 30 * 864e5 },
  { id: 's2', name: 'Price watch: Sony XM6', cron: '0 */4 * * *', prompt: 'Check the price', enabled: true, nextRun: now + 38 * min, createdAt: now - 3 * 864e5 },
  { id: 's3', name: 'Weekly reading list', cron: '0 9 * * 1', prompt: 'Reading list', enabled: true, nextRun: now + 3 * 864e5, createdAt: now - 60 * 864e5 },
  { id: 's4', name: 'Paused job', cron: '0 9 * * *', prompt: 'x', enabled: false, nextRun: now + 3600e3, createdAt: now - 60 * 864e5 },
]

export const reports: Report[] = [
  {
    id: 'r1',
    title: 'Noise-cancelling headphones under $300',
    format: 'markdown',
    content: '# Noise-cancelling headphones under $300\n\nThe **Sony WH-1000XM6** is the one: the quietest on planes, 30 hours of battery, and $279 at Best Buy with delivery tomorrow.\n\n## The shortlist\n\n| Model | Price | Battery |\n|---|---|---|\n| Sony WH-1000XM6 | $279 | 30 h |\n| Bose QC Ultra | $299 | 24 h |\n| Sennheiser Momentum 4 | $249 | 60 h |\n\n## Why the Sony\n\nIt’s the quietest on planes and lasts the longest of the three that fold flat.',
    data: { takeaway: 'Sony XM6 at $279 is the pick: quietest on planes, 30 h of battery.' },
    createdAt: now - 2 * 3600e3,
    updatedAt: now - 2 * 3600e3,
  },
  {
    id: 'r2',
    title: 'A long weekend in Lisbon',
    format: 'markdown',
    content: '# A long weekend in Lisbon\n\nThree days, two neighbourhoods, one very good custard tart. Fly out Friday evening on TAP for $412 return.\n\n## Friday\n\n- Land at 21:40, taxi to Alfama',
    createdAt: now - 26 * 3600e3,
    updatedAt: now - 26 * 3600e3,
  },
  {
    id: 'r3',
    title: 'Hacker News, top 5 right now',
    format: 'markdown',
    content: '# Hacker News, top 5 right now\n\nMistral Large 4 leads with 1,277 points, ahead of a deep dive on SQLite’s query planner.',
    data: { preview: { kinds: '5 stories, a comparison' } },
    createdAt: now - 4 * 864e5,
    updatedAt: now - 4 * 864e5,
  },
]

export const logins: Login[] = [{ id: 1, site: 'bestbuy.com', username: 'alex@example.com', createdAt: now - 864e5, updatedAt: now - 864e5 }]

// A whole exchange that has just finished, with follow-ups under its say.
export function finished(): Entry[] {
  return [
    entry('user', 'Find me the best noise-cancelling headphones under $300 and check which store can deliver by Friday.', { data: { start: true } }, 9),
    entry('step', 'Searching the web', { status: 'done', doneText: 'Searched the web for “best noise cancelling headphones 2026”' }, 8),
    entry('step', 'Reading rtings.com', { status: 'done', doneText: 'Read rtings.com' }, 7),
    entry('step', 'Checking delivery dates', { status: 'done', doneText: 'Checked delivery dates on bestbuy.com' }, 5),
    entry('report', 'Noise-cancelling headphones under $300', { data: { id: 'r1' } }, 2),
    entry('say', 'The Sony WH-1000XM6 is the one: the quietest on planes, 30 hours of battery, and $279 at Best Buy with delivery tomorrow. The details are on https://www.bestbuy.com/site/sony-wh1000xm6 and the full comparison is in the report.', { data: { final: true, suggestions: ['Order the Sony', 'Compare battery life', 'Watch the price for a week'] } }, 1),
  ]
}

// What Alexa did over the last day, for the home board's Recent list: a
// scheduled briefing, a quick question (too short to list), a long
// comparison that wrote no report and a research that wrote one (r1).
// With the reports, the list is r1, the flights, the briefing and r2; r3
// is a fifth, one too many.
export function past(): Entry[] {
  const h = 60
  return [
    entry('note', 'Scheduled: Morning briefing', { data: { start: true, scheduled: true } }, 25 * h + 4),
    entry('step', 'Searching the web', { status: 'done', doneText: 'Searched the web for today’s headlines' }, 25 * h + 3),
    entry('step', 'Reading bbc.com', { status: 'done', doneText: 'Read bbc.com' }, 25 * h + 2),
    entry('say', 'Three things today: a strike on the Northern line, 14° and dry, and the dentist at 3.', { data: { final: true } }, 25 * h),
    entry('user', 'What time does the Tate Modern close today?', { data: { start: true } }, 20 * h + 1),
    entry('step', 'Searching the web', { status: 'done', doneText: 'Searched the web for “Tate Modern opening hours”' }, 20 * h + 1),
    entry('say', 'At 6pm today, and at 10pm on Fridays.', { data: { final: true } }, 20 * h),
    entry('user', 'Compare flights to Lisbon for the second weekend of November', { data: { start: true } }, 6 * h + 12),
    entry('step', 'Opening google.com/flights', { status: 'done', doneText: 'Opened google.com/flights' }, 6 * h + 11),
    entry('step', 'Searching flights', { status: 'done', doneText: 'Searched London to Lisbon, 13 to 16 November' }, 6 * h + 10),
    entry('step', 'Checking flytap.com', { status: 'done', doneText: 'Checked fares on flytap.com' }, 6 * h + 8),
    entry('step', 'Checking easyjet.com', { status: 'done', doneText: 'Checked fares on easyjet.com' }, 6 * h + 6),
    entry('step', 'Checking ba.com', { status: 'done', doneText: 'Checked fares on ba.com' }, 6 * h + 4),
    entry('say', 'TAP is the one: $412 return, out Friday at 18:05 and back Monday at 20:40. easyJet is $40 less but lands after midnight.', { data: { final: true } }, 6 * h + 3),
    entry('user', 'Find me the best noise-cancelling headphones under $300.', { data: { start: true } }, 2 * h + 9),
    entry('step', 'Searching the web', { status: 'done', doneText: 'Searched the web for “best noise cancelling headphones 2026”' }, 2 * h + 8),
    entry('step', 'Reading rtings.com', { status: 'done', doneText: 'Read rtings.com' }, 2 * h + 6),
    entry('report', 'Noise-cancelling headphones under $300', { data: { id: 'r1' } }, 2 * h + 1),
    entry('say', 'The Sony WH-1000XM6 is the one. The full comparison is in the report.', { data: { final: true } }, 2 * h),
  ]
}

// An exchange that was stopped part of the way through.
function stopped(): Entry[] {
  return [
    entry('user', 'Compare flights to Lisbon for the second weekend of November', { data: { start: true } }, 420),
    entry('step', 'Opening google.com/flights', { status: 'done', doneText: 'Opened google.com/flights' }, 419),
    entry('note', 'Stopped', {}, 417),
  ]
}

// A scheduled job that's just started off screen, after a finished
// exchange.
export function job(): Entry[] {
  return [entry('note', 'Scheduled: Morning briefing', { data: { start: true, scheduled: true } }, 0), entry('step', 'Searching the web', { status: 'running' }, 0)]
}

// jobBrowsing is the job a little later, in the browser, as a server with
// step data (WP-05) records it.
export function jobBrowsing(): Entry[] {
  const [note, search] = job()
  return [
    note,
    { ...search, status: 'done', doneText: 'Searched the web for today’s headlines', data: { step: { tool: 'web_search', kind: 'search', status: 'done' } } },
    entry('step', 'Reading bbc.com', { status: 'running', data: { step: { tool: 'browser_open', kind: 'browse', status: 'running', host: 'bbc.com' } } }, 0),
  ]
}

// waiting is an exchange left on a question this morning: the board shows
// the question first, under Needs you.
export function waiting(): { feed: Entry[]; ask: Entry } {
  const user = entry('user', 'Order the Sony XM6 from Best Buy, whichever colour is in stock for Friday.', { data: { start: true } }, 8 * 60)
  const ask = entry('ask', 'Black and silver both arrive Friday. Which colour should I order?', { status: 'open', data: { options: ['Black', 'Silver', 'Either is fine'], recommended: 0 } }, 7 * 60)
  return { feed: [...stopped(), user, ask], ask }
}
