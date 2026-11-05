import type { BlockOf, Card as C } from '../../../api/blocks'
import { dateRange, fmtWhen, nights, wallClock } from '../../../lib/format'
import { cardClass } from '../../../ui/Card'
import { cx } from '../../../ui/cx'
import { Pill } from '../../../ui/Pill'
import { Sparkle } from '../../../ui/icons'
import { useBlocks } from '../context'
import { Directions, Phone } from '../icons'
import { Action, HostLink, safeURL } from '../parts/Action'
import { Img, Monogram } from '../parts/Img'
import { Money, Price } from '../parts/Money'
import { Facts, Rating } from '../parts/Rating'

export type Kind = NonNullable<BlockOf<'cards'>['kind']>

// Pick is Alexa's choice among the cards: an ink pill.
export const Pick = () => (
  <Pill tone="ink" className="b-pick">
    <Sparkle size={12} />
    Alexa’s pick
  </Pill>
)

// CardView is one card of a cards block. The layout (a row with a
// thumbnail, or a column with the picture on top) comes from where it sits
// (blocks.css), so the same card works in a narrow column, a grid and a
// carousel. The title is the card's link; its buttons sit above that link.
export function CardView({ c, kind, i }: { c: C; kind: Kind; i: number }) {
  const href = safeURL(c.url)
  const product = kind === 'product'
  const media = c.image ? (
    <Img src={c.image} alt={c.title} ratio={product ? 1 : 3 / 2} fit={product ? 'contain' : 'cover'} fallback={<Monogram name={c.title} />} />
  ) : kind === 'place' || kind === 'stay' ? (
    <Monogram name={c.title} />
  ) : null
  return (
    <article className={cx(cardClass(!!href, 'none'), 'b-card', `k-${kind}`, c.pick && 'pick', media ? 'has-media' : 'no-media')}>
      {media && <div className="b-card-media">{media}</div>}
      <div className="b-card-body">
        {c.pick && <Pick />}
        <div className="b-card-top">
          {kind === 'event' && <DateTile start={c.start} />}
          <div className="b-card-titles">
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
        </div>
        {kind === 'place' && <PlaceLines c={c} />}
        {kind === 'stay' && <StayLines c={c} />}
        {kind === 'event' && <EventLines c={c} />}
        {(kind === 'product' || kind === 'item') && (c.price || c.rating) && (
          <div className="b-card-line">
            {c.price && <Price price={c.price} />}
            {c.rating && <Rating rating={c.rating} />}
          </div>
        )}
        {c.badges && c.badges.length > 0 && (
          <div className="b-badges">
            {c.badges.map((t, j) => (
              <Pill key={j}>{t}</Pill>
            ))}
          </div>
        )}
        {c.facts && <Facts facts={c.facts} />}
        {c.note && <p className="b-card-note">{c.note}</p>}
        <CardActions c={c} kind={kind} i={i} />
      </div>
    </article>
  )
}

// A place: rating and distance, open or closed, the address.
function PlaceLines({ c }: { c: C }) {
  return (
    <>
      {(c.rating || c.distance) && (
        <div className="b-card-line">
          {c.rating && <Rating rating={c.rating} />}
          {c.distance && <span className="b-dot">{c.distance}</span>}
        </div>
      )}
      {(c.open_now !== undefined || c.hours) && (
        <p className="b-card-line">
          {c.open_now !== undefined && <span className={c.open_now ? 'b-open' : 'b-closed'}>{c.open_now ? 'Open' : 'Closed'}</span>}
          {c.hours && <span className={c.open_now !== undefined ? 'b-dot' : undefined}>{c.hours}</span>}
        </p>
      )}
      {c.address && <p className="b-card-addr">{c.address}</p>}
    </>
  )
}

// A stay: its dates and nights, the nightly price and the total.
function StayLines({ c }: { c: C }) {
  const { zone } = useBlocks()
  const n = nights(c.check_in, c.check_out, zone)
  const total = c.price && c.price.per === 'night' && n > 0 ? c.price.amount * n : 0
  return (
    <>
      {c.rating && (
        <div className="b-card-line">
          <Rating rating={c.rating} />
        </div>
      )}
      {c.check_in && (
        <p className="b-card-line">
          <span>{dateRange(c.check_in, c.check_out, zone)}</span>
          {n > 0 && <span className="b-dot">{n === 1 ? '1 night' : `${n} nights`}</span>}
        </p>
      )}
      {c.price && (
        <div className="b-card-line">
          <Price price={c.price} />
          {total > 0 && (
            <span className="b-dot b-total">
              <Money amount={total} currency={c.price.currency} /> total
            </span>
          )}
        </div>
      )}
    </>
  )
}

// An event: the day and time (the date is on the tile), the venue, the
// price. Its times are the venue's, as printed on the ticket.
function EventLines({ c }: { c: C }) {
  const s = wallClock(c.start)
  const e = wallClock(c.end)
  return (
    <>
      {s && (
        <p className="b-card-line">
          <span>{fmtWhen(s, 'weekday')}</span>
          {!s.dateOnly && (
            <span className="b-dot num">
              {fmtWhen(s, 'time')}
              {e && !e.dateOnly ? ` – ${fmtWhen(e, 'time')}` : ''}
            </span>
          )}
        </p>
      )}
      {c.venue && <p className="b-card-addr">{c.venue}</p>}
      {c.price && (
        <div className="b-card-line">
          <Price price={c.price} />
        </div>
      )}
    </>
  )
}

// DateTile is an event's day, like a page torn off a calendar.
export function DateTile({ start }: { start?: string }) {
  const s = wallClock(start)
  if (!s) return null
  return (
    <span className="b-date" aria-hidden="true">
      <span className="m">{fmtWhen(s, 'month')}</span>
      <span className="d num">{fmtWhen(s, 'dom')}</span>
    </span>
  )
}

// CardActions is the card's own action plus the ones the app adds for a
// place: Directions and Call.
function CardActions({ c, kind, i }: { c: C; kind: Kind; i: number }) {
  const where = c.lat !== undefined && c.lng !== undefined ? `${c.lat},${c.lng}` : c.address
  const dir = kind === 'place' && where ? `https://www.google.com/maps/dir/?api=1&destination=${encodeURIComponent(where)}` : ''
  const tel = kind === 'place' && c.phone ? `tel:${c.phone.replace(/[^\d+]/g, '')}` : ''
  if (!c.action && !dir && !tel) return null
  return (
    <div className="b-card-actions">
      {c.action && <Action action={c.action} item={i} primary={c.pick} />}
      {dir && (
        <HostLink href={dir} icon={<Directions size={16} />}>
          Directions
        </HostLink>
      )}
      {tel && (
        <HostLink href={tel} icon={<Phone size={16} />}>
          Call
        </HostLink>
      )}
    </div>
  )
}
