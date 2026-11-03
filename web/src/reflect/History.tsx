import { Fragment, memo, useEffect, useLayoutEffect, useMemo, useRef, type ReactNode } from 'react'
import type { Entry } from '../api/types'
import { renderEntry } from '../features/conversation/registry'
import { StepsBlock } from '../features/progress/StepsBlock'
import { clock, day } from '../lib/format'
import { Button } from '../ui/Button'
import { EmptyState } from '../ui/EmptyState'
import { History as HistoryIcon, Report } from '../ui/icons'
import { cx } from '../ui/cx'
import { Pill } from '../ui/Pill'
import { answerOf, isStopNote, matches, sameDay, toExchanges, type Exchange } from './exchanges'
import { PastAsk } from './PastAsk'

type Props = { feed: Entry[]; hasMore: boolean; onMore: () => void; query: string }

// History is the whole conversation, one exchange after another under a
// header with its time, what it was and how it ended, and each day under
// its date (WP-12). The newest is at the bottom, as in the conversation.
export function History({ feed, hasMore, onMore, query }: Props) {
  const box = useRef<HTMLDivElement>(null)
  const prev = useRef({ first: 0, height: 0, opened: false })
  const all = useMemo(() => toExchanges(feed), [feed])
  const shown = useMemo(() => all.filter((x) => matches(x, query)), [all, query])

  // Open at the newest; keep the view steady when earlier ones load above.
  // Reflect's dialog lays out only once it's shown, so the first scroll
  // waits until the box has a size.
  const first = feed[0]?.id || 0
  const anchor = () => {
    const el = box.current
    if (!el || !el.clientHeight) return
    if (!prev.current.opened) el.scrollTop = el.scrollHeight
    else if (prev.current.first && first < prev.current.first) el.scrollTop += el.scrollHeight - prev.current.height
    prev.current = { first, height: el.scrollHeight, opened: true }
  }
  const anchorRef = useRef(anchor)
  useLayoutEffect(() => {
    anchorRef.current = anchor
    anchor()
  })
  useEffect(() => {
    const el = box.current
    if (!el) return
    const ro = new ResizeObserver(() => {
      if (!prev.current.opened) anchorRef.current()
    })
    ro.observe(el)
    return () => ro.disconnect()
  }, [])

  const searching = !!query.trim()
  return (
    <div ref={box} className="reflect-scroll">
      <div className="history">
        {hasMore && (
          <Button size="sm" className="more" onClick={onMore}>
            Show earlier
          </Button>
        )}
        {searching && (
          <p className="search-note num" role="status">
            {shown.length === 0 ? `Nothing here mentions “${query.trim()}”` : `${shown.length} of ${all.length} ${all.length === 1 ? 'exchange' : 'exchanges'}`}
            {hasMore ? '. Show earlier to search further back.' : '.'}
          </p>
        )}
        {all.length === 0 && <EmptyState icon={<HistoryIcon size={28} />} title="Nothing here yet" body="Everything you ask Alexa shows up here, a day at a time, with how each one ended." />}
        {shown.map((x, i) => (
          <Fragment key={x.id}>
            {(i === 0 || !sameDay(shown[i - 1].at, x.at)) && <DayLabel at={x.at} />}
            <ExchangeView x={x} />
          </Fragment>
        ))}
      </div>
    </div>
  )
}

function DayLabel({ at }: { at: number }) {
  return (
    <div className="day" role="heading" aria-level={2}>
      <span>{day(at)}</span>
    </div>
  )
}

const outcomes: Partial<Record<Exchange['outcome'], ReactNode>> = {
  stopped: <Pill>Stopped</Pill>,
  failed: <Pill tone="bad">Didn’t finish</Pill>,
  waiting: <Pill tone="ink">Waiting on you</Pill>,
}

// ExchangeView is one exchange: its header, then what was said and done.
// When the title is only the user's request, which follows as their
// message, the header is just the time and how it ended; the title stays
// for screen readers. An exchange only changes while it's the newest, so
// the others don't re-render as the conversation goes on.
const ExchangeView = memo(function ExchangeView({ x }: { x: Exchange }) {
  return (
    <section className="exchange" data-exchange={x.id} aria-label={`${x.title}, ${day(x.at)} ${clock(x.at)}`}>
      <header className="exchange-head">
        <span className="exchange-time num">{clock(x.at)}</span>
        <h3 className={cx('exchange-title', x.ownWords && 'sr')}>{x.title}</h3>
        <span className="exchange-tags">
          {x.scheduled && <Pill>Scheduled</Pill>}
          {x.reports > 0 && (
            <span className="exchange-kind" title={x.reports === 1 ? 'A report' : `${x.reports} reports`}>
              <Report size={14} />
              {x.reports > 1 && <span className="num">{x.reports}</span>}
            </span>
          )}
          {outcomes[x.outcome]}
        </span>
      </header>
      <div className="exchange-body">{rows(x.entries)}</div>
    </section>
  )
}, (a, b) => a.x.entries.length === b.x.entries.length && a.x.entries.every((e, i) => e === b.x.entries[i]))

// rows lays out an exchange's entries as the conversation does: runs of
// steps fold into one block, a question shows the choices and the answer,
// and an answer that was one of the choices isn't repeated as a bubble.
function rows(entries: Entry[]): ReactNode[] {
  const out: ReactNode[] = []
  const skip = new Set<number>()
  let steps: Entry[] = []
  // A plan heads its run of steps, as in the log (toRows).
  let plan: Entry | undefined
  const flush = () => {
    // Always history, so the steps don't change as Reflect closes.
    const head = plan ?? steps[0]
    if (head) out.push(<StepsBlock key={`s${head.id}`} steps={steps} plan={plan} live={false} mode="history" />)
    steps = []
    plan = undefined
  }
  for (const e of entries) {
    if (e.kind === 'step') {
      steps.push(e)
      continue
    }
    if (e.kind === 'plan') {
      if (plan) flush()
      plan = e
      continue
    }
    flush()
    if (skip.has(e.id)) continue
    // The exchange's header already says a scheduled job began, or that
    // it was stopped.
    if (e.kind === 'note' && (e.data?.scheduled || isStopNote(e))) continue
    if (e.kind === 'ask') {
      const a = answerOf(entries, e)
      // The chips already show a choice, so its message would only repeat it.
      if (a?.option || a?.picks.length) skip.add(a.entry.id)
      out.push(<PastAsk key={e.id} ask={e} answer={a} />)
      continue
    }
    out.push(<Fragment key={e.id}>{renderEntry(e, 'history')}</Fragment>)
  }
  flush()
  return out
}
