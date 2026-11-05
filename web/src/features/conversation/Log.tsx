import { Fragment, memo, useEffect, useLayoutEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import type { Entry } from '../../api/types'
import { clock, day } from '../../lib/format'
import { answer } from '../../state/actions'
import { followUps } from '../../state/exchange'
import { useApp } from '../../state/store'
import { answersIn, type Answer } from '../ask/answer'
import { StepsBlock } from '../progress/StepsBlock'
import { WorkingLine } from '../progress/WorkingLine'
import './conversation.css'
import { renderEntry, type EntryMode } from './registry'
import { Suggestions } from './Suggestions'

type Props = {
  entries: Entry[]
  mode: EntryMode
  hasMore?: boolean
  onMore?: () => void
  // head opens the column (the RunCard on the main screen) and stays at its
  // top; home is what an empty exchange shows.
  head?: ReactNode
  home?: ReactNode
  // live says the work going on is this exchange's (not a background job's).
  // Without it the session's status decides.
  live?: boolean
}

// A run of steps can have a plan at its head (StepsBlock folds them into
// one line once the run is over).
type Row = { key: string; time?: number; entry?: Entry; steps?: Entry[]; plan?: Entry }

// A pause this long between entries gets a time divider.
const gap = 45 * 60e3

export function toRows(entries: Entry[], lead: boolean): Row[] {
  const out: Row[] = []
  let prev = 0
  for (const e of entries) {
    if (prev ? e.createdAt - prev > gap : lead) out.push({ key: `t${e.id}`, time: e.createdAt })
    prev = e.createdAt
    const last = out[out.length - 1]
    if (e.kind === 'step') {
      if (last?.steps) last.steps.push(e)
      else out.push({ key: `s${e.id}`, steps: [e] })
      continue
    }
    // A plan heads the run of steps that follows it, or the one it was
    // made in (a web search runs inside the model's turn, before the plan
    // call), so the run and its plan fold into one summary.
    if (e.kind === 'plan') {
      if (last?.steps && !last.plan) last.plan = e
      else out.push({ key: `s${e.id}`, steps: [], plan: e })
      continue
    }
    out.push({ key: `e${e.id}`, entry: e })
  }
  return out
}

// An entry re-renders only when it changes: the reducer keeps unchanged
// entries as they were. An ask also gets how it was answered.
const Item = memo(function Item({ entry, mode, answer }: { entry: Entry; mode: EntryMode; answer?: Answer | null }) {
  return renderEntry(entry, mode, undefined, answer)
})

// Log shows the conversation: what the user said, what Alexa said (only
// through its say tool), its questions and reports, and the steps it took
// in between, folded away once done. On the main screen it's only the
// current exchange; Reflect has all of it.
export function Log({ entries, mode, hasMore, onMore, head, home, live }: Props) {
  const box = useRef<HTMLDivElement>(null)
  const atBottom = useRef(true)
  const prev = useRef({ first: 0, last: 0, height: 0 })
  const now = mode === 'now'
  // The current exchange gets its time only when it isn't from just now.
  const lead = !now || (entries.length > 0 && Date.now() - entries[0].createdAt > 60 * 60e3)
  // An answered ask shows the choice it got ticked, so a reply that only
  // repeats that choice isn't shown again as a bubble (as in Reflect).
  const answers = useMemo(() => answersIn(entries), [entries])
  const rows = useMemo(() => toRows(answers.repeats.size ? entries.filter((e) => !answers.repeats.has(e.id)) : entries, lead), [entries, answers, lead])
  const busy = useApp((s) => now && s.data.session.status === 'working')
  const working = now && (live ?? busy)
  // Follow-ups go under Alexa's last say of the exchange, once it's done.
  const ups = useMemo(() => (now ? followUps(entries, working) : null), [now, entries, working])
  // What was there when the log first filled doesn't animate in; only what
  // arrives while you watch does (and nothing in Reflect).
  const [settled, setSettled] = useState(false)
  const filled = entries.length > 0
  useEffect(() => {
    if (!filled || settled) return
    const f = requestAnimationFrame(() => setSettled(true))
    return () => cancelAnimationFrame(f)
  }, [filled, settled])

  // Stay pinned to the newest entry unless the user scrolled up; keep the
  // view steady when older entries come in above (an earlier page, or the
  // rest of the topic a follow-up carries on). What the user just sent
  // brings them back down to it, wherever they were.
  useLayoutEffect(() => {
    const el = box.current
    if (!el) return
    const first = entries[0]?.id || 0
    const end = entries[entries.length - 1]
    if (now && end?.kind === 'user' && end.id > prev.current.last && prev.current.last) atBottom.current = true
    if (prev.current.first && first < prev.current.first) el.scrollTop += el.scrollHeight - prev.current.height
    else if (atBottom.current) el.scrollTop = el.scrollHeight
    prev.current = { first, last: end?.id || 0, height: el.scrollHeight }
  })

  // The column changes height when the composer docks, a sheet moves or the
  // keyboard opens, and its content grows after it renders (an image loads,
  // a card opens); the newest entry stays in view through all of it.
  useEffect(() => {
    const el = box.current
    if (!el || typeof ResizeObserver === 'undefined') return
    const ro = new ResizeObserver(() => {
      if (atBottom.current) el.scrollTop = el.scrollHeight
    })
    ro.observe(el)
    if (el.firstElementChild) ro.observe(el.firstElementChild)
    return () => ro.disconnect()
  }, [])

  return (
    <div
      ref={box}
      className={`log${now && settled ? ' settled' : ''}${now ? '' : ' history'}`}
      role="region"
      aria-label="Conversation"
      onScroll={(e) => {
        const el = e.currentTarget
        atBottom.current = el.scrollHeight - el.scrollTop - el.clientHeight < 80
      }}
    >
      <div className="log-inner">
        <div className="log-head">{head}</div>
        {hasMore && (
          <button type="button" className="btn sm more" onClick={onMore}>
            Show earlier
          </button>
        )}
        {entries.length === 0 &&
          !working &&
          (now ? (
            home
          ) : (
            <div className="idle">
              <p className="empty-note">Nothing here yet. Your conversations with Alexa will show up here.</p>
            </div>
          ))}
        {rows.map((r, i) => (
          <Fragment key={r.key}>
            {r.time && <div className="when">{`${day(r.time)} ${clock(r.time)}`}</div>}
            {r.steps && <StepsBlock steps={r.steps} plan={r.plan} live={working && i === rows.length - 1} mode={mode} />}
            {r.entry && <Item entry={r.entry} mode={mode} answer={answers.of.get(r.entry.id)} />}
            {r.entry && ups && r.entry.id === ups.id && <Suggestions key={ups.id} suggestions={ups.items} onPick={answer} />}
          </Fragment>
        ))}
        {working && <WorkingLine />}
      </div>
    </div>
  )
}
