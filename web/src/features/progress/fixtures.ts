// Progress fixtures, for the gallery and the specs: a 14-step run on three
// sites with full step data (Appendix B.2), the same kind of run from a
// server that sends no step data, runs that ended done, stopped and
// failed, plans in every state, and run events for each phase. Times are
// relative to now, so the timers read naturally.

import type { Source } from '../../api/blocks'
import type { Plan, StepData } from '../../api/progress'
import type { Entry } from '../../api/types'
import type { RunInfo } from '../../state/reducer'

const sec = 1e3
const min = 60e3
let next = 5000

// t0 is when the fixture runs started: a little over three minutes ago.
export const t0 = Date.now() - 3 * min - 12 * sec

function entry(kind: string, text: string, at: number, rest: Partial<Entry> = {}, ms = 0): Entry {
  const id = next++
  const createdAt = t0 + at
  return { id, kind, text, createdAt, updatedAt: createdAt + ms, ...rest }
}

// step builds a step entry: the model's two labels, and what the executor
// recorded.
function step(text: string, doneText: string, at: number, d: Partial<StepData> & Pick<StepData, 'kind'>, ms = 2 * sec): Entry {
  const status = d.status ?? 'done'
  const data: StepData = { tool: 'browser_click', status, ok: status === 'done', ms, ...d }
  return entry('step', text, at, { status, doneText: status === 'done' ? doneText : '', data: { step: data } }, ms)
}

const src = (url: string, title?: string): Source => ({ url, title })

export const user = entry('user', 'Find me the best noise-cancelling headphones under $300, and check who can deliver by Friday.', 0, { data: { start: true } })

// ---------- 14 steps on 3 sites, with step data ----------

export const run14: Entry[] = [
  step('Searching for “best noise cancelling headphones 2026”', 'Searched for “best noise cancelling headphones 2026”', 4 * sec, {
    tool: 'web_search',
    kind: 'search',
    query: 'best noise cancelling headphones 2026',
    plan: 0,
    sources: [
      src('https://www.rtings.com/headphones/reviews/best/noise-cancelling', 'The 6 Best Noise Cancelling Headphones'),
      src('https://www.nytimes.com/wirecutter/reviews/best-noise-cancelling-headphones/', 'The Best Noise-Cancelling Headphones'),
      src('https://www.soundguys.com/best-noise-cancelling-headphones-1186/', 'Best noise cancelling headphones'),
      src('https://www.theverge.com/23300263/best-noise-canceling-headphones', 'The best noise-canceling headphones'),
    ],
  }),
  step('Searching for “Sony WH-1000XM6 review”', 'Searched for “Sony WH-1000XM6 review”', 9 * sec, {
    tool: 'web_search',
    kind: 'search',
    query: 'Sony WH-1000XM6 review',
    plan: 0,
    sources: [src('https://www.rtings.com/headphones/reviews/sony/wh-1000xm6'), src('https://www.whathifi.com/reviews/sony-wh-1000xm6'), src('https://www.techradar.com/audio/sony-wh-1000xm6-review')],
  }),
  step('Reading rtings.com', 'Read rtings.com', 14 * sec, { tool: 'web_search', kind: 'read', host: 'rtings.com', url: 'https://www.rtings.com/headphones/reviews/sony/wh-1000xm6', plan: 0 }, 3 * sec),
  step('Searching for “Bose QuietComfort Ultra price”', 'Searched for “Bose QuietComfort Ultra price”', 20 * sec, {
    tool: 'web_search',
    kind: 'search',
    query: 'Bose QuietComfort Ultra price',
    plan: 0,
    sources: [src('https://www.bose.com/p/headphones/bose-quietcomfort-ultra-headphones')],
  }),
  step('Searching for “AirPods Max 2 deal”', 'Searched for “AirPods Max 2 deal”', 26 * sec, {
    tool: 'web_search',
    kind: 'search',
    query: 'AirPods Max 2 deal',
    plan: 0,
    sources: [src('https://www.apple.com/airpods-max/'), src('https://www.theverge.com/23300263/best-noise-canceling-headphones')],
  }),
  step('Opening Amazon', 'Opened Amazon', 41 * sec, { tool: 'browser_open', kind: 'browse', host: 'amazon.com', url: 'https://www.amazon.com/', plan: 1 }),
  step('Searching Amazon for the Sony', 'Searched Amazon for the Sony', 47 * sec, { tool: 'browser_type', kind: 'type', host: 'amazon.com', target: 'Search Amazon', plan: 1 }),
  step('Opening the listing', 'Opened the listing', 53 * sec, { tool: 'browser_click', kind: 'click', host: 'amazon.com', target: 'Sony WH-1000XM6 Wireless', plan: 1 }),
  step('Checking the price', 'Checked the price', 60 * sec, { tool: 'browser_read', kind: 'read', host: 'amazon.com', plan: 1 }),
  step('Checking delivery options', 'Checked delivery options', 68 * sec, { tool: 'browser_click', kind: 'click', host: 'amazon.com', target: 'Delivery options', plan: 1 }),
  step('Opening Best Buy', 'Opened Best Buy', 82 * sec, { tool: 'browser_open', kind: 'browse', host: 'bestbuy.com', url: 'https://www.bestbuy.com/', plan: 2 }),
  step('Checking store pickup', 'Checked store pickup', 90 * sec, { tool: 'browser_click', kind: 'click', host: 'bestbuy.com', target: 'Pickup', plan: 2 }),
  step('Clicking “Check other stores”', 'Clicked “Check other stores”', 97 * sec, {
    tool: 'browser_click',
    kind: 'click',
    host: 'bestbuy.com',
    target: 'Check other stores',
    status: 'failed',
    error: 'A sign-up pop-up covered the button',
    plan: 2,
  }),
  step('Checking delivery dates', 'Checked delivery dates', 104 * sec, { tool: 'browser_read', kind: 'read', host: 'bestbuy.com', plan: 2 }, 4 * sec),
]

export const finalSay = entry('say', 'The Sony WH-1000XM6 is the one: the quietest on planes, 30 hours of battery, and $279 at Best Buy with delivery on Thursday.', 3 * min + 12 * sec, { data: { final: true } })

// ---------- the same kind of run from a server with no step data ----------

export const plainRun: Entry[] = [
  entry('step', 'Searching the web', 3 * sec, { status: 'done', doneText: 'Searched the web for “best noise cancelling headphones 2026”' }, 3 * sec),
  entry('step', 'Searching the web', 8 * sec, { status: 'done', doneText: 'Searched the web for “Sony WH-1000XM6 vs Bose QC Ultra”' }, 3 * sec),
  entry('step', 'Reading rtings.com', 13 * sec, { status: 'done', doneText: 'Read rtings.com' }, 4 * sec),
  entry('step', 'Opening bestbuy.com', 21 * sec, { status: 'done', doneText: 'Opened bestbuy.com' }, 2 * sec),
  entry('step', 'Comparing prices on bestbuy.com', 25 * sec, { status: 'done', doneText: 'Compared prices on bestbuy.com' }, 6 * sec),
  entry('step', 'You took over', 34 * sec, { status: 'done' }, 40 * sec),
  entry('step', 'Clicking “Add to cart”', 76 * sec, { status: 'failed' }, 2 * sec),
  entry('step', 'Checking delivery dates', 80 * sec, { status: 'done', doneText: 'Checked delivery dates' }, 3 * sec),
  entry('step', 'Saving the comparison to headphones.md', 86 * sec, { status: 'done', doneText: 'Saved the comparison to headphones.md' }, 1 * sec),
  entry('step', 'Remembering that you prefer over-ear headphones', 88 * sec, { status: 'done', doneText: 'Remembered that you prefer over-ear headphones' }, 1 * sec),
]

// ---------- three runs that ended ----------

// done: the 14 steps, then the answer.
export const doneRun: Entry[] = [user, ...run14, finalSay]

// stopped: the user pressed Stop mid-step.
const stopUser = entry('user', 'Book me a table for two at Dishoom Shoreditch on Saturday at 8.', 0, { data: { start: true } })
export const stoppedSteps: Entry[] = [
  step('Opening OpenTable', 'Opened OpenTable', 3 * sec, { tool: 'browser_open', kind: 'browse', host: 'opentable.co.uk' }),
  step('Searching for Dishoom Shoreditch', 'Searched for Dishoom Shoreditch', 9 * sec, { tool: 'browser_type', kind: 'type', host: 'opentable.co.uk', target: 'Location, restaurant' }),
  step('Opening the restaurant', 'Opened the restaurant', 15 * sec, { tool: 'browser_click', kind: 'click', host: 'opentable.co.uk' }),
  step('Choosing Saturday', 'Chose Saturday', 22 * sec, { tool: 'browser_click', kind: 'click', host: 'opentable.co.uk', target: 'Sat 2 Nov' }),
  step('Checking times around 8pm', 'Checked times around 8pm', 28 * sec, { tool: 'browser_read', kind: 'read', host: 'opentable.co.uk' }),
  step('Opening dishoom.com', 'Opened dishoom.com', 36 * sec, { tool: 'browser_open', kind: 'browse', host: 'dishoom.com', status: 'interrupted' }),
]
export const stoppedRun: Entry[] = [stopUser, ...stoppedSteps, entry('note', 'Stopped', 40 * sec)]

// failed: the sign-in didn't work and the run gave up.
const failUser = entry('user', 'Check me in for my United flight tomorrow and get the boarding pass.', 0, { data: { start: true } })
export const failedSteps: Entry[] = [
  step('Opening united.com', 'Opened united.com', 3 * sec, { tool: 'browser_open', kind: 'browse', host: 'united.com' }),
  step('Opening check-in', 'Opened check-in', 9 * sec, { tool: 'browser_click', kind: 'click', host: 'united.com', target: 'Check-in' }),
  step('Entering your confirmation number', 'Entered your confirmation number', 15 * sec, { tool: 'browser_type', kind: 'type', host: 'united.com', target: 'Confirmation number' }),
  step('Signing in to United', 'Signed in to United', 22 * sec, { tool: 'browser_login', kind: 'type', host: 'united.com', status: 'failed', error: 'Couldn’t sign in to united.com: the password was turned down' }),
  step('Trying the sign-in again', 'Tried the sign-in again', 30 * sec, { tool: 'browser_login', kind: 'type', host: 'united.com', status: 'failed', error: 'Couldn’t sign in to united.com' }),
  step('Checking for another way in', 'Checked for another way in', 38 * sec, { tool: 'browser_read', kind: 'read', host: 'united.com' }),
]
export const failedError = entry('error', 'I couldn’t sign in to united.com, so I stopped.', 44 * sec, {
  data: { failure: { reason: 'tool_crashed', retryable: false, partial: true, detail: 'Couldn’t sign in to united.com' } },
})
export const failedRun: Entry[] = [failUser, ...failedSteps, failedError]

// ---------- a long live run, for the fold ----------

export const longRun: Entry[] = [
  ...run14.slice(0, 10),
  step('Opening Walmart', 'Opened Walmart', 74 * sec, { tool: 'browser_open', kind: 'browse', host: 'walmart.com', plan: 1 }),
  step('Checking the price', 'Checked the price', 79 * sec, { tool: 'browser_read', kind: 'read', host: 'walmart.com', plan: 1 }),
  step('Saving the prices to prices.csv', 'Saved the prices to prices.csv', 84 * sec, { tool: 'write_file', kind: 'file', plan: 1 }, 1 * sec),
  step('Reading prices.csv', 'Read prices.csv', 86 * sec, { tool: 'read_file', kind: 'file', plan: 1 }, 1 * sec),
  ...run14.slice(10, 13),
]
export const runningStep = step('Checking delivery dates on bestbuy.com', '', 104 * sec, { tool: 'browser_read', kind: 'read', host: 'bestbuy.com', status: 'running', plan: 2 }, 0)

// memory and schedule steps, which show as chips.
export const chipSteps: Entry[] = [
  step('Remembering your size', 'Remembered you wear a medium', 2 * sec, { tool: 'memory_save', kind: 'memory' }, 1 * sec),
  step('Scheduling the price check', 'Scheduled a price check for Friday at 9am', 4 * sec, { tool: 'schedule_create', kind: 'schedule' }, 1 * sec),
]

// ---------- plans ----------

export const planLive: Plan = {
  title: 'Find headphones under $300',
  steps: [
    { text: 'Read the reviews', status: 'done' },
    { text: 'Compare prices', status: 'done' },
    { text: 'Check delivery by Friday', status: 'active' },
    { text: 'Pick one', status: 'pending' },
  ],
}
export const planMixed: Plan = {
  title: 'Book a table for Saturday',
  steps: [
    { text: 'Find Dishoom on OpenTable', status: 'done' },
    { text: 'Book 8pm on OpenTable', status: 'failed', note: 'No table for two between 7 and 9' },
    { text: 'Try the restaurant’s own site', status: 'active' },
    { text: 'Join the waitlist', status: 'skipped', note: 'Not needed if the site has a table' },
    { text: 'Send you the confirmation', status: 'pending' },
  ],
  note: 'Saturdays book up fast; I’ll take the closest time to 8pm.',
}
export const planDone: Plan = {
  title: 'Find headphones under $300',
  steps: [
    { text: 'Read the reviews', status: 'done' },
    { text: 'Compare prices', status: 'done' },
    { text: 'Check delivery by Friday', status: 'done' },
    { text: 'Pick one', status: 'done' },
  ],
}
export const planDoneEntry = entry('plan', planDone.title ?? '', 2 * sec, { data: { plan: planDone } }, 3 * min)
export const planMixedEntry = entry('plan', planMixed.title ?? '', 2 * sec, { data: { plan: planMixed } }, 36 * sec)

// ---------- the session at work on a plan ----------

// The live exchange is a fresh copy of the run, so it can follow the ended
// runs in one feed (which is in id order).
const copy = (e: Entry): Entry => ({ ...e, id: next++ })
const liveUser = copy(user)
export const planEntry = entry('plan', planLive.title ?? '', 2 * sec, { data: { plan: planLive } }, 100 * sec)
export const liveExchange: Entry[] = [liveUser, planEntry, ...run14.slice(0, 13).map(copy), copy(runningStep)]

// feed is every fixture run in one conversation, oldest first: what Reflect
// would show, with the live exchange last.
export const feed: Entry[] = [...doneRun, ...stoppedRun, ...failedRun, ...liveExchange]

// ---------- run events, one per phase ----------

const ev = (r: Omit<RunInfo, 'runId' | 'at'>): RunInfo => ({ runId: 'r1', at: Date.now(), ...r })
export const runs = {
  thinking: ev({ phase: 'thinking', headline: 'Weighing battery life against price' }),
  thinkingBare: ev({ phase: 'thinking' }),
  tool: ev({ phase: 'tool', tool: { name: 'browser_read', label: 'Checking delivery dates', kind: 'read', host: 'bestbuy.com' } }),
  search: ev({ phase: 'tool', tool: { name: 'web_search', label: 'Searching for “Sony WH-1000XM6 review”', kind: 'search' } }),
  retrying: ev({ phase: 'retrying', retry: { attempt: 2, of: 4, reason: 'rate_limited', at: Date.now() + 8 * sec } }),
  compacting: ev({ phase: 'compacting' }),
  waiting: ev({ phase: 'waiting', tool: { name: 'browser_wait', label: 'Waiting for the page to load', kind: 'browse', host: 'bestbuy.com' } }),
  stopping: ev({ phase: 'stopping' }),
  writingSay: ev({ phase: 'writing', target: 'say' }),
  writingAsk: ev({ phase: 'writing', target: 'ask' }),
  writingReport: ev({ phase: 'writing', target: 'report' }),
}
