import type { ComponentType } from 'react'
import type { BlockOf } from '../../../api/blocks'
import { api } from '../../../api/client'
import { dateRange, fmtClock, fmtDate } from '../../../lib/format'
import { Calendar } from '../../../ui/icons'
import { useBlocks } from '../context'
import { Bed, Car, Fork, Pin, Plane, Task, Ticket } from '../icons'
import { HostLink } from '../parts/Action'
import { Head } from '../parts/Head'
import './timeline.css'

type B = BlockOf<'timeline'>
type Kind = NonNullable<B['days'][number]['items'][number]['kind']>

const glyphs: Record<Kind, ComponentType<{ size?: number }>> = { flight: Plane, stay: Bed, meal: Fork, event: Ticket, travel: Car, task: Task }

// Timeline is an itinerary: a heading per day, then each item on a rail
// with its time, a glyph for its kind, the title, the place and a note.
// When Alexa's message has an itinerary with times, Add to calendar gives
// it as a calendar file. One day needs no day heading: the head has the
// date.
export function Timeline({ block: b }: { block: B }) {
  const ctx = useBlocks()
  const first = b.days[0]?.date
  const last = b.days[b.days.length - 1]?.date
  const range = b.days.length > 1 ? dateRange(first, last, ctx.zone) : fmtDate(first, 'day', ctx.zone)
  const timed = b.days.some((d) => d.items.some((i) => i.time))
  return (
    <>
      <Head block={b} meta={range} />
      <div className="b-surface b-timeline">
        {b.days.map((d, i) => (
          <section key={i} className="b-day" aria-label={fmtDate(d.date, 'long', ctx.zone)}>
            {b.days.length > 1 && (
              <h4 className="b-day-head">
                <span className="dow">{fmtDate(d.date, 'weekday', ctx.zone)}</span>
                <span>{fmtDate(d.date, 'short', ctx.zone)}</span>
              </h4>
            )}
            <ol className="b-rail">
              {d.items.map((it, j) => {
                const G = it.kind ? glyphs[it.kind] : null
                return (
                  <li key={j} className={`b-stop k-${it.kind ?? 'none'}`}>
                    <span className="b-stop-time num">
                      {it.time ? fmtClock(it.time) : ''}
                      {it.end && <span className="b-stop-end">{fmtClock(it.end)}</span>}
                    </span>
                    <span className="b-stop-glyph">{G ? <G size={14} /> : <i className="dot" />}</span>
                    <div className="b-stop-body">
                      <b>{it.title}</b>
                      {it.place && (
                        <span className="b-stop-place">
                          <Pin size={12} />
                          {it.place}
                        </span>
                      )}
                      {it.note && <span className="b-stop-note">{it.note}</span>}
                    </div>
                  </li>
                )
              })}
            </ol>
          </section>
        ))}
        {timed && ctx.entryId !== undefined && (
          <div className="b-timeline-foot">
            <HostLink href={api.entryCalendarURL(ctx.entryId)} icon={<Calendar size={16} />} download>
              Add to calendar
            </HostLink>
          </div>
        )}
      </div>
    </>
  )
}
