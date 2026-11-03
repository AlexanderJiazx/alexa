import type { Card as C } from '../../../api/blocks'
import { daysBetween, fmtWhen, wallClock } from '../../../lib/format'
import { cardClass } from '../../../ui/Card'
import { cx } from '../../../ui/cx'
import { Plane } from '../icons'
import { Action, safeURL } from '../parts/Action'
import { Price } from '../parts/Money'
import { Pick } from './Card'

// FlightCard is a flight as a boarding-pass row: departure time and airport,
// the duration on a line with the plane, arrival (+1 when it lands a day
// later), then the day, cabin and price. Times are each airport's own.
export function FlightCard({ c, i }: { c: C; i: number }) {
  const href = safeURL(c.url)
  const segs = c.segments ?? []
  const first = segs[0]
  const last = segs[segs.length - 1]
  const dep = wallClock(first?.depart)
  const arr = wallClock(last?.arrive)
  const plus = dep && arr ? daysBetween(dep, arr) : 0
  const stops = typeof c.stops === 'number' ? c.stops : Math.max(0, segs.length - 1)
  const via = segs.slice(0, -1).map((s) => s.to)
  const numbers = segs.map((s) => s.number).filter(Boolean)
  const meta = [dep && fmtWhen(dep, 'day'), numbers.join(', '), c.cabin].filter(Boolean)
  return (
    <article className={cx(cardClass(!!href, 'none'), 'b-card', 'k-flight', c.pick && 'pick')}>
      <div className="b-fl-head">
        <div className="b-card-titles">
          {c.pick && <Pick />}
          <h4 className="b-card-title">
            {href ? (
              <a className="b-stretch" href={href} target="_blank" rel="noopener noreferrer">
                {c.title}
              </a>
            ) : (
              c.title
            )}
          </h4>
          {c.subtitle && <p className="b-card-sub">{c.subtitle}</p>}
        </div>
        {c.price && <Price price={c.price} />}
      </div>
      {first && (
        <div className="b-pass">
          <div className="b-pass-end">
            <b className="num">{dep ? fmtWhen(dep, 'time') : ''}</b>
            <span className="code">{first.from}</span>
          </div>
          <div className="b-pass-mid">
            <span className="num">{c.duration}</span>
            <span className="b-pass-line">
              <Plane size={14} />
            </span>
            <span>
              {stops === 0 ? 'Nonstop' : stops === 1 ? '1 stop' : `${stops} stops`}
              {via.length > 0 && ` · ${via.join(', ')}`}
            </span>
          </div>
          <div className="b-pass-end r">
            <b className="num">
              {arr ? fmtWhen(arr, 'time') : ''}
              {plus > 0 && (
                <sup>
                  +{plus}
                  <span className="sr"> day{plus > 1 ? 's' : ''}</span>
                </sup>
              )}
            </b>
            <span className="code">{last.to}</span>
          </div>
        </div>
      )}
      {(meta.length > 0 || (c.badges && c.badges.length > 0)) && (
        <p className="b-fl-meta">
          {meta.length > 0 && <span>{meta.join(' · ')}</span>}
          {c.badges?.map((b, j) => (
            <span key={j} className="b-dot">
              {b}
            </span>
          ))}
        </p>
      )}
      {c.note && <p className="b-card-note">{c.note}</p>}
      {c.action && (
        <div className="b-card-actions">
          <Action action={c.action} item={i} primary={c.pick} />
        </div>
      )}
    </article>
  )
}
