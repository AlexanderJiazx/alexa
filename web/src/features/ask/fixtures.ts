import type { Block } from '../../api/blocks'
import type { Entry, Needs } from '../../api/types'

// Ask fixtures: every kind of needs-you card, the failures and the stopped
// note, for the gallery (?gallery=ask) and the tests. Times are relative to
// now, so they read naturally.

const min = 60e3
const t0 = Date.now() - 8 * min
let next = 2000

function entry(kind: string, text: string, rest: Partial<Entry> = {}, at = 0): Entry {
  const id = next++
  const createdAt = t0 + at * min
  return { id, kind, text, createdAt, updatedAt: createdAt, ...rest }
}

export const receipt: Block = {
  type: 'receipt',
  status: 'draft',
  merchant: 'bestbuy.com',
  lines: [
    { label: 'Sony WH-1000XM6 · Black', value: '$279.00' },
    { label: 'Shipping', value: 'Free · Thu Nov 6' },
    { label: 'Tax', value: '$24.76' },
    { label: 'Pay with', value: 'Visa ···· 4242 (saved on site)' },
  ],
  total: { amount: 303.76, currency: 'USD' },
}

// A single choice, with Alexa recommending the second: the primary isn't
// always the first.
export const choice = entry('ask', 'Which colour would you like?', { status: 'open', data: { options: ['Black', 'Silver', 'Midnight blue'], recommended: 1 } }, 1)
// No preference: every option looks the same.
export const choiceEven = entry('ask', 'Tea or coffee for the meeting?', { status: 'open', data: { options: ['Tea', 'Coffee', 'Both'] } }, 1)
// Long options read as rows.
export const choiceRows = entry(
  'ask',
  'Which delivery should I pick?',
  { status: 'open', data: { options: ['Tomorrow by 9 pm, $5.99 express', 'Friday, free standard shipping', 'Pick up today at the Union Square store'], recommended: 0 } },
  2,
)
export const multi = entry('ask', 'Which stores should I watch for a price drop?', { status: 'open', data: { options: ['Amazon', 'Best Buy', 'Target', 'B&H'], multi: true } }, 2)
export const form = entry(
  'ask',
  'A few details for the hotel search?',
  {
    status: 'open',
    data: {
      form: {
        submit: 'Search',
        fields: [
          { name: 'check_in', label: 'Check-in', type: 'date', value: '2026-11-14', required: true },
          { name: 'check_out', label: 'Check-out', type: 'date', value: '2026-11-16', required: true },
          { name: 'guests', label: 'Guests', type: 'stepper', value: 2, min: 1, max: 8 },
          { name: 'area', label: 'Area', type: 'select', options: ['Alfama', 'Chiado', 'Príncipe Real', 'Anywhere'] },
          { name: 'budget', label: 'Max per night', type: 'number', min: 40, max: 900, placeholder: '€' },
          { name: 'breakfast', label: 'Breakfast included', type: 'toggle', value: true },
        ],
      },
    },
  },
  3,
)
// A form with a field the client refuses to draw.
export const formSecret = entry(
  'ask',
  'To finish the booking I need a couple of things.',
  {
    status: 'open',
    data: {
      form: {
        fields: [
          { name: 'name', label: 'Name on the booking', type: 'text', required: true, placeholder: 'As on your passport' },
          { name: 'password', label: 'Account password', type: 'text' },
          { name: 'cvv', label: 'CVV', type: 'number' },
          { name: 'rooms', label: 'Rooms', type: 'multiselect', options: ['Double', 'Twin', 'Suite'] },
        ],
      },
    },
  },
  3,
)
export const confirm = entry(
  'ask',
  'Place this order for $303.76?',
  { status: 'open', data: { confirm: true, options: ['Place order', 'Change something', 'Cancel'], recommended: 0, blocks: [receipt] } },
  4,
)
export const confirmSend = entry('ask', 'Send this email to Sam Lee now?', { status: 'open', data: { confirm: true, options: ['Send it', 'Edit the email', 'Don’t send'], blocks: [{ type: 'callout', tone: 'info', text: 'Subject: Dinner on Saturday. “Hi Sam, does 7:30 at Via Carota still work for you?”' }] } }, 4)
export const free = entry('ask', 'What name should go on the reservation?', { status: 'open' }, 5)
export const answered = entry('ask', 'Which colour would you like?', { status: 'answered', data: { options: ['Black', 'Silver', 'Midnight blue'], recommended: 1 } }, 1)
// The replies that answered it, and a multi-select and a form answered,
// for the answered cards as the main screen and Reflect show them.
export const answeredReply = entry('user', 'Silver', {}, 1.2)
export const answeredMulti = entry('ask', 'Which stores should I watch for a price drop?', { status: 'answered', data: { options: ['Amazon', 'Best Buy', 'Target', 'B&H'], multi: true } }, 2)
export const answeredMultiReply = entry('user', 'Amazon, B&H', { data: { via: 'multi' } }, 2.1)
export const answeredForm = entry('ask', 'Where to?', { status: 'answered', data: { form: { fields: [{ name: 'city', label: 'City', type: 'text', placeholder: 'Lisbon', required: true }] } } }, 3)
export const answeredFormReply = entry('user', 'City: Porto · 2 nights · Breakfast: Yes', { data: { via: 'form' } }, 3.1)
export const unanswered = entry('ask', 'Want me to book the earlier train too?', { status: 'answered', data: { options: ['Yes', 'No'] } }, 4)

export const takeover = entry('takeover', 'Best Buy wants a verification code sent to your phone. Could you enter it?', { status: 'open' }, 5)
export const login = entry('login', 'Your Best Buy session expired, so I need to sign in again to place the order.', { status: 'open', data: { site: 'bestbuy.com', username: 'alex@example.com' } }, 5)

// What the session says while the colour question waits, for the dock and
// the caption. Its entry isn't on the page, so the dock shows.
export const needsChoice: Needs = { kind: 'question', message: 'Which colour would you like? The Midnight blue ships a week later.', options: ['Black', 'Silver', 'Midnight blue'], recommended: 1, entryId: 1 }

// ---------- failures (Appendix B.4) and a stop ----------

export const failRetry = entry('error', 'The model service is overloaded.', { data: { failure: { reason: 'model_unavailable', retryable: true, partial: true, detail: 'The model service didn’t answer after 4 tries.' } } }, 6)
export const failWait = entry('error', 'Too many requests.', { data: { failure: { reason: 'rate_limited', retryable: true, retryAt: Date.now() + 47e3 } } }, 6)
export const failInput = entry('error', 'An attachment couldn’t be read.', { data: { failure: { reason: 'invalid_input', retryable: false, detail: 'receipt.heic couldn’t be read by the model.' } } }, 6)
export const failLoop = entry('error', 'Alexa kept repeating the same step.', { data: { failure: { reason: 'loop_blocked', retryable: false, detail: 'Clicked “Next” on the same page 6 times.' } } }, 6)
export const failOld = entry('error', 'The model didn’t answer in time. Try again in a moment.', {}, 6)
export const stopped = entry('note', 'Stopped', {}, 7)
