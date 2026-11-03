import type { Entry } from '../../api/types'

// Conversation fixtures: one or more entries of every kind, for the gallery
// and for tests. Times are relative to now, so they read naturally.

const min = 60e3
const t0 = Date.now() - 12 * min
let next = 1000

function entry(kind: string, text: string, rest: Partial<Entry> = {}, at = 0): Entry {
  const id = next++
  const createdAt = t0 + at * min
  return { id, kind, text, createdAt, updatedAt: createdAt + (rest.status === 'running' ? 0 : 20e3), ...rest }
}

export const user = entry('user', 'Find me the best noise-cancelling headphones under $300 and check which store can deliver by Friday.', { data: { start: true } })
export const userWithFile = entry('user', 'Here’s the receipt from last time.', { data: { attachments: [{ name: 'receipt-2026-09.pdf', path: 'uploads/receipt-2026-09.pdf', mime: 'application/pdf', size: 48213 }] } })
export const say = entry('say', 'The Sony WH-1000XM6 is the one: the quietest on planes, 30 hours of battery, and $279 at Best Buy with delivery tomorrow. Details are on https://www.bestbuy.com/site/sony-wh1000xm6.', {}, 6)
export const sayWithBlocks = entry('say', 'Here are the three worth considering.', { data: { blocks: [{ type: 'stats', items: [{ label: 'Lowest price', value: '$279' }] }], suggestions: ['Order the Sony', 'Compare battery life'] } }, 7)
export const askOpen = entry('ask', 'Which colour would you like?', { status: 'open', data: { options: ['Black', 'Silver', 'Midnight blue'] } }, 8)
export const askFree = entry('ask', 'Should I use the card ending 4242, or a different one?', { status: 'open' }, 8)
export const askAnswered = entry('ask', 'Which colour would you like?', { status: 'answered', data: { options: ['Black', 'Silver'] } }, 8)
export const takeoverOpen = entry('takeover', 'Best Buy wants a verification code sent to your phone. Could you enter it?', { status: 'open' }, 9)
export const takeoverDone = entry('takeover', 'Best Buy wanted a verification code.', { status: 'answered' }, 9)
export const loginOpen = entry('login', 'Your Best Buy session expired, so I need to sign in again to place the order.', { status: 'open', data: { site: 'bestbuy.com', username: 'alex@example.com' } }, 9)
export const report = entry('report', 'Noise-cancelling headphones under $300', { data: { id: 'r-fixture' } }, 10)
export const reportUpdated = entry('report', 'Noise-cancelling headphones under $300', { data: { id: 'r-fixture', updated: true } }, 11)
export const error = entry('error', 'The model didn’t answer in time. Try again in a moment.', {}, 11)
export const note = entry('note', 'Stopped', {}, 11)
export const plan = entry('plan', 'Find headphones', { data: { plan: { title: 'Find headphones', steps: [{ text: 'Search reviews', status: 'done' }, { text: 'Compare prices', status: 'active' }, { text: 'Check delivery', status: 'pending' }] } } }, 1)
export const unknown = entry('poll', 'A kind this client doesn’t know yet shows its text.', {}, 11)

export const steps: Entry[] = [
  entry('step', 'Searching the web', { status: 'done', doneText: 'Searched the web for “best noise cancelling headphones 2026”' }, 1),
  entry('step', 'Reading rtings.com', { status: 'done', doneText: 'Read rtings.com' }, 2),
  entry('step', 'Opening bestbuy.com', { status: 'done', doneText: 'Opened bestbuy.com' }, 3),
  entry('step', 'You took over', { status: 'done' }, 4),
  entry('step', 'Clicking “Add to cart”', { status: 'failed' }, 4),
  entry('step', 'Checking delivery dates', { status: 'done', doneText: 'Checked delivery dates' }, 5),
]
export const runningStep = entry('step', 'Comparing prices on amazon.com', { status: 'running' }, 5)

// A whole exchange, as the main screen or Reflect shows it.
export const exchange: Entry[] = [user, ...steps, say, report]
