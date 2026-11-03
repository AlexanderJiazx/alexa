import type { Entry } from '../api/types'
import { firstLine } from '../features/palette/context'

// Reflect reads the conversation as exchanges (WP-12, IX-F13): each request
// and everything Alexa did about it, under a header with its time, what it
// was and how it ended. Pure, so it's tested without a DOM.

export type Outcome = 'done' | 'stopped' | 'failed' | 'waiting'

export type Exchange = {
  id: number // the first entry's id
  at: number // when it began
  title: string
  // ownWords is true when the title is the user's own request, which the
  // exchange shows anyway as their message.
  ownWords: boolean
  scheduled: boolean
  outcome: Outcome
  reports: number
  entries: Entry[]
}

// Before exchanges were marked, a pause this long between messages is the
// best guess at a new one.
const gap = 45 * 60e3

// quietJob says whether a scheduled job hasn't had anything to say yet:
// only its note, its plan and its steps so far.
const quietKinds = new Set(['note', 'plan', 'step'])
export const quietJob = (entries: Entry[]) => !!entries[0]?.data?.scheduled && entries.every((e) => quietKinds.has(e.kind))

// toExchanges splits the feed where a topic begins: a message that set
// Alexa to work (data.start) without carrying on the one before
// (data.followUp), or a scheduled job. A follow-up stays under its topic's
// header, as on the main screen, unless a scheduled job that had nothing
// to say came in between: it carries on the topic before the job, which
// has its own header already. A feed with no marks at all is split at each
// message after a long pause.
export function toExchanges(feed: Entry[]): Exchange[] {
  const marked = feed.some((e) => e.data?.start)
  const out: Entry[][] = []
  const afterQuietJob = () => !!out.length && quietJob(out[out.length - 1])
  feed.forEach((e, i) => {
    const prev = feed[i - 1]
    const begins = i === 0 || e.data?.scheduled || (marked ? !!e.data?.start && (!e.data.followUp || afterQuietJob()) : e.kind === 'user' && e.createdAt - prev.createdAt > gap)
    if (begins) out.push([e])
    else out[out.length - 1].push(e)
  })
  return out.map(describe)
}

function describe(entries: Entry[]): Exchange {
  const first = entries[0]
  const scheduled = !!first.data?.scheduled
  const ask = entries.find((e) => e.kind === 'user')
  const title = scheduled ? first.text.replace(/^Scheduled:\s*/, '').trim() || 'Scheduled' : ask ? firstLine(ask.text, 80) : firstLine(first.text, 80) || 'Earlier'
  const ownWords = !scheduled && !!ask
  return { id: first.id, at: first.createdAt, title, ownWords, scheduled, outcome: outcome(entries), reports: entries.filter((e) => e.kind === 'report').length, entries }
}

// outcome is how an exchange ended, from its last word.
function outcome(entries: Entry[]): Outcome {
  for (let i = entries.length - 1; i >= 0; i--) {
    const e = entries[i]
    if (e.kind === 'step' || e.kind === 'plan') continue
    if (e.kind === 'error') return 'failed'
    if (isStopNote(e)) return 'stopped'
    if ((e.kind === 'ask' || e.kind === 'takeover' || e.kind === 'login') && e.status === 'open') return 'waiting'
    if (e.kind === 'note') continue
    return 'done'
  }
  return 'done'
}

// How an ask was answered is read the same way here and on the main
// screen (features/ask/answer.ts).
export { answerOf, picksIn, type Answer } from '../features/ask/answer'

// matches says whether an exchange mentions what was searched for.
export function matches(ex: Exchange, query: string): boolean {
  const q = query.trim().toLowerCase()
  if (!q) return true
  return ex.title.toLowerCase().includes(q) || ex.entries.some((e) => e.kind !== 'step' && e.text.toLowerCase().includes(q))
}

// sameDay says whether two times fall on the same calendar day here.
export function sameDay(a: number, b: number) {
  return new Date(a).toDateString() === new Date(b).toDateString()
}

// isStopNote tells the note the server adds when the user stops a run.
export function isStopNote(e: Entry): boolean {
  return e.kind === 'note' && /^stopped/i.test(e.text)
}
