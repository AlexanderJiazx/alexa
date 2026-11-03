import type { Fact, Rating as R } from '../../../api/blocks'
import { compact, number } from '../../../lib/format'
import { Star } from '../icons'

// Rating is "★ 4.7 (3.1K)": the score in ink, the count quiet. Screen
// readers hear it in words.
export function Rating({ rating: r }: { rating: R }) {
  const out = r.out_of && r.out_of > 0 ? r.out_of : 5
  const words = `Rated ${number(r.value, undefined, 1)} out of ${out}${r.count ? `, ${number(r.count, undefined, 0)} reviews` : ''}`
  return (
    <span className="b-rating" role="img" aria-label={words}>
      <Star />
      <b>{r.value.toFixed(1)}</b>
      {out !== 5 && <span className="of">/{out}</span>}
      {r.count ? <span className="cnt">({compact(r.count)})</span> : null}
    </span>
  )
}

// Facts are a card's label and value pairs, two to a row.
export function Facts({ facts }: { facts: Fact[] }) {
  if (!facts.length) return null
  return (
    <dl className="b-facts">
      {facts.map((f, i) => (
        <div key={i}>
          <dt>{f.label}</dt>
          <dd>{f.value}</dd>
        </div>
      ))}
    </dl>
  )
}
