// Step grouping (SYNTHESIS Appendix C.3): how the progress views read a run
// of steps. Consecutive steps of one kind on one site become one line,
// searches merge across sites, file and shell work is counted, and memory
// and schedule steps become chips. A server that doesn't send step data
// yet (Appendix B.2) still groups well: the kind and the site are read from
// the step's own words.

import type { Source } from '../../api/blocks'
import type { StepKind, StepStatus } from '../../api/progress'
import type { Entry } from '../../api/types'
import { fmtDuration } from '../../ui/Timer'

// Kind is a step's kind, plus the steps the user took themselves.
export type Kind = StepKind | 'user'

// Step is one step entry as the progress views read it.
export type Step = {
  id: number
  kind: Kind
  tool?: string
  host?: string
  url?: string
  // label is what it did once done, or what it's doing.
  label: string
  status: StepStatus
  error?: string
  query?: string
  sources: Source[]
  plan?: number
  start: number
  end: number
}

// The kinds of group, each drawn its own way.
export type GroupCat = 'search' | 'web' | 'files' | 'chip' | 'user' | 'other'

export type Group = {
  key: string
  cat: GroupCat
  // kind picks the glyph; host the favicon.
  kind: Kind
  host?: string
  steps: Step[]
  failed: Step[]
  status: StepStatus
  sources: Source[]
  start: number
  end: number
  // head, items and meta are the line in parts: "amazon.com" + ["Opened
  // the listing", "Checked the price"] + more, or "Searched the web" + meta
  // ["4 searches", "9 sources"]. text is the whole line in words.
  head: string
  items: string[]
  more: number
  meta: string[]
  text: string
}

const statuses = new Set<string>(['running', 'done', 'failed', 'skipped', 'interrupted'])
// Steps that happen on a site: they carry its favicon and group by it.
const webKinds = new Set<Kind>(['read', 'browse', 'click', 'type'])

// What a step's first words say it is, for steps without step data. The
// file check comes early so "Reading notes.md" isn't a site.
const fileWords = /\bfiles?\b|\bfolders?\b|\.(md|txt|csv|tsv|pdf|json|ya?ml|py|js|ts|go|sh|html?|docx?|xlsx?|pptx?|png|jpe?g|gif|svg|zip)\b/i
const verbs: [RegExp, Kind][] = [
  [/^(search\w*|look\w* up|googl\w*)\b/i, 'search'],
  [/^(remember\w*|forg[eo]t\w*|not(ed|ing) (that|down))\b/i, 'memory'],
  [/^(schedul\w*|remind\w*|set\w* (a |up a )?reminder)/i, 'schedule'],
  [/^(run\w*|ran)\b/i, 'shell'],
  [/^(generat\w*|draw\w*|paint\w*)\b/i, 'image'],
  [/^(writ\w*|wrote|draft\w*|prepar\w*|put\w* together)\b.*\breport\b/i, 'report'],
  [/^(read\w*|skim\w*|scan\w*|look\w* (at|through|over)|check\w*|compar\w*|review\w*|extract\w*)\b/i, 'read'],
  [/^(click\w*|tap\w*|press\w*|select\w*|choos\w*|chose|pick\w*|accept\w*|dismiss\w*|clos(e|es|ed|ing)|add\w* .+ to (the )?(cart|basket|bag))\b/i, 'click'],
  [/^(typ\w*|enter\w*|fill\w*|sign\w* in|log\w* in)\b/i, 'type'],
  [/^(open\w*|go\w* (to|back)|went (to|back)|visit\w*|navigat\w*|load\w*|brows\w*|return\w* to|scroll\w*|wait\w* for)\b/i, 'browse'],
]

export function kindFromText(text: string): Kind {
  const t = text.trim()
  if (/^you\b/i.test(t)) return 'user'
  if (!/^search/i.test(t) && fileWords.test(t)) return 'file'
  for (const [re, k] of verbs) if (re.test(t)) return k
  return 'other'
}

// hostFromText finds the site a step names ("Opened bestbuy.com"),
// skipping quoted text and email addresses.
export function hostFromText(text: string): string | undefined {
  const t = text.replace(/[“"‘][^”"’]*[”"’]/g, ' ')
  for (const m of t.matchAll(/(^|[^@\w.-])((?:[a-z0-9](?:[a-z0-9-]*[a-z0-9])?\.)+[a-z]{2,24})\b/gi)) {
    const h = m[2].toLowerCase().replace(/^www\./, '')
    if (!fileWords.test(h)) return h
  }
  return undefined
}

// quoted is the first quoted phrase of a label: a search's query.
export function quoted(text: string): string | undefined {
  return text.match(/[“"]([^”"]+)[”"]/)?.[1]
}

// toStep reads a step entry: its step data where the server sends it, its
// words where it doesn't.
export function toStep(e: Entry): Step {
  const d = e.data?.step
  const raw = statuses.has(e.status ?? '') ? e.status : d?.status
  const status = (raw && statuses.has(raw) ? raw : 'done') as StepStatus
  const label = status === 'done' && e.doneText ? e.doneText : e.text
  const user = /^you\b/i.test(e.text.trim())
  const kind: Kind = user ? 'user' : (d?.kind ?? kindFromText(e.text))
  const host = d?.host ?? (webKinds.has(kind) ? hostFromText(e.doneText || e.text) : undefined)
  const end = status === 'running' ? e.createdAt : d?.ms ? e.createdAt + d.ms : Math.max(e.createdAt, e.updatedAt)
  return {
    id: e.id,
    kind,
    tool: d?.tool,
    host: host || undefined,
    url: d?.url || undefined,
    label,
    status,
    error: d?.error || undefined,
    query: d?.query ?? (kind === 'search' ? quoted(e.doneText || e.text) : undefined),
    sources: d?.sources ?? [],
    plan: typeof d?.plan === 'number' ? d.plan : undefined,
    start: e.createdAt,
    end,
  }
}

export const toSteps = (entries: Entry[]) => entries.filter((e) => e.kind === 'step').map(toStep)

function catOf(s: Step, prev: Group | undefined): GroupCat {
  switch (s.kind) {
    case 'user':
      return 'user'
    case 'search':
      return 'search'
    case 'memory':
    case 'schedule':
      return 'chip'
    case 'file':
    case 'shell':
      return 'files'
  }
  // A page the search tool opened belongs to that search: it's one of its
  // sources.
  if (s.tool === 'web_search' && prev?.cat === 'search') return 'search'
  if (s.host && webKinds.has(s.kind)) return 'web'
  return 'other'
}

function keyOf(cat: GroupCat, s: Step) {
  if (cat === 'web') return `web:${s.host}`
  if (cat === 'other') return `other:${s.kind}`
  return cat
}

// groupSteps folds a run of steps into the lines the user reads.
export function groupSteps(steps: Step[]): Group[] {
  const out: Group[] = []
  let site: string | undefined
  for (const raw of steps) {
    // A click, typing or scroll without a site happens on the page Alexa
    // is on. Steps the user took don't move it; anything else does.
    const s = !raw.host && site && webKinds.has(raw.kind) ? { ...raw, host: site } : raw
    if (s.host && webKinds.has(s.kind)) site = s.host
    else if (s.kind !== 'user') site = undefined
    const last = out[out.length - 1]
    const cat = catOf(s, last)
    const key = keyOf(cat, s)
    if (last && last.key === key) last.steps.push(s)
    else out.push({ key, cat, kind: s.kind, host: cat === 'web' ? s.host : undefined, steps: [s], failed: [], status: 'done', sources: [], start: s.start, end: s.end, head: '', items: [], more: 0, meta: [], text: '' })
  }
  return out.map(finish)
}

function finish(g: Group): Group {
  const steps = g.steps
  const failed = steps.filter((s) => s.status === 'failed')
  const end = Math.max(...steps.map((s) => s.end))
  const status = groupStatus(steps)
  const sources = g.cat === 'search' ? searchSources(steps) : []
  const words = { ...wordsFor(g.cat, steps, g.host, sources, status === 'running'), sources }
  return { ...g, key: `${g.cat}-${steps[0].id}`, failed, end, status, ...words }
}

function groupStatus(steps: Step[]): StepStatus {
  if (steps.some((s) => s.status === 'running')) return 'running'
  const last = steps[steps.length - 1].status
  if (steps.every((s) => s.status === 'failed')) return 'failed'
  if (steps.every((s) => s.status === 'skipped')) return 'skipped'
  if (last === 'interrupted') return 'interrupted'
  return 'done'
}

// searchSources is every page the searches found or opened, once each.
function searchSources(steps: Step[]): Source[] {
  const out: Source[] = []
  const seen = new Set<string>()
  const add = (s: Source) => {
    if (!s.url || seen.has(s.url)) return
    seen.add(s.url)
    out.push(s)
  }
  for (const s of steps) {
    s.sources.forEach(add)
    if (s.kind !== 'search' && (s.url || s.host)) add({ url: s.url || `https://${s.host}` })
  }
  return out
}

const plural = (n: number, one: string, many = `${one}s`) => `${n} ${n === 1 ? one : many}`

// unique keeps the first of each label, in order.
function unique(labels: string[]) {
  return [...new Set(labels.filter(Boolean))]
}

// stripHost takes the site out of a label that sits under the site's name:
// "Opened amazon.com" reads "Opened" next to "amazon.com:".
export function stripHost(label: string, host: string) {
  const h = host.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
  const out = label
    .replace(new RegExp(`\\s+(on|at|from|in|to)\\s+(www\\.)?${h}\\b(’s)?`, 'i'), '')
    .replace(new RegExp(`\\s*(www\\.)?${h}\\b(’s)?`, 'i'), '')
    .replace(/\s{2,}/g, ' ')
    .replace(/[\s,:·-]+$/, '')
    .trim()
  return out || label
}

function wordsFor(cat: GroupCat, steps: Step[], host: string | undefined, sources: Source[], running: boolean) {
  // A failed step gets its own line with its error, so the group's words
  // are what worked (unless nothing did).
  const ok = steps.filter((s) => s.status !== 'failed')
  const labels = unique((ok.length ? ok : steps).map((s) => s.label))
  const two = (list: string[]) => ({ items: list.slice(0, 2), more: Math.max(0, list.length - 2) })
  switch (cat) {
    case 'search': {
      const searches = steps.filter((s) => s.kind === 'search')
      const meta: string[] = []
      let head = searches[0]?.label ?? steps[0].label
      if (searches.length > 1) {
        head = running ? 'Searching the web' : 'Searched the web'
        meta.push(plural(searches.length, 'search', 'searches'))
      }
      if (sources.length) meta.push(plural(sources.length, 'source'))
      return { head, items: [], more: 0, meta, text: [head, ...meta].join(' · ') }
    }
    case 'web': {
      if (steps.length === 1 || labels.length === 1) return { head: labels[0], items: [], more: 0, meta: [], text: labels[0] }
      const { items, more } = two(unique(labels.map((l) => stripHost(l, host ?? ''))))
      const h = host ?? ''
      return { head: h, items, more, meta: [], text: `${h}: ${items.join(', ')}${more ? `, +${more} more` : ''}` }
    }
    case 'files': {
      if (steps.length === 1) return { head: labels[0], items: [], more: 0, meta: [], text: labels[0] }
      const files = steps.filter((s) => s.kind === 'file').length
      const cmds = steps.length - files
      if (!files) {
        const head = `Ran ${plural(cmds, 'command')}`
        return { head, items: [], more: 0, meta: [], text: head }
      }
      const meta = [cmds ? `${plural(files, 'file')}, ${plural(cmds, 'command')}` : plural(files, 'file')]
      return { head: 'Worked with files', items: [], more: 0, meta, text: `Worked with files · ${meta[0]}` }
    }
    case 'chip':
      return { head: labels[0], items: labels, more: 0, meta: [], text: labels.join(', ') }
    default: {
      if (labels.length === 1) return { head: labels[0], items: [], more: 0, meta: [], text: labels[0] }
      const { items, more } = two(labels)
      return { head: '', items, more, meta: [], text: `${items.join(', ')}${more ? `, +${more} more` : ''}` }
    }
  }
}

// foldGroups keeps the last few groups of a live run. The older ones fold
// into "N earlier steps", but their failed steps stay in view.
export function foldGroups(groups: Group[], keep = 4): { shown: Group[]; hidden: number; failed: Step[] } {
  if (groups.length <= keep) return { shown: groups, hidden: 0, failed: [] }
  const old = groups.slice(0, groups.length - keep)
  return { shown: groups.slice(-keep), hidden: old.reduce((n, g) => n + g.steps.length, 0), failed: old.flatMap((g) => g.failed) }
}

export const earlierLabel = (n: number) => plural(n, 'earlier step')

// sitesOf is every site the steps were on, in the order Alexa got there.
export function sitesOf(steps: Step[]): string[] {
  const out: string[] = []
  for (const g of groupSteps(steps)) for (const s of g.steps) if (s.host && webKinds.has(s.kind) && !out.includes(s.host)) out.push(s.host)
  return out
}

// ---------- the completion summary ----------

// Outcome is how a run of steps ended: done, stopped by the user, stopped
// by a failure, or not over (more came after it, or it's still going).
export type Outcome = 'done' | 'stopped' | 'failed' | 'open'

export type Summary = { outcome: Outcome; text: string; steps: number; sites: string[]; reason?: string }

// reasonCase lowers the first word of a reason that follows a colon
// ("Stopped after 6 steps: couldn’t sign in"), unless it might be a name.
const lowerable = /^(couldn[’']?t|could|can[’']?t|cannot|the|this|that|these|it|no|not|unable|failed|timed|there|something|too|a|an|my|your|i)\b/i
export function reasonCase(s: string) {
  const t = s.trim().replace(/\.$/, '')
  return lowerable.test(t) && !/^I\b/.test(t) ? t[0].toLowerCase() + t.slice(1) : t
}

// summarize is the one line a finished run of steps folds into: "Done ·
// 3m 12s · 14 steps on 3 sites", or "Stopped after 6 steps: couldn’t sign in
// to united.com". The user's own steps aren't counted as Alexa's.
export function summarize(steps: Step[], o: { outcome: Outcome; ms?: number; reason?: string }): Summary {
  const mine = steps.filter((s) => s.kind !== 'user')
  const n = mine.length
  const sites = sitesOf(steps)
  const span = steps.length ? Math.max(...steps.map((s) => s.end)) - steps[0].start : 0
  const dur = fmtDuration(o.ms ?? span)
  const work = n ? `${plural(n, 'step')}${sites.length ? ` on ${plural(sites.length, 'site')}` : ''}` : ''
  const failedStep = [...mine].reverse().find((s) => s.status === 'failed')
  const reason = o.reason || (o.outcome === 'failed' ? failedStep?.error : undefined)
  let text: string
  switch (o.outcome) {
    case 'done':
      text = ['Done', dur, work].filter(Boolean).join(' · ')
      break
    case 'stopped':
    case 'failed':
      text = `${n ? `Stopped after ${plural(n, 'step')}` : 'Stopped'}${reason ? `: ${reasonCase(reason)}` : ''}`
      break
    default:
      text = [work, dur].filter(Boolean).join(' · ')
  }
  return { outcome: o.outcome, text, steps: n, sites, reason }
}

// spanText is a finished group's or step's time, for the right column:
// nothing under a second, which would only read "0s".
export const spanText = (start: number, end: number) => (end - start >= 1000 ? fmtDuration(end - start) : '')
