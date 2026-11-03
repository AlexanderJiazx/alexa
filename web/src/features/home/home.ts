import type { Entry, Report, Schedule } from '../../api/types'
import { quietJob, toExchanges, type Exchange } from '../../reflect/exchanges'
import { toSteps } from '../progress/group'

// The home board's words and choices, kept pure so they can be tested and
// shown in the gallery with any clock.

const day = 864e5

// greeting is the board's headline: the time of day and the user's first
// name.
export function greeting(name: string, hour: number) {
  const part = hour < 5 ? 'Up late' : hour < 12 ? 'Good morning' : hour < 18 ? 'Good afternoon' : 'Good evening'
  const first = name.trim().split(/\s+/)[0]
  return first ? `${part}, ${first}` : part
}

// nextRun is the schedule that will run soonest, if any is on.
export function nextRun(list: Schedule[], now: number): Schedule | undefined {
  let next: Schedule | undefined
  for (const s of list) if (s.enabled && s.nextRun && s.nextRun > now && (!next?.nextRun || s.nextRun < next.nextRun)) next = s
  return next
}

// when says when a schedule runs next, the way you'd say it: "in 20 min",
// "today at 7:30", "tomorrow at 7:30", "Mon at 7:30".
export function when(ts: number, now: number) {
  const mins = Math.round((ts - now) / 60e3)
  if (mins < 1) return 'now'
  if (mins < 60) return `in ${mins} min`
  const d = new Date(ts)
  const t = d.toLocaleTimeString(undefined, { hour: 'numeric', minute: '2-digit' })
  const date = (x: Date) => new Date(x.getFullYear(), x.getMonth(), x.getDate()).getTime()
  const days = Math.round((date(d) - date(new Date(now))) / day)
  if (days === 0) return `today at ${t}`
  if (days === 1) return `tomorrow at ${t}`
  if (days < 7) return `${d.toLocaleDateString(undefined, { weekday: 'short' })} at ${t}`
  return `${d.toLocaleDateString(undefined, { month: 'short', day: 'numeric' })} at ${t}`
}

// status is the quiet line under the greeting, only when it has something
// real to say: that you're offline, what Alexa is busy with off screen, or
// what runs next within a day. Otherwise it's null and the line stays
// empty, rather than filling it with something to say.
export function status(o: { offline: boolean; job?: string | null; next?: Schedule; now: number }): string | null {
  if (o.offline) return 'You’re offline. Alexa keeps working and catches you up when you’re back.'
  if (o.job) return `Working on ${o.job} in the background`
  const at = o.next?.nextRun
  if (o.next && at && at - o.now < day) return `${o.next.name} runs ${when(at, o.now)}`
  return null
}

// ---------- what Alexa finished lately ----------

// Done is one row of the board's Recent list. A report opens beside the
// board; anything else opens Reflect at the exchange it began with.
export type Done = { key: string; title: string; line: string; at: number } & ({ report: string } | { exchange: number })

const week = 7 * day

// Long-running is two minutes or more of Alexa's own time, or five tool
// steps or more. Two minutes is well past a quick answer; five steps is past
// a quick look-up (a search and a page or two), which is all most short
// questions take. Her time leaves out what she spent waiting on you, so a
// question answered an hour later doesn't make a short run long. Work she
// made a plan for counts too: she planned it because it took several
// steps, like research across a few sites.
const longMs = 2 * 60e3
const manySteps = 5

// isLong says whether Alexa worked on an exchange (or a topic of several)
// long enough that its result is worth coming back to.
function isLong(entries: Entry[]) {
  return entries.some((e) => e.kind === 'plan') || workMs(entries) >= longMs || toSteps(entries).filter((s) => s.kind !== 'user').length >= manySteps
}

// workMs is how long Alexa worked: from the first entry to the end of the
// last, less the time she waited on the user (from a question, a take-over
// or a sign-in to their next message, and the pause before each later
// exchange of a topic).
function workMs(entries: Entry[]) {
  if (!entries.length) return 0
  let waited = 0
  let asked = 0
  let end = entries[0].createdAt
  entries.forEach((e, i) => {
    if (i > 0 && (e.data?.start || e.data?.scheduled)) {
      waited += Math.max(0, e.createdAt - (asked || end))
      asked = 0
    } else if (e.kind === 'ask' || e.kind === 'takeover' || e.kind === 'login') asked ||= e.createdAt
    else if (e.kind === 'user' && asked) {
      waited += Math.max(0, e.createdAt - asked)
      asked = 0
    }
    end = Math.max(end, ended(e))
  })
  return end - entries[0].createdAt - waited
}

// ended is when an entry ended: a step when it finished, anything else
// when it was said.
const ended = (e: Entry) => (e.kind === 'step' ? Math.max(e.createdAt, e.updatedAt) : e.createdAt)


// topics reads the feed by topic, as Reflect does (toExchanges: a
// follow-up stays under the topic it carries on). A follow-up after a
// quiet scheduled job, which Reflect gives a header of its own, goes back
// with the topic before the job, the one it carries on.
function topics(feed: Entry[]): Exchange[] {
  const out: Exchange[] = []
  for (const x of toExchanges(feed)) {
    let to = -1
    if (x.entries[0]?.data?.followUp) for (let i = out.length - 1; i >= 0 && to < 0; i--) if (!quietJob(out[i].entries)) to = i
    if (to < 0) out.push(x)
    else out[to] = toExchanges([...out[to].entries, ...x.entries])[0]
  }
  return out
}

// recentWork is what the board lists under Recent: the newest few things
// Alexa finished in the last week that are worth coming back to.
//   - From the conversation, a topic (an exchange and its follow-ups) that
//     ended done and wrote a report, was a scheduled run that said
//     something (the latest run of each schedule), or was long-running
//     (isLong). It's listed under its report's title, or its first
//     request, at its latest activity.
//   - The reports list adds what she wrote up whose exchange isn't loaded.
//     A report is listed once, at its newest.
// busy says Alexa isn't idle: the latest topic is still going (a job in
// the background, a question waiting), so it isn't listed. reports is null
// while the list is loading.
export function recentWork(feed: Entry[], reports: Report[] | null, o: { now: number; busy: boolean }, n = 4): Done[] {
  const listed = new Map((reports ?? []).map((r) => [r.id, r]))
  const latest = feed[feed.length - 1]
  const rows: Done[] = []
  for (const x of topics(feed)) {
    if (x.outcome !== 'done' || (o.busy && x.entries.includes(latest))) continue
    const at = x.entries.reduce((m, e) => Math.max(m, ended(e)), 0)
    if (o.now - at > week) continue
    const r = reportOf(x.entries, listed, reports !== null)
    const line = said(x.entries)
    if (r) rows.push({ key: `r${r.id}`, title: r.title, line: preview(r) || line, at, report: r.id })
    else if (x.scheduled && line) rows.push({ key: `s${x.title}`, title: x.title, line, at, exchange: x.id })
    else if (isLong(x.entries)) rows.push({ key: `x${x.id}`, title: x.title, line, at, exchange: x.id })
  }
  for (const r of reports ?? []) if (o.now - r.updatedAt <= week) rows.push({ key: `r${r.id}`, title: r.title, line: preview(r), at: r.updatedAt, report: r.id })
  const seen = new Set<string>()
  return rows
    .sort((a, b) => b.at - a.at)
    .filter((d) => !seen.has(d.key) && !!seen.add(d.key))
    .slice(0, n)
}

// reportOf is the last report some work wrote that's still there: from
// the reports list once it's loaded (one missing from it was deleted), or
// from the report's entry until then.
function reportOf(entries: Entry[], listed: Map<string, Report>, loaded: boolean): Report | undefined {
  for (let i = entries.length - 1; i >= 0; i--) {
    const e = entries[i]
    const id = e.kind === 'report' ? e.data?.id : undefined
    if (!id) continue
    const r = listed.get(id) ?? (loaded ? undefined : { id, title: e.text, format: 'markdown', data: { takeaway: e.data?.takeaway, preview: e.data?.preview }, createdAt: e.createdAt, updatedAt: e.updatedAt })
    if (r) return r
  }
  return undefined
}

// said is the first line of what Alexa last said in an exchange.
function said(entries: Entry[]) {
  for (let i = entries.length - 1; i >= 0; i--) {
    const e = entries[i]
    if (e.kind === 'say') return firstProse(e.text) || e.data?.summary || ''
  }
  return ''
}

// preview is a line of what a report says: the takeaway Alexa gave it,
// what's inside, or the start of its text.
export function preview(r: Report) {
  const d = r.data
  if (d?.takeaway) return d.takeaway
  if (d?.preview?.stat) return `${d.preview.stat.label}: ${d.preview.stat.value}`
  if (d?.preview?.kinds) return d.preview.kinds
  if (r.format === 'html' || !r.content) return d?.items?.[0]?.label ?? ''
  return firstProse(r.content, r.title)
}

// firstProse is the first line of prose in some markdown, without the
// markup and at most 158 characters: headings, rules, tables, code, HTML
// and a line that only repeats skip are passed over.
function firstProse(text: string, skip = '') {
  for (const line of text.split('\n')) {
    const t = plain(line)
    if (t && t !== skip) return t.length > 160 ? `${t.slice(0, 157).trimEnd()}…` : t
  }
  return ''
}

// plain strips a line of markdown down to its words.
function plain(line: string) {
  const l = line.trim()
  if (!l || /^(#{1,6}\s|[-*_]{3,}$|\||```|<)/.test(l)) return ''
  return l
    .replace(/!\[[^\]]*\]\([^)]*\)/g, '')
    .replace(/\[([^\]]*)\]\([^)]*\)/g, '$1')
    .replace(/^([-*+]|\d+\.)\s+/, '')
    .replace(/^>\s?/, '')
    .replace(/[*_`~]+/g, '')
    .trim()
}
